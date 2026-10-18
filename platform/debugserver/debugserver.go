// Package debugserver exposes the Go profiling endpoints on a loopback-only listener of their own.
//
// The profiles are how a slow or leaking service is diagnosed (agentctl perf capture reads exactly these URLs). They
// also reveal memory contents and let a caller burn CPU, so they never share a port with user traffic and never bind to
// anything but loopback. Reaching them in a cluster takes a port-forward, which is the point.
package debugserver

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"runtime"
	"time"

	"github.com/tienduc120/go-base/platform/httpserver"
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

// Server serves the profiling endpoints; Addr, Close, and Run come from the HTTP server underneath.
type Server struct {
	*httpserver.Server
}

// New validates addr and binds it, so the caller learns about a bad or taken address at startup and, with port 0, can
// read the real port from Addr. ctx bounds the bind only, not the server's life; Run takes that.
func New(ctx context.Context, addr string) (*Server, error) {
	if err := requireLoopback(addr); err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	inner, err := httpserver.New(ctx, addr, mux, httpserver.Timeouts{
		ReadHeader: readHeaderTimeout,
		Read:       readTimeout,
		Write:      writeTimeout,
		Idle:       idleTimeout,
		Shutdown:   shutdownGrace,
	})
	if err != nil {
		return nil, fmt.Errorf("debug server: %w", err)
	}
	return &Server{Server: inner}, nil
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

// EnableContentionProfiles turns on sampling for the mutex and block profiles, which are off by default.
func EnableContentionProfiles() {
	runtime.SetMutexProfileFraction(mutexFraction)
	runtime.SetBlockProfileRate(blockRateNanos)
}
