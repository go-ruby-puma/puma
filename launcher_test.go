// Copyright (c) the go-ruby-puma/puma authors
//
// SPDX-License-Identifier: BSD-3-Clause

package puma

import (
	"crypto/tls"
	"io"
	"net/http"
	"testing"
)

func okApp() RackApp {
	return RackAppFunc(func(map[string]any) (int, map[string][]string, [][]byte) {
		return 200, nil, [][]byte{[]byte("ok")}
	})
}

func TestLauncherRunTCP(t *testing.T) {
	booted := make([]int, 0)
	conf := NewConfiguration(func(c *DSL) {
		c.Bind("tcp://127.0.0.1:0")
		c.Threads(1, 4)
		c.OnWorkerBoot(func(i int) { booted = append(booted, i) })
	})
	l := NewLauncher(conf, okApp())
	if l.Server() != nil {
		t.Fatal("Server should be nil before Run")
	}
	if err := l.Run(); err != nil {
		t.Fatal(err)
	}
	defer l.Stop()

	if len(booted) != 1 || booted[0] != 0 {
		t.Fatalf("on_worker_boot not fired for worker 0: %v", booted)
	}
	addr := l.Server().listeners[0].ln.Addr().String()
	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "ok" {
		t.Fatalf("body = %q", b)
	}
}

func TestLauncherRunSSL(t *testing.T) {
	conf := NewConfiguration(func(c *DSL) { c.Bind("ssl://127.0.0.1:0") })
	l := NewLauncher(conf, okApp()).WithTLSConfig(newTLSConfig(t))
	if err := l.Run(); err != nil {
		t.Fatal(err)
	}
	defer l.Halt()

	addr := l.Server().listeners[0].ln.Addr().String()
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}}
	resp, err := client.Get("https://" + addr + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestLauncherRunErrors(t *testing.T) {
	// Unparseable bind.
	l1 := NewLauncher(NewConfiguration(func(c *DSL) { c.Bind("http://x:1") }), okApp())
	if err := l1.Run(); err == nil {
		t.Error("expected error from unsupported bind scheme")
	}
	// SSL bind without a TLS config.
	l2 := NewLauncher(NewConfiguration(func(c *DSL) { c.Bind("ssl://127.0.0.1:0") }), okApp())
	if err := l2.Run(); err == nil {
		t.Error("expected error from ssl bind without tls config")
	}
	// TCP bind on a bogus host.
	l3 := NewLauncher(NewConfiguration(func(c *DSL) { c.Bind("tcp://256.300.1.1:0") }), okApp())
	if err := l3.Run(); err == nil {
		t.Error("expected error from bogus tcp host")
	}
}

func TestLauncherStopHaltNil(t *testing.T) {
	l := NewLauncher(NewConfiguration(), okApp())
	l.Stop() // server nil => no-op
	l.Halt() // server nil => no-op
}
