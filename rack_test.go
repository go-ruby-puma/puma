// Copyright (c) the go-ruby-puma/puma authors
//
// SPDX-License-Identifier: BSD-3-Clause

package puma

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRackAppFunc(t *testing.T) {
	var app RackApp = RackAppFunc(func(env map[string]any) (int, map[string][]string, [][]byte) {
		return 201, map[string][]string{"X-A": {"b"}}, [][]byte{[]byte("hi")}
	})
	status, headers, body := app.Call(map[string]any{})
	if status != 201 || headers["X-A"][0] != "b" || string(body[0]) != "hi" {
		t.Fatalf("unexpected: %d %v %s", status, headers, body)
	}
}

func TestBuildEnvFull(t *testing.T) {
	body := strings.NewReader("name=alice&x=1")
	r := httptest.NewRequest(http.MethodPost, "http://example.test/users/42?q=go&n=2", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Custom-Header", "one")
	r.Header.Add("X-Custom-Header", "two")
	r.Header.Set("User-Agent", "test-agent")
	r.RemoteAddr = "203.0.113.7:5555"

	var errs bytes.Buffer
	env := buildEnv(r, "http", "example.test", "80", &errs)

	checks := map[string]any{
		"REQUEST_METHOD":       "POST",
		"SCRIPT_NAME":          "",
		"PATH_INFO":            "/users/42",
		"REQUEST_PATH":         "/users/42",
		"QUERY_STRING":         "q=go&n=2",
		"SERVER_NAME":          "example.test",
		"SERVER_PORT":          "80",
		"SERVER_PROTOCOL":      "HTTP/1.1",
		"HTTP_VERSION":         "HTTP/1.1",
		"SERVER_SOFTWARE":      ServerSoftware,
		"GATEWAY_INTERFACE":    "CGI/1.2",
		"REMOTE_ADDR":          "203.0.113.7",
		"rack.url_scheme":      "http",
		"CONTENT_TYPE":         "application/x-www-form-urlencoded",
		"HTTP_X_CUSTOM_HEADER": "one, two",
		"HTTP_USER_AGENT":      "test-agent",
	}
	for k, want := range checks {
		if got := env[k]; got != want {
			t.Errorf("env[%q] = %v, want %v", k, got, want)
		}
	}
	if env["CONTENT_LENGTH"] != "14" {
		t.Errorf("CONTENT_LENGTH = %v, want 14", env["CONTENT_LENGTH"])
	}
	if env["rack.multithread"] != true || env["rack.multiprocess"] != false || env["rack.run_once"] != false {
		t.Error("rack flags wrong")
	}
	if env["rack.hijack?"] != false {
		t.Error("rack.hijack? wrong")
	}
	if _, ok := env["rack.version"].([]int); !ok {
		t.Error("rack.version type wrong")
	}
	if env["rack.errors"] != io.Writer(&errs) {
		t.Error("rack.errors not wired")
	}
	// rack.input echoes the body.
	in, _ := io.ReadAll(env["rack.input"].(io.Reader))
	if string(in) != "name=alice&x=1" {
		t.Errorf("rack.input = %q", in)
	}
	if _, ok := env["REQUEST_URI"]; !ok {
		t.Error("REQUEST_URI missing")
	}
}

func TestBuildEnvNoBodyNoContentType(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://h/", nil)
	// httptest gives a body; force nil to hit the bodyInput nil branch and
	// clear Content-Length to skip that env key.
	r.Body = nil
	r.ContentLength = -1
	env := buildEnv(r, "http", "h", "80", io.Discard)
	if _, ok := env["CONTENT_TYPE"]; ok {
		t.Error("CONTENT_TYPE should be absent")
	}
	if _, ok := env["CONTENT_LENGTH"]; ok {
		t.Error("CONTENT_LENGTH should be absent")
	}
	in, _ := io.ReadAll(env["rack.input"].(io.Reader))
	if len(in) != 0 {
		t.Errorf("rack.input should be empty, got %q", in)
	}
}

func TestBuildEnvContentLengthZero(t *testing.T) {
	// A determinate zero length is reported as "0" (net/http exposes it via
	// r.ContentLength, not the header map).
	r := httptest.NewRequest(http.MethodGet, "http://h/", nil)
	r.ContentLength = 0
	env := buildEnv(r, "http", "h", "80", io.Discard)
	if env["CONTENT_LENGTH"] != "0" {
		t.Errorf("CONTENT_LENGTH = %v, want 0", env["CONTENT_LENGTH"])
	}
}

func TestHeaderKey(t *testing.T) {
	if got := headerKey("Content-Type"); got != "" {
		t.Errorf("Content-Type => %q", got)
	}
	if got := headerKey("Content-Length"); got != "" {
		t.Errorf("Content-Length => %q", got)
	}
	if got := headerKey("X-Forwarded-For"); got != "HTTP_X_FORWARDED_FOR" {
		t.Errorf("=> %q", got)
	}
	if got := headerKey("accept"); got != "HTTP_ACCEPT" {
		t.Errorf("=> %q", got)
	}
}

func TestRemoteHost(t *testing.T) {
	if got := remoteHost(""); got != "" {
		t.Errorf("empty => %q", got)
	}
	if got := remoteHost("10.0.0.1:80"); got != "10.0.0.1" {
		t.Errorf("=> %q", got)
	}
	if got := remoteHost("noport"); got != "noport" {
		t.Errorf("=> %q", got)
	}
}
