// Copyright (c) the go-ruby-puma/puma authors
//
// SPDX-License-Identifier: BSD-3-Clause

package puma

import (
	"fmt"
	"net/url"
	"strconv"
)

// DSL is a faithful port of Puma::DSL: the receiver of a puma config block. Each
// method records an option; the accumulated options are read back through a
// [Configuration]. Mirrors the puma config file DSL (bind, port, threads, …).
type DSL struct {
	opts *Options
}

// Bind adds a listener URL ("tcp://host:port", "ssl://host:port",
// "unix:///path"). Mirrors Puma::DSL#bind.
func (d *DSL) Bind(u string) { d.opts.Binds = append(d.opts.Binds, u) }

// Port binds a TCP listener on the given port (host defaults to "0.0.0.0"),
// mirroring Puma::DSL#port(port, host = nil).
func (d *DSL) Port(port int, host ...string) {
	h := "0.0.0.0"
	if len(host) > 0 && host[0] != "" {
		h = host[0]
	}
	d.opts.Binds = append(d.opts.Binds, fmt.Sprintf("tcp://%s:%d", h, port))
}

// Threads sets the min/max thread-pool bounds, mirroring Puma::DSL#threads.
func (d *DSL) Threads(min, max int) {
	d.opts.MinThreads = min
	d.opts.MaxThreads = max
}

// Workers sets the cluster worker count (surface parity; single-process embed
// runs one worker). Mirrors Puma::DSL#workers.
func (d *DSL) Workers(n int) { d.opts.Workers = n }

// Environment sets the Rack environment, mirroring Puma::DSL#environment.
func (d *DSL) Environment(env string) { d.opts.Environment = env }

// OnWorkerBoot registers a hook run once at worker boot, mirroring
// Puma::DSL#on_worker_boot.
func (d *DSL) OnWorkerBoot(hook func(int)) {
	d.opts.OnWorkerBoot = append(d.opts.OnWorkerBoot, hook)
}

// Configuration is a faithful port of Puma::Configuration: the evaluated result
// of a config block, exposing the accumulated [Options].
type Configuration struct {
	options *Options
}

// NewConfiguration evaluates zero or more config blocks against a fresh [DSL]
// seeded with [DefaultOptions], mirroring Puma::Configuration.new { |c| ... }.
func NewConfiguration(blocks ...func(*DSL)) *Configuration {
	d := &DSL{opts: DefaultOptions()}
	for _, b := range blocks {
		b(d)
	}
	return &Configuration{options: d.opts}
}

// Options returns the configuration's accumulated options.
func (c *Configuration) Options() *Options { return c.options }

// bindTarget is a parsed bind URL: a scheme plus the network/address a listener
// is opened on.
type bindTarget struct {
	scheme string // "tcp", "ssl" or "unix"
	host   string
	port   int
	path   string // for unix
}

// parseBind parses a puma bind URL into a [bindTarget], mirroring how puma maps
// tcp:// / ssl:// / unix:// binds to listeners.
func parseBind(bind string) (bindTarget, error) {
	u, err := url.Parse(bind)
	if err != nil {
		return bindTarget{}, NewHTTPParserError("invalid bind URL: " + bind)
	}
	switch u.Scheme {
	case "tcp", "ssl":
		port, perr := strconv.Atoi(u.Port())
		if perr != nil {
			return bindTarget{}, NewHTTPParserError("bind missing port: " + bind)
		}
		return bindTarget{scheme: u.Scheme, host: u.Hostname(), port: port}, nil
	case "unix":
		path := u.Path
		if u.Host != "" {
			// unix://relative/path forms parse the first segment as Host.
			path = u.Host + u.Path
		}
		if path == "" {
			return bindTarget{}, NewHTTPParserError("unix bind missing path: " + bind)
		}
		return bindTarget{scheme: "unix", path: path}, nil
	default:
		return bindTarget{}, NewHTTPParserError("unsupported bind scheme: " + u.Scheme)
	}
}

// String renders a bindTarget back to its URL form (used in diagnostics).
func (b bindTarget) String() string {
	if b.scheme == "unix" {
		return "unix://" + b.path
	}
	return fmt.Sprintf("%s://%s:%d", b.scheme, b.host, b.port)
}
