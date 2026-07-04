// Copyright (c) the go-ruby-puma/puma authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package puma

import "net"

// addUnixListener reports that Unix-domain listeners are unavailable on Windows,
// where puma's `unix://` binds are not supported.
func (s *Server) addUnixListener(path string) (net.Addr, error) {
	return nil, NewHTTPParserError("unix listeners are not supported on windows: " + path)
}
