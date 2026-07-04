// Copyright (c) the go-ruby-puma/puma authors
//
// SPDX-License-Identifier: BSD-3-Clause

package puma

// HTTPParserError mirrors Puma::HttpParserError — the error raised when an
// incoming request cannot be parsed into a valid HTTP message.
type HTTPParserError struct {
	Message string
}

func (e *HTTPParserError) Error() string { return e.Message }

// NewHTTPParserError builds an [HTTPParserError] with the given message.
func NewHTTPParserError(msg string) *HTTPParserError {
	return &HTTPParserError{Message: msg}
}
