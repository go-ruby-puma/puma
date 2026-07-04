// Copyright (c) the go-ruby-puma/puma authors
//
// SPDX-License-Identifier: BSD-3-Clause

// Package puma is a pure-Go (no cgo) reimplementation of Ruby's puma gem — the
// threaded Rack web server — matching the shape of the MRI `puma` gem.
//
// It builds the server machinery on the Go standard library's net/http, and
// treats the Rack application as an injectable host seam: a [RackApp] is any
// value implementing the Rack `call(env)` contract
//
//	Call(env map[string]any) (status int, headers map[string][]string, body [][]byte)
//
// The server accepts HTTP connections, translates each request into a Rack
// environment Hash (REQUEST_METHOD, SCRIPT_NAME, PATH_INFO, QUERY_STRING,
// SERVER_NAME/PORT, HTTP_* headers, rack.input, rack.url_scheme, CONTENT_TYPE /
// CONTENT_LENGTH, REMOTE_ADDR, …), invokes the application through a bounded
// [ThreadPool], and writes the returned status/headers/body back to the client.
//
// The MRI-faithful surface mirrors puma's Ruby classes: [Server] (with
// AddTCPListener / AddSSLListener / Run / Stop / Halt / GracefulShutdown),
// [Configuration] and [DSL] (bind, port, threads, workers, environment,
// on_worker_boot), [ThreadPool], [Launcher] and [Const].
//
// The application seam keeps the package free of any Ruby runtime: go-embedded-ruby
// supplies the Ruby Rack app through [RackApp]; here the app is a plain Go value,
// so the whole server is exercised in-process with real HTTP round-trips. Cluster
// (multi-worker fork) mode is out of scope for a single-process embed; the
// threaded single-process server is implemented faithfully.
//
// This is the server for go-embedded-ruby, a sibling of go-ruby-rack (the Rack
// value types), go-ruby-sinatra and go-ruby-regexp.
package puma
