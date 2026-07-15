package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

const (
	orchestratorReadHeaderTimeout = 10 * time.Second
	orchestratorIdleTimeout       = 60 * time.Second
)

func newOrchestratorHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: orchestratorReadHeaderTimeout,
		IdleTimeout:       orchestratorIdleTimeout,
	}
}

// preparedHTTPServer reserves the configured address before workers start, so
// a bind conflict is a synchronous startup error instead of a later health-check
// timeout in the desktop shell.
type preparedHTTPServer struct {
	server   *http.Server
	listener net.Listener
	errors   chan error
}

func prepareHTTPServer(server *http.Server) (*preparedHTTPServer, error) {
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", server.Addr, err)
	}
	return &preparedHTTPServer{
		server:   server,
		listener: listener,
		errors:   make(chan error, 1),
	}, nil
}

func (s *preparedHTTPServer) Addr() net.Addr {
	return s.listener.Addr()
}

func (s *preparedHTTPServer) Start() {
	go func() {
		s.errors <- s.server.Serve(s.listener)
	}()
}

// Wait returns nil for context cancellation or normal http.Server shutdown and
// returns an error when serving stops unexpectedly.
func (s *preparedHTTPServer) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return nil
	case err := <-s.errors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve: %w", err)
	}
}

// waitAndCancelOnHTTPFailure cancels the runtime's signal context when serving
// dies unexpectedly. run still stops HTTP admissions before canceling workers.
func waitAndCancelOnHTTPFailure(ctx context.Context, cancel func(), server *preparedHTTPServer) error {
	err := server.Wait(ctx)
	if err != nil {
		cancel()
	}
	return err
}

func (s *preparedHTTPServer) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}
