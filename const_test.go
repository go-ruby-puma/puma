// Copyright (c) the go-ruby-puma/puma authors
//
// SPDX-License-Identifier: BSD-3-Clause

package puma

import "testing"

func TestStatusText(t *testing.T) {
	if got := StatusText(200); got != "OK" {
		t.Fatalf("StatusText(200) = %q, want OK", got)
	}
	if got := StatusText(404); got != "Not Found" {
		t.Fatalf("StatusText(404) = %q, want Not Found", got)
	}
	if got := StatusText(799); got != "CUSTOM" {
		t.Fatalf("StatusText(799) = %q, want CUSTOM", got)
	}
}

func TestVersionConstants(t *testing.T) {
	if ServerSoftware != "puma "+Version {
		t.Fatalf("ServerSoftware = %q", ServerSoftware)
	}
	if len(HTTPStatusCodes) == 0 {
		t.Fatal("HTTPStatusCodes empty")
	}
}

func TestHTTPParserError(t *testing.T) {
	err := NewHTTPParserError("boom")
	if err.Error() != "boom" {
		t.Fatalf("Error() = %q", err.Error())
	}
	var e *HTTPParserError = err
	if e.Message != "boom" {
		t.Fatalf("Message = %q", e.Message)
	}
}
