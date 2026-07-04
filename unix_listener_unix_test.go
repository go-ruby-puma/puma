// Copyright (c) the go-ruby-puma/puma authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows

package puma

import (
	"context"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"testing"
)

func TestAddUnixListenerRoundTrip(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "puma.sock")
	app := RackAppFunc(func(map[string]any) (int, map[string][]string, [][]byte) {
		return 200, nil, [][]byte{[]byte("unix-ok")}
	})
	s := NewServer(app, &Options{MaxThreads: 2})
	if _, err := s.AddUnixListener(sock); err != nil {
		t.Fatal(err)
	}
	s.Run()
	defer s.Stop()

	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}}
	resp, err := client.Get("http://unix/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "unix-ok" {
		t.Fatalf("body = %q", b)
	}
}

func TestAddUnixListenerError(t *testing.T) {
	s := NewServer(okApp(), &Options{MaxThreads: 1})
	defer s.pool.Shutdown()
	if _, err := s.AddUnixListener("/nonexistent-dir-xyzzy/puma.sock"); err == nil {
		t.Fatal("expected error binding unix socket in a missing directory")
	}
}

func TestLauncherUnixBind(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "l.sock")
	l := NewLauncher(NewConfiguration(func(c *DSL) { c.Bind("unix://" + sock) }), okApp())
	if err := l.Run(); err != nil {
		t.Fatal(err)
	}
	l.Stop()
}
