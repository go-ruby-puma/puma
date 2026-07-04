// Copyright (c) the go-ruby-puma/puma authors
//
// SPDX-License-Identifier: BSD-3-Clause

package puma

import (
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
)

// RackApp is the injectable host seam: any value implementing the Rack
// `call(env)` contract. The server translates each incoming HTTP request into a
// Rack environment Hash, invokes Call, and writes the returned tuple back.
//
// It returns the Rack `[status, headers, body]` triple: an integer status, a
// header map (each key mapping to one-or-more values), and a body as a sequence
// of byte chunks (each chunk is written and flushed in order, modelling Rack's
// enumerable body / streaming).
type RackApp interface {
	Call(env map[string]any) (status int, headers map[string][]string, body [][]byte)
}

// RackAppFunc adapts a plain function to the [RackApp] interface, mirroring how
// a Ruby Proc (`->(env){ ... }`) is a valid Rack app.
type RackAppFunc func(env map[string]any) (int, map[string][]string, [][]byte)

// Call implements [RackApp].
func (f RackAppFunc) Call(env map[string]any) (int, map[string][]string, [][]byte) {
	return f(env)
}

// The Rack SPEC version tuple exposed as rack.version.
var rackVersion = []int{1, 6}

// buildEnv constructs the Rack environment Hash for an incoming request,
// mirroring puma's Puma::Client#env / Request#rack_env. urlScheme is "http" or
// "https"; serverName/serverPort name the listener the request arrived on.
func buildEnv(r *http.Request, urlScheme, serverName, serverPort string, errs io.Writer) map[string]any {
	env := make(map[string]any)

	env["REQUEST_METHOD"] = r.Method
	env["SCRIPT_NAME"] = ""
	env["PATH_INFO"] = r.URL.Path
	env["REQUEST_PATH"] = r.URL.Path
	env["QUERY_STRING"] = r.URL.RawQuery
	env["SERVER_NAME"] = serverName
	env["SERVER_PORT"] = serverPort
	env["SERVER_PROTOCOL"] = r.Proto
	env["HTTP_VERSION"] = r.Proto
	env["SERVER_SOFTWARE"] = ServerSoftware
	env["GATEWAY_INTERFACE"] = "CGI/1.2"
	env["REQUEST_URI"] = r.URL.RequestURI()
	env["REMOTE_ADDR"] = remoteHost(r.RemoteAddr)

	// rack.* SPEC keys.
	env["rack.version"] = rackVersion
	env["rack.url_scheme"] = urlScheme
	env["rack.input"] = bodyInput(r.Body)
	env["rack.errors"] = errs
	env["rack.multithread"] = true
	env["rack.multiprocess"] = false
	env["rack.run_once"] = false
	env["rack.hijack?"] = false

	// CONTENT_TYPE and CONTENT_LENGTH are the two headers that appear without
	// the HTTP_ prefix, exactly as in CGI / the Rack SPEC.
	if ct := r.Header.Get("Content-Type"); ct != "" {
		env["CONTENT_TYPE"] = ct
	}
	// net/http parses the request's Content-Length into r.ContentLength and
	// removes it from the header map, so read it from there: a determinate
	// length (>= 0) becomes CONTENT_LENGTH; an unknown / chunked length (-1) is
	// omitted, matching the Rack SPEC.
	if r.ContentLength >= 0 {
		env["CONTENT_LENGTH"] = strconv.FormatInt(r.ContentLength, 10)
	}

	for name, values := range r.Header {
		key := headerKey(name)
		if key == "" {
			continue
		}
		env[key] = strings.Join(values, ", ")
	}

	return env
}

// headerKey maps an HTTP header name to its Rack env key: uppercased, dashes to
// underscores, prefixed with HTTP_ — except Content-Type / Content-Length which
// are handled separately and skipped here (return "").
func headerKey(name string) string {
	canon := http.CanonicalHeaderKey(name)
	if canon == "Content-Type" || canon == "Content-Length" {
		return ""
	}
	var b strings.Builder
	b.WriteString("HTTP_")
	for _, c := range name {
		switch {
		case c == '-':
			b.WriteByte('_')
		case c >= 'a' && c <= 'z':
			b.WriteRune(c - 32)
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}

// remoteHost strips the port from a "host:port" RemoteAddr, tolerating an
// address with no port.
func remoteHost(addr string) string {
	if addr == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// bodyInput wraps the request body so rack.input is always a non-nil reader,
// mirroring puma always supplying a rewindable input IO.
func bodyInput(body io.ReadCloser) io.Reader {
	if body == nil {
		return strings.NewReader("")
	}
	return body
}
