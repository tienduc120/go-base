// Package debugserver exposes the Go profiling endpoints on a loopback-only listener of their own.
//
// The profiles are how a slow or leaking service is diagnosed (agentctl perf capture reads exactly these URLs). They
// also reveal memory contents and let a caller burn CPU, so they never share a port with user traffic and never bind to
// anything but loopback. Reaching them in a cluster takes a port-forward, which is the point.
package debugserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"runtime"
	"time"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	// writeTimeout must exceed the longest CPU profile a caller may ask for (30 seconds), or the profile is cut off.
	writeTimeout  = 45 * time.Second
	idleTimeout   = time.Minute
	shutdownGrace = 5 * time.Second

	// mutexFraction samples one in this many contended lock events; blockRateNanos samples blocking events longer than
	// this. Both are cheap enough to leave on and are what the lock and blocking profiles need.
	mutexFraction  = 5
	blockRateNanos = 1000
)

// Server serves the profiling endpoints.
type Server struct {
	srv *http.Server
	ln  net.Listener
}

// New validates addr and binds it, so the caller learns about a bad or taken address at startup and, with port 0, can
// read the real port from Addr. ctx bounds the bind only, not the server's life; Run takes that.
func New(ctx context.Context, addr string) (*Server, error) {
	if err := requireLoopback(addr); err != nil {
		return nil, err
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("debug server: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	return &Server{
		ln: ln,
		srv: &http.Server{
			Handler:           mux,
			ReadHeaderTimeout: readHeaderTimeout,
			ReadTimeout:       readTimeout,
			WriteTimeout:      writeTimeout,
			IdleTimeout:       idleTimeout,
		},
	}, nil
}

func requireLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("debug address %q: %w", addr, err)
	}
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf(
		"debug address %q is not loopback: profiling endpoints must never be reachable from outside",
		addr,
	)
}

// Addr is the address the server is bound to.
func (s *Server) Addr() string { return s.ln.Addr().String() }

// Close releases the port without serving, for a server that was created but never run.
func (s *Server) Close() error {
	err := s.ln.Close()
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

// Run serves until ctx ends, then shuts down gracefully and returns nil.
func (s *Server) Run(ctx context.Context) error {
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(done)
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
		defer cancel()
		_ = s.srv.Shutdown(shutdownCtx)
	})
	defer stop()

	err := s.srv.Serve(s.ln)
	if errors.Is(err, http.ErrServerClosed) {
		<-done
		return nil
	}
	return err
}

// EnableContentionProfiles turns on sampling for the mutex and block profiles, which are off by default.
func EnableContentionProfiles() {
	runtime.SetMutexProfileFraction(mutexFraction)
	runtime.SetBlockProfileRate(blockRateNanos)
}
