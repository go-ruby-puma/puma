// Copyright (c) the go-ruby-puma/puma authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package puma

import "testing"

func TestAddUnixListenerWindowsUnsupported(t *testing.T) {
	s := NewServer(okApp(), &Options{MaxThreads: 1})
	defer s.pool.Shutdown()
	if _, err := s.AddUnixListener(`C:\tmp\puma.sock`); err == nil {
		t.Fatal("expected unix listeners to be unsupported on windows")
	}
}

func TestLauncherUnixBindWindows(t *testing.T) {
	l := NewLauncher(NewConfiguration(func(c *DSL) { c.Bind(`unix://C:/tmp/l.sock`) }), okApp())
	if err := l.Run(); err == nil {
		t.Fatal("expected unix bind to fail on windows")
	}
}
