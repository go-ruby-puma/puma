// Copyright (c) the go-ruby-puma/puma authors
//
// SPDX-License-Identifier: BSD-3-Clause

package puma

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTLSConfig builds a self-signed loopback TLS server config for tests.
func newTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return &tls.Config{Certificates: []tls.Certificate{{
		Certificate: [][]byte{der},
		PrivateKey:  key,
	}}}
}

func TestDefaultOptions(t *testing.T) {
	o := DefaultOptions()
	if o.MinThreads != 0 || o.MaxThreads != 5 || o.Workers != 1 || o.Environment != "development" {
		t.Fatalf("unexpected defaults: %+v", o)
	}
}

func TestNewServerDefaults(t *testing.T) {
	s := NewServer(RackAppFunc(func(map[string]any) (int, map[string][]string, [][]byte) {
		return 200, nil, nil
	}), nil)
	if s.opts.MaxThreads != 5 {
		t.Fatal("nil options should default")
	}
	if s.ThreadPool() == nil {
		t.Fatal("ThreadPool nil")
	}
	s.pool.Shutdown()
}

// startTCP starts a server on an ephemeral loopback port and returns its base
// URL plus a teardown.
func startTCP(t *testing.T, app RackApp, opts *Options) (string, *Server) {
	t.Helper()
	s := NewServer(app, opts)
	addr, err := s.AddTCPListener("127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	s.Run()
	return "http://" + addr.String(), s
}

func TestRoundTripGET(t *testing.T) {
	var gotEnv map[string]any
	app := RackAppFunc(func(env map[string]any) (int, map[string][]string, [][]byte) {
		gotEnv = env
		return 200, map[string][]string{"Content-Type": {"text/plain"}, "X-Multi": {"a", "b"}},
			[][]byte{[]byte("hello "), []byte("world")}
	})
	base, s := startTCP(t, app, &Options{MinThreads: 1, MaxThreads: 4})
	defer s.Stop()

	req, _ := http.NewRequest(http.MethodGet, base+"/greet?who=go", nil)
	req.Header.Set("X-Trace", "abc")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if string(body) != "hello world" {
		t.Fatalf("body = %q", body)
	}
	if resp.Header.Get("Content-Type") != "text/plain" {
		t.Fatalf("content-type = %q", resp.Header.Get("Content-Type"))
	}
	if got := resp.Header.Values("X-Multi"); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("X-Multi = %v", got)
	}
	if resp.Header.Get("Server") != ServerSoftware {
		t.Fatalf("Server = %q", resp.Header.Get("Server"))
	}
	// Env assertions.
	if gotEnv["REQUEST_METHOD"] != "GET" || gotEnv["PATH_INFO"] != "/greet" ||
		gotEnv["QUERY_STRING"] != "who=go" || gotEnv["HTTP_X_TRACE"] != "abc" ||
		gotEnv["rack.url_scheme"] != "http" || gotEnv["SERVER_NAME"] != "127.0.0.1" {
		t.Fatalf("env wrong: %+v", gotEnv)
	}
	if _, ok := gotEnv["SERVER_PORT"].(string); !ok {
		t.Fatal("SERVER_PORT not a string")
	}
}

func TestRoundTripPOSTBody(t *testing.T) {
	app := RackAppFunc(func(env map[string]any) (int, map[string][]string, [][]byte) {
		in, _ := io.ReadAll(env["rack.input"].(io.Reader))
		return 200, map[string][]string{"Content-Type": {"text/plain"}},
			[][]byte{[]byte("echo:" + string(in) + ":ct=" + env["CONTENT_TYPE"].(string))}
	})
	base, s := startTCP(t, app, nil)
	defer s.Stop()

	resp, err := http.Post(base+"/", "application/json", strings.NewReader(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `echo:{"a":1}:ct=application/json` {
		t.Fatalf("body = %q", body)
	}
}

func TestConcurrencyBounded(t *testing.T) {
	var active, peak int32
	release := make(chan struct{})
	app := RackAppFunc(func(env map[string]any) (int, map[string][]string, [][]byte) {
		n := atomic.AddInt32(&active, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		<-release
		atomic.AddInt32(&active, -1)
		return 200, nil, [][]byte{[]byte("ok")}
	})
	base, s := startTCP(t, app, &Options{MinThreads: 0, MaxThreads: 2})
	defer s.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Get(base + "/")
			if err == nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}()
	}
	// Give requests time to pile up, then confirm the pool capped concurrency.
	waitFor(t, "pool saturated", func() bool { return atomic.LoadInt32(&peak) == 2 })
	time.Sleep(20 * time.Millisecond)
	if p := atomic.LoadInt32(&peak); p != 2 {
		t.Fatalf("peak concurrency = %d, want 2", p)
	}
	close(release)
	wg.Wait()
}

func TestKeepAliveReuse(t *testing.T) {
	app := RackAppFunc(func(env map[string]any) (int, map[string][]string, [][]byte) {
		return 200, nil, [][]byte{[]byte("k")}
	})
	base, s := startTCP(t, app, nil)
	defer s.Stop()

	tr := &http.Transport{MaxIdleConns: 1, MaxIdleConnsPerHost: 1}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr}
	for i := 0; i < 3; i++ {
		resp, err := client.Get(base + "/")
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("status = %d", resp.StatusCode)
		}
	}
}

