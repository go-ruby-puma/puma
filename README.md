<p align="center"><img src="https://raw.githubusercontent.com/go-ruby-puma/brand/main/social/go-ruby-puma-puma.png" alt="go-ruby-puma/puma" width="720"></p>

# puma — go-ruby-puma

[![Docs](https://img.shields.io/badge/docs-mkdocs--material-DC2626)](https://go-ruby-puma.github.io/docs/)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.27.1%2B-00ADD8)](https://go.dev/dl/)
[![Coverage](https://img.shields.io/badge/coverage-100%25-1a7f37)](#tests--coverage)

**A pure-Go (no cgo) reimplementation of Ruby's
[puma](https://github.com/puma/puma)** — the threaded Rack web server — built on
the Go standard library's `net/http`, and matching the shape of the MRI `puma`
gem. It accepts HTTP connections, shapes each request into a **Rack environment
Hash**, runs the application through a bounded **thread pool**, and writes the
returned `[status, headers, body]` tuple back to the client — **without any Ruby
runtime**.

The Rack application is an **injectable host seam**: a
[`RackApp`](#the-rack-app-seam) is any value implementing the Rack `call(env)`
contract. [go-embedded-ruby](https://github.com/go-embedded-ruby/ruby) supplies
the Ruby Rack/Sinatra app through this seam; here the app is a plain Go value, so
the whole server is exercised in-process with real HTTP round-trips.

It is the server for go-embedded-ruby, a sibling of
[go-ruby-rack](https://github.com/go-ruby-rack/rack) (the Rack value types),
[go-ruby-sinatra](https://github.com/go-ruby-sinatra/sinatra) and
[go-ruby-regexp](https://github.com/go-ruby-regexp/regexp).

> **What it is — and isn't.** The threaded, single-process server is implemented
> faithfully: listeners, the Rack env, the thread pool, response writing,
> keep-alive, graceful shutdown and the `lowlevel_error` path. Cluster
> (multi-worker `fork`) mode is **out of scope** for a single-process embed and
> is a documented follow-up; the `workers` option is carried for surface parity.

## MRI-faithful surface

| Ruby (`puma`) | Go (`puma`) |
| --- | --- |
| `Puma::Server.new(app, options)` | `puma.NewServer(app, opts)` |
| `#add_tcp_listener(host, port)` | `Server.AddTCPListener(host, port)` |
| `#add_ssl_listener(host, port, ctx)` | `Server.AddSSLListener(host, port, cfg)` |
| `unix://` bind | `Server.AddUnixListener(path)` |
| `#run` / `#stop` / `#halt` | `Server.Run()` / `Stop()` / `Halt()` |
| graceful shutdown | `Server.GracefulShutdown()` |
| `Puma::ThreadPool.new(min, max)` | `puma.NewThreadPool(min, max)` |
| `Puma::Configuration` / `Puma::DSL` | `puma.NewConfiguration(func(*DSL))` |
| `bind` / `port` / `threads` / `workers` | `DSL.Bind` / `Port` / `Threads` / `Workers` |
| `environment` / `on_worker_boot` | `DSL.Environment` / `OnWorkerBoot` |
| `Puma::Launcher.new(conf)#run` | `puma.NewLauncher(conf, app).Run()` |
| `Puma::Const::HTTP_STATUS_CODES` | `puma.HTTPStatusCodes` / `StatusText` |
| `Puma::HttpParserError` | `puma.HTTPParserError` |
| `#lowlevel_error(e, env)` | `Options.Lowlevel` (`LowlevelError`) |

## The Rack-app seam

```go
type RackApp interface {
    Call(env map[string]any) (status int, headers map[string][]string, body [][]byte)
}
```

`RackAppFunc` adapts a plain function (like a Ruby `->(env){ ... }` Proc). The
server builds the env — `REQUEST_METHOD`, `SCRIPT_NAME`, `PATH_INFO`,
`QUERY_STRING`, `SERVER_NAME`/`SERVER_PORT`, `HTTP_*` headers, `CONTENT_TYPE`/
`CONTENT_LENGTH`, `REMOTE_ADDR`, `rack.input`, `rack.url_scheme`, `rack.errors`,
`rack.multithread` … — invokes `Call`, and writes the tuple back (each body
chunk flushed in order to model streaming).

## Usage

```go
app := puma.RackAppFunc(func(env map[string]any) (int, map[string][]string, [][]byte) {
    return 200, map[string][]string{"Content-Type": {"text/plain"}}, [][]byte{[]byte("hello")}
})

srv := puma.NewServer(app, &puma.Options{MinThreads: 0, MaxThreads: 16})
addr, _ := srv.AddTCPListener("127.0.0.1", 0) // ephemeral port
srv.Run()
defer srv.Stop() // graceful: drains in-flight requests, then the thread pool

resp, _ := http.Get("http://" + addr.String() + "/")
```

Or drive it from a config block, as in a `config/puma.rb`:

```go
conf := puma.NewConfiguration(func(c *puma.DSL) {
    c.Bind("tcp://127.0.0.1:0")
    c.Threads(1, 8)
    c.Environment("production")
    c.OnWorkerBoot(func(i int) { /* warm up */ })
})
launcher := puma.NewLauncher(conf, app)
launcher.Run()
defer launcher.Stop()
```

## Tests & coverage

`go test -race` drives real loopback HTTP round-trips (GET/POST with body, query
strings, custom headers), asserts every standard Rack env key, exercises
thread-pool concurrency bounding, keep-alive reuse, graceful shutdown draining
and the `lowlevel_error` (panic → 500) path — all in-process, no external
network. Coverage is **100%** and enforced in CI across Linux, macOS and Windows,
and the suite runs on all six 64-bit targets (amd64, arm64, riscv64, loong64,
ppc64le, s390x — including big-endian s390x) under qemu.

## License

BSD-3-Clause — see [LICENSE](LICENSE). Copyright (c) 2026, the go-ruby-puma/puma
authors.

## WebAssembly

Being pure Go (CGO=0), this library also compiles to **WebAssembly** — both
`GOOS=js GOARCH=wasm` (browser / Node.js) and `GOOS=wasip1 GOARCH=wasm` (WASI).
CI builds both targets on every push, alongside the six 64-bit native/qemu arches.

```sh
GOOS=js     GOARCH=wasm go build ./...   # browser / Node
GOOS=wasip1 GOARCH=wasm go build ./...   # WASI (wasmtime, wasmer, wasmedge, …)
```
