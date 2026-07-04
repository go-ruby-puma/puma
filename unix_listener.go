// Copyright (c) the go-ruby-puma/puma authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows

package puma

import "net"

// addUnixListener opens a Unix-domain listener at path. Available on every
// non-Windows platform.
func (s *Server) addUnixListener(path string) (net.Addr, error) {
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	s.addListener(ln, "http")
	return ln.Addr(), nil
}