func TestLowlevelErrorDefault(t *testing.T) {
	app := RackAppFunc(func(env map[string]any) (int, map[string][]string, [][]byte) {
		panic("kaboom")
	})
	base, s := startTCP(t, app, nil)
	defer s.Stop()

	resp, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 500 {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	if !strings.Contains(string(body), "Puma caught this error: kaboom") {
		t.Fatalf("body = %q", body)
	}
}

func TestLowlevelErrorCustom(t *testing.T) {
	app := RackAppFunc(func(env map[string]any) (int, map[string][]string, [][]byte) {
		panic("nope")
	})
	opts := &Options{MaxThreads: 2, Lowlevel: func(err any, env map[string]any) (int, map[string][]string, [][]byte) {
		return 503, map[string][]string{"X-Err": {"handled"}}, [][]byte{[]byte("down")}
	}}
	base, s := startTCP(t, app, opts)
	defer s.Stop()

	resp, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 503 || resp.Header.Get("X-Err") != "handled" || string(body) != "down" {
		t.Fatalf("custom lowlevel failed: %d %q %q", resp.StatusCode, resp.Header.Get("X-Err"), body)
	}
}

func TestZeroStatusAndPresetServerHeader(t *testing.T) {
	app := RackAppFunc(func(env map[string]any) (int, map[string][]string, [][]byte) {
		return 0, map[string][]string{"Server": {"custom/1"}}, [][]byte{[]byte("z")}
	})
	base, s := startTCP(t, app, nil)
	defer s.Stop()

	resp, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200 (zero-normalised)", resp.StatusCode)
	}
	if resp.Header.Get("Server") != "custom/1" {
		t.Fatalf("Server = %q, want custom/1", resp.Header.Get("Server"))
	}
}

