// Copyright (c) the go-ruby-puma/puma authors
//
// SPDX-License-Identifier: BSD-3-Clause

package puma

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// LowlevelError is the hook invoked when a Rack call raises (panics) or the
// server cannot otherwise produce a response, mirroring
// Puma::Server#lowlevel_error(e, env). It returns the Rack tuple written to the
// client. err is the recovered value; env is the request environment.
type LowlevelError func(err any, env map[string]any) (status int, headers map[string][]string, body [][]byte)

// defaultLowlevelError mirrors puma's default lowlevel_error: a bare 500 with a
// plain-text body.
func defaultLowlevelError(err any, env map[string]any) (int, map[string][]string, [][]byte) {
	return 500,
		map[string][]string{"Content-Type": {"text/plain"}},
		[][]byte{[]byte("Puma caught this error: " + fmt.Sprint(err))}
}

// Options configures a [Server] / [Launcher], mirroring the puma options Hash.
type Options struct {
	// Min / Max threads bound the Rack-call thread pool.
	MinThreads int
	MaxThreads int
	// Workers is the cluster worker count. Cluster mode (fork) is out of scope
	// for the single-process embed; the value is carried for surface parity.
	Workers int
	// Environment names the Rack environment ("development" / "production").
	Environment string
	// Binds are the listener URLs ("tcp://host:port", "ssl://host:port",
	// "unix:///path"). Consumed by [Launcher].
	Binds []string
	// OnWorkerBoot hooks run once per (single) worker at boot, receiving the
	// worker index (always 0 in single-process mode).
	OnWorkerBoot []func(int)
	// Lowlevel overrides the error handler; nil uses [defaultLowlevelError].
	Lowlevel LowlevelError
	// shutdownTimeout bounds GracefulShutdown draining; zero means wait.
	shutdownTimeout time.Duration
}

// DefaultOptions returns the puma defaults: threads 0..5, one worker, the
// "development" environment.
func DefaultOptions() *Options {
	return &Options{MinThreads: 0, MaxThreads: 5, Workers: 1, Environment: "development"}
}

// Server is a faithful port of Puma::Server: a threaded Rack web server over one
// or more listeners. It accepts connections through net/http, shapes each
// request into a Rack env, runs the app through a bounded [ThreadPool] and
// writes the response back.
type Server struct {
	app  RackApp
	opts *Options
	pool *ThreadPool

	mu        sync.Mutex
	listeners []*listener
	servers   []*http.Server
	running   bool
	errs      io.Writer
}

type listener struct {
	ln     net.Listener
	scheme string
}

// NewServer builds a server for app with the given options (nil uses
// [DefaultOptions]), mirroring Puma::Server.new(app, options).
func NewServer(app RackApp, opts *Options) *Server {
	if opts == nil {
		opts = DefaultOptions()
	}
	return &Server{
		app:  app,
		opts: opts,
		pool: NewThreadPool(opts.MinThreads, opts.MaxThreads),
		errs: os.Stderr,
	}
}

// ThreadPool returns the server's Rack-call thread pool.
func (s *Server) ThreadPool() *ThreadPool { return s.pool }

// AddTCPListener binds a plain-HTTP TCP listener on host:port and returns the
// resolved local address, mirroring Puma::Server#add_tcp_listener. A port of 0
// selects an ephemeral port.
func (s *Server) AddTCPListener(host string, port int) (net.Addr, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	s.addListener(ln, "http")
	return ln.Addr(), nil
}

// AddSSLListener binds a TLS listener on host:port with the given config and
// returns the resolved local address, mirroring Puma::Server#add_ssl_listener.
func (s *Server) AddSSLListener(host string, port int, cfg *tls.Config) (net.Addr, error) {
	if cfg == nil {
		return nil, NewHTTPParserError("ssl listener requires a tls.Config")
	}
	ln, err := tls.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)), cfg)
	if err != nil {
		return nil, err
	}
	s.addListener(ln, "https")
	return ln.Addr(), nil
}

// AddUnixListener binds a Unix-domain listener at path, mirroring puma's
// `unix://` binds. It returns the listener address. Unix sockets are only
// available on non-Windows platforms; see addUnixListener.
func (s *Server) AddUnixListener(path string) (net.Addr, error) {
	return s.addUnixListener(path)
}

func (s *Server) addListener(ln net.Listener, scheme string) {
	s.mu.Lock()
	s.listeners = append(s.listeners, &listener{ln: ln, scheme: scheme})
	s.mu.Unlock()
}

