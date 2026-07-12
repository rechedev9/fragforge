package main

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPrepareHTTPServerRejectsOccupiedAddress(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen test address: %v", err)
	}
	t.Cleanup(func() { _ = occupied.Close() })

	server := &http.Server{Addr: occupied.Addr().String()}
	prepared, err := prepareHTTPServer(server)
	if err == nil {
		_ = prepared.listener.Close()
		t.Fatal("prepareHTTPServer() error = nil, want occupied-address error")
	}
	if got, want := err.Error(), "listen on "+occupied.Addr().String(); !strings.Contains(got, want) {
		t.Fatalf("prepareHTTPServer() error = %q, want containing %q", got, want)
	}
}

func TestPreparedHTTPServerServesAndWaitReturnsOnContextCancellation(t *testing.T) {
	server := &http.Server{
		Addr: "127.0.0.1:0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	}
	prepared, err := prepareHTTPServer(server)
	if err != nil {
		t.Fatalf("prepareHTTPServer(): %v", err)
	}
	prepared.Start()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = prepared.Shutdown(ctx)
	})

	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + prepared.Addr().String())
	if err != nil {
		t.Fatalf("GET prepared server: %v", err)
	}
	_ = response.Body.Close()
	if got, want := response.StatusCode, http.StatusNoContent; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := prepared.Wait(ctx); err != nil {
		t.Fatalf("Wait(cancelled) = %v, want nil", err)
	}
}

func TestPreparedHTTPServerReportsUnexpectedServeFailure(t *testing.T) {
	prepared, err := prepareHTTPServer(&http.Server{Addr: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("prepareHTTPServer(): %v", err)
	}
	if err := prepared.listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	prepared.Start()

	ctx, cancel := context.WithCancel(context.Background())
	if err := waitAndCancelOnHTTPFailure(ctx, cancel, prepared); err == nil || !strings.Contains(err.Error(), "serve:") {
		t.Fatalf("Wait() error = %v, want unexpected serve error", err)
	}
	if got, want := ctx.Err(), context.Canceled; got != want {
		t.Fatalf("runtime context error = %v, want %v", got, want)
	}
}