func TestGracefulShutdownDrains(t *testing.T) {
	entered := make(chan struct{})
	app := RackAppFunc(func(env map[string]any) (int, map[string][]string, [][]byte) {
		close(entered)
		time.Sleep(60 * time.Millisecond)
		return 200, nil, [][]byte{[]byte("drained")}
	})
	base, s := startTCP(t, app, nil)

	type res struct {
		body string
		err  error
	}
	ch := make(chan res, 1)
	go func() {
		resp, err := http.Get(base + "/")
		if err != nil {
			ch <- res{err: err}
			return
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		ch <- res{body: string(b)}
	}()

	<-entered
	s.Stop() // graceful: must let the in-flight request finish
	r := <-ch
	if r.err != nil {
		t.Fatalf("in-flight request failed: %v", r.err)
	}
	if r.body != "drained" {
		t.Fatalf("body = %q, want drained", r.body)
	}
	if s.Running() {
		t.Fatal("still running after Stop")
	}
}

func TestRunIdempotent(t *testing.T) {
	app := RackAppFunc(func(map[string]any) (int, map[string][]string, [][]byte) {
		return 200, nil, nil
	})
	s := NewServer(app, nil)
	if _, err := s.AddTCPListener("127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if !func() bool { s.Run(); return s.Running() }() {
		t.Fatal("not running")
	}
	s.Run() // second call is a no-op
	s.Halt()
	if s.Running() {
		t.Fatal("running after Halt")
	}
}

func TestGracefulShutdownWithTimeout(t *testing.T) {
	app := RackAppFunc(func(map[string]any) (int, map[string][]string, [][]byte) {
		return 200, nil, nil
	})
	s := NewServer(app, &Options{MaxThreads: 2, shutdownTimeout: 200 * time.Millisecond})
	if _, err := s.AddTCPListener("127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	s.Run()
	s.GracefulShutdown()
	if s.Running() {
		t.Fatal("still running")
	}
}

func TestListenerErrors(t *testing.T) {
	s := NewServer(RackAppFunc(func(map[string]any) (int, map[string][]string, [][]byte) {
		return 200, nil, nil
	}), &Options{MaxThreads: 1})
	defer s.pool.Shutdown()

	if _, err := s.AddTCPListener("256.300.1.1", 0); err == nil {
		t.Fatal("expected TCP listen error on bogus host")
	}
	if _, err := s.AddSSLListener("127.0.0.1", 0, nil); err == nil {
		t.Fatal("expected error for nil TLS config")
	}
	if _, err := s.AddSSLListener("256.300.1.1", 0, newTLSConfig(t)); err == nil {
		t.Fatal("expected TLS listen error on bogus host")
	}
}

func TestSSLRoundTrip(t *testing.T) {
	app := RackAppFunc(func(env map[string]any) (int, map[string][]string, [][]byte) {
		if env["rack.url_scheme"] != "https" {
			t.Errorf("scheme = %v, want https", env["rack.url_scheme"])
		}
		return 200, nil, [][]byte{[]byte("secure")}
	})
	s := NewServer(app, &Options{MaxThreads: 2})
	addr, err := s.AddSSLListener("127.0.0.1", 0, newTLSConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	s.Run()
	defer s.Stop()

	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}}
	resp, err := client.Get("https://" + addr.String() + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "secure" {
		t.Fatalf("body = %q", body)
	}
}

// nonFlusherWriter is an http.ResponseWriter without a Flush method, exercising
// writeResponse's no-flush path.
type nonFlusherWriter struct {
	header http.Header
	status int
	buf    []byte
}

func (w *nonFlusherWriter) Header() http.Header { return w.header }
func (w *nonFlusherWriter) Write(b []byte) (int, error) {
	w.buf = append(w.buf, b...)
	return len(b), nil
}
func (w *nonFlusherWriter) WriteHeader(status int) { w.status = status }

func TestWriteResponseNonFlusher(t *testing.T) {
	s := NewServer(RackAppFunc(func(map[string]any) (int, map[string][]string, [][]byte) {
		return 200, nil, nil
	}), &Options{MaxThreads: 1})
	defer s.pool.Shutdown()

	w := &nonFlusherWriter{header: http.Header{}}
	s.writeResponse(w, 204, map[string][]string{"X-A": {"1"}}, [][]byte{[]byte("ab"), []byte("cd")})
	if w.status != 204 {
		t.Fatalf("status = %d", w.status)
	}
	if string(w.buf) != "abcd" {
		t.Fatalf("buf = %q", w.buf)
	}
	if w.header.Get("X-A") != "1" || w.header.Get("Server") != ServerSoftware {
		t.Fatalf("headers = %v", w.header)
	}
}

func TestHostPort(t *testing.T) {
	cases := []struct {
		host, scheme, name, port string
	}{
		{"example.com:8080", "http", "example.com", "8080"},
		{"example.com", "http", "example.com", "80"},
		{"example.com", "https", "example.com", "443"},
		{"", "http", "localhost", "80"},
	}
	for _, c := range cases {
		r := &http.Request{Host: c.host}
		name, port := hostPort(r, c.scheme)
		if name != c.name || port != c.port {
			t.Errorf("hostPort(%q,%q) = %q,%q want %q,%q", c.host, c.scheme, name, port, c.name, c.port)
		}
	}
}

func TestShutdownServersNonGraceful(t *testing.T) {
	// Halt path already covered elsewhere; assert Close-based teardown directly.
	app := RackAppFunc(func(map[string]any) (int, map[string][]string, [][]byte) {
		return 200, nil, nil
	})
	s := NewServer(app, &Options{MaxThreads: 1})
	if _, err := s.AddTCPListener("127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	s.Run()
	s.shutdownServers(false)
	s.pool.Shutdown()
	s.markStopped()
	if s.Running() {
		t.Fatal("running")
	}
}
