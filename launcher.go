// Copyright (c) the go-ruby-puma/puma authors
//
// SPDX-License-Identifier: BSD-3-Clause

package puma

import "crypto/tls"

// Launcher is a faithful port of Puma::Launcher: it takes a [Configuration] and
// a Rack app, opens the configured binds as listeners, runs the on_worker_boot
// hooks and starts the [Server]. Cluster (multi-process) mode is out of scope;
// the single worker runs in-process.
type Launcher struct {
	config *Configuration
	app    RackApp
	server *Server
	// tlsConfig is used for any ssl:// binds. Callers set it before Run.
	tlsConfig *tls.Config
}

// NewLauncher builds a launcher for the configuration and Rack app, mirroring
// Puma::Launcher.new(conf). The app is the Rack seam the launcher serves.
func NewLauncher(config *Configuration, app RackApp) *Launcher {
	return &Launcher{config: config, app: app}
}

// WithTLSConfig sets the tls.Config used to open ssl:// binds, returning the
// launcher for chaining.
func (l *Launcher) WithTLSConfig(cfg *tls.Config) *Launcher {
	l.tlsConfig = cfg
	return l
}

// Server returns the underlying [Server] (nil until Run has been called).
func (l *Launcher) Server() *Server { return l.server }

// Run opens every configured bind, fires the on_worker_boot hooks and starts
// serving, returning immediately (the server runs in the background). Mirrors
// Puma::Launcher#run for the single-process case. Use Stop / Halt to tear down.
func (l *Launcher) Run() error {
	opts := l.config.Options()
	l.server = NewServer(l.app, opts)

	for _, bind := range opts.Binds {
		target, err := parseBind(bind)
		if err != nil {
			return err
		}
		if err := l.openBind(target); err != nil {
			return err
		}
	}

	// Single-process worker boot: run every on_worker_boot hook for worker 0.
	for _, hook := range opts.OnWorkerBoot {
		hook(0)
	}

	l.server.Run()
	return nil
}

// openBind opens one parsed bind on the server.
func (l *Launcher) openBind(t bindTarget) error {
	switch t.scheme {
	case "tcp":
		_, err := l.server.AddTCPListener(t.host, t.port)
		return err
	case "ssl":
		if l.tlsConfig == nil {
			return NewHTTPParserError("ssl bind requires a tls.Config (WithTLSConfig)")
		}
		_, err := l.server.AddSSLListener(t.host, t.port, l.tlsConfig)
		return err
	default: // "unix"
		_, err := l.server.AddUnixListener(t.path)
		return err
	}
}

// Stop gracefully stops the launched server, mirroring Puma::Launcher#stop.
func (l *Launcher) Stop() {
	if l.server != nil {
		l.server.Stop()
	}
}

// Halt immediately stops the launched server, mirroring Puma::Launcher#halt.
func (l *Launcher) Halt() {
	if l.server != nil {
		l.server.Halt()
	}
}