// Run starts serving on every added listener and returns immediately, mirroring
// Puma::Server#run (which returns a thread handle). Use Stop / Halt /
// GracefulShutdown to tear the server down. It is a no-op if already running.
func (s *Server) Run() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	lns := s.listeners
	s.servers = make([]*http.Server, 0, len(lns))
	for _, l := range lns {
		scheme := l.scheme
		hs := &http.Server{
			Handler: s.handler(scheme),
			// A fixed conservative header timeout, matching puma always having
			// a persistent-connection reaper. Kept generous for slow lanes.
			ReadHeaderTimeout: 30 * time.Second,
		}
		s.servers = append(s.servers, hs)
		go hs.Serve(l.ln)
	}
	s.mu.Unlock()
}

// handler returns the net/http handler that performs one Rack round-trip,
// bounding concurrency through the thread pool.
func (s *Server) handler(scheme string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverName, serverPort := hostPort(r, scheme)
		env := buildEnv(r, scheme, serverName, serverPort, s.errs)

		type result struct {
			status  int
			headers map[string][]string
			body    [][]byte
		}
		resCh := make(chan result, 1)
		s.pool.Schedule(func() {
			status, headers, body := s.invoke(env)
			resCh <- result{status, headers, body}
		})
		res := <-resCh
		s.writeResponse(w, res.status, res.headers, res.body)
	})
}

// invoke runs the Rack app, converting a panic into the lowlevel_error tuple so
// a broken app yields a 500 rather than tearing down the connection.
func (s *Server) invoke(env map[string]any) (status int, headers map[string][]string, body [][]byte) {
	defer func() {
		if rec := recover(); rec != nil {
			handler := s.opts.Lowlevel
			if handler == nil {
				handler = defaultLowlevelError
			}
			status, headers, body = handler(rec, env)
		}
	}()
	return s.app.Call(env)
}

// writeResponse writes the Rack tuple to the client, flushing each body chunk to
// model streaming / chunked bodies.
func (s *Server) writeResponse(w http.ResponseWriter, status int, headers map[string][]string, body [][]byte) {
	h := w.Header()
	for k, vs := range headers {
		h.Del(k)
		for _, v := range vs {
			h.Add(k, v)
		}
	}
	if h.Get("Server") == "" {
		h.Set("Server", ServerSoftware)
	}
	if status == 0 {
		status = 200
	}
	w.WriteHeader(status)
	flusher, canFlush := w.(http.Flusher)
	for _, chunk := range body {
		_, _ = w.Write(chunk)
		if canFlush {
			flusher.Flush()
		}
	}
}

// hostPort splits the request Host into a SERVER_NAME / SERVER_PORT pair,
// defaulting the port from the scheme when the Host omits it.
func hostPort(r *http.Request, scheme string) (name, port string) {
	host := r.Host
	if host == "" {
		host = "localhost"
	}
	if h, p, err := net.SplitHostPort(host); err == nil {
		return h, p
	}
	if scheme == "https" {
		return host, "443"
	}
	return host, "80"
}

// Stop performs a graceful shutdown: it stops accepting new connections, lets
// in-flight requests finish, then drains the thread pool. Mirrors
// Puma::Server#stop(true) / graceful stop.
func (s *Server) Stop() {
	s.shutdownServers(true)
	s.pool.Shutdown()
	s.markStopped()
}

// GracefulShutdown is an alias for a bounded graceful stop, mirroring puma's
// graceful shutdown that drains in-flight requests before exiting.
func (s *Server) GracefulShutdown() {
	ctx := context.Background()
	if s.opts.shutdownTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.opts.shutdownTimeout)
		defer cancel()
	}
	s.mu.Lock()
	servers := s.servers
	s.mu.Unlock()
	for _, hs := range servers {
		_ = hs.Shutdown(ctx)
	}
	s.pool.Shutdown()
	s.markStopped()
}

// Halt performs an immediate shutdown: connections are closed at once and the
// pool is torn down without waiting to drain. Mirrors Puma::Server#halt.
func (s *Server) Halt() {
	s.shutdownServers(false)
	s.pool.Shutdown()
	s.markStopped()
}

// shutdownServers closes every listening http.Server. When graceful is true it
// waits for in-flight requests; otherwise it closes immediately.
func (s *Server) shutdownServers(graceful bool) {
	s.mu.Lock()
	servers := s.servers
	s.mu.Unlock()
	for _, hs := range servers {
		if graceful {
			_ = hs.Shutdown(context.Background())
		} else {
			_ = hs.Close()
		}
	}
}

func (s *Server) markStopped() {
	s.mu.Lock()
	s.running = false
	s.servers = nil
	s.mu.Unlock()
}

// Running reports whether the server is currently accepting connections.
func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}
