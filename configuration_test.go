// Copyright (c) the go-ruby-puma/puma authors
//
// SPDX-License-Identifier: BSD-3-Clause

package puma

import "testing"

func TestDSLAndConfiguration(t *testing.T) {
	booted := 0
	conf := NewConfiguration(func(c *DSL) {
		c.Bind("tcp://127.0.0.1:9292")
		c.Port(3000)
		c.Port(3001, "127.0.0.1")
		c.Threads(2, 16)
		c.Workers(4)
		c.Environment("production")
		c.OnWorkerBoot(func(i int) { booted += i + 1 })
	})
	o := conf.Options()
	want := []string{"tcp://127.0.0.1:9292", "tcp://0.0.0.0:3000", "tcp://127.0.0.1:3001"}
	if len(o.Binds) != 3 {
		t.Fatalf("binds = %v", o.Binds)
	}
	for i, w := range want {
		if o.Binds[i] != w {
			t.Errorf("bind[%d] = %q, want %q", i, o.Binds[i], w)
		}
	}
	if o.MinThreads != 2 || o.MaxThreads != 16 || o.Workers != 4 || o.Environment != "production" {
		t.Fatalf("opts = %+v", o)
	}
	if len(o.OnWorkerBoot) != 1 {
		t.Fatal("hook not registered")
	}
	o.OnWorkerBoot[0](0)
	if booted != 1 {
		t.Fatalf("booted = %d", booted)
	}
}

func TestParseBind(t *testing.T) {
	tcp, err := parseBind("tcp://0.0.0.0:8080")
	if err != nil || tcp.scheme != "tcp" || tcp.host != "0.0.0.0" || tcp.port != 8080 {
		t.Fatalf("tcp: %+v %v", tcp, err)
	}
	ssl, err := parseBind("ssl://127.0.0.1:9443")
	if err != nil || ssl.scheme != "ssl" || ssl.port != 9443 {
		t.Fatalf("ssl: %+v %v", ssl, err)
	}
	uAbs, err := parseBind("unix:///tmp/puma.sock")
	if err != nil || uAbs.scheme != "unix" || uAbs.path != "/tmp/puma.sock" {
		t.Fatalf("unix abs: %+v %v", uAbs, err)
	}
	uRel, err := parseBind("unix://relative/puma.sock")
	if err != nil || uRel.path != "relative/puma.sock" {
		t.Fatalf("unix rel: %+v %v", uRel, err)
	}
}

func TestParseBindErrors(t *testing.T) {
	if _, err := parseBind("tcp://\x7f"); err == nil {
		t.Error("expected URL parse error")
	}
	if _, err := parseBind("tcp://host-no-port"); err == nil {
		t.Error("expected missing-port error")
	}
	if _, err := parseBind("unix://"); err == nil {
		t.Error("expected missing-path error")
	}
	if _, err := parseBind("http://host:1"); err == nil {
		t.Error("expected unsupported-scheme error")
	}
}

func TestBindTargetString(t *testing.T) {
	if got := (bindTarget{scheme: "unix", path: "/x.sock"}).String(); got != "unix:///x.sock" {
		t.Errorf("unix String = %q", got)
	}
	if got := (bindTarget{scheme: "tcp", host: "h", port: 5}).String(); got != "tcp://h:5" {
		t.Errorf("tcp String = %q", got)
	}
}
