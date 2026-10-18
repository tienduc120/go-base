// Package httpserver is the one way a service listens for HTTP: every timeout is set, the port is bound at
// construction, and the server drains in-flight requests when its context ends.
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Timeouts bounds every phase of a connection. A zero field is a bug, not "no limit", so New refuses it.
type Timeouts struct {
	ReadHeader time.Duration
	Read       time.Duration
	Write      time.Duration
	Idle       time.Duration
	// Shutdown is how long Run waits for in-flight requests after its context ends.
	Shutdown time.Duration
}

const (
	defaultReadHeader = 5 * time.Second
	defaultRead       = 15 * time.Second
	defaultWrite      = 15 * time.Second
	defaultIdle       = time.Minute
	defaultShutdown   = 10 * time.Second
)

// DefaultTimeouts suit a request/response API; a caller with long responses passes its own.
func DefaultTimeouts() Timeouts {
	return Timeouts{
		ReadHeader: defaultReadHeader,
		Read:       defaultRead,
		Write:      defaultWrite,
		Idle:       defaultIdle,
		Shutdown:   defaultShutdown,
	}
}

func (t Timeouts) validate() error {
	for name, d := range map[string]time.Duration{
		"ReadHeader": t.ReadHeader, "Read": t.Read, "Write": t.Write, "Idle": t.Idle, "Shutdown": t.Shutdown,
	} {
		if d <= 0 {
			return fmt.Errorf("http server: timeout %s must be positive, got %v", name, d)
		}
	}
	return nil
}

// Server serves one handler on one bound listener.
type Server struct {
	srv      *http.Server
	ln       net.Listener
	shutdown time.Duration
}

// New binds addr and prepares the server. ctx bounds the bind only, not the server's life; Run takes that. Binding
// here makes a taken port a startup error and lets port 0 report the real port from Addr.
func New(ctx context.Context, addr string, handler http.Handler, timeouts Timeouts) (*Server, error) {
	if handler == nil {
		return nil, errors.New("http server: handler is nil")
	}
	if err := timeouts.validate(); err != nil {
		return nil, err
	}

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("http server: %w", err)
	}
	return &Server{
		ln:       ln,
		shutdown: timeouts.Shutdown,
		srv: &http.Server{
			Handler:           handler,
			ReadHeaderTimeout: timeouts.ReadHeader,
			ReadTimeout:       timeouts.Read,
			WriteTimeout:      timeouts.Write,
			IdleTimeout:       timeouts.Idle,
		},
	}, nil
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

// Run serves until ctx ends, then drains in-flight requests for up to the shutdown timeout and returns nil.
func (s *Server) Run(ctx context.Context) error {
	done := make(chan struct{})
	// AfterFunc starts the shutdown without a bare go statement; done lets Run wait for it.
	stop := context.AfterFunc(ctx, func() {
		defer close(done)
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.shutdown)
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
