package main

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rechedev9/fragforge/internal/httpapi"
)

func TestNewOrchestratorHTTPServerSetsDefensiveTimeouts(t *testing.T) {
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	server := newOrchestratorHTTPServer("127.0.0.1:8080", handler)

	if got, want := server.Addr, "127.0.0.1:8080"; got != want {
		t.Fatalf("Addr = %q, want %q", got, want)
	}
	if server.Handler == nil {
		t.Fatal("Handler = nil, want configured handler")
	}
	if got, want := server.ReadHeaderTimeout, orchestratorReadHeaderTimeout; got != want {
		t.Fatalf("ReadHeaderTimeout = %s, want %s", got, want)
	}
	if got, want := server.ReadTimeout, orchestratorReadTimeout; got != want {
		t.Fatalf("ReadTimeout = %s, want %s", got, want)
	}
	if server.WriteTimeout != 0 {
		t.Fatalf("WriteTimeout = %s, want zero so media streaming stays client-paced", server.WriteTimeout)
	}
	if got, want := server.IdleTimeout, orchestratorIdleTimeout; got != want {
		t.Fatalf("IdleTimeout = %s, want %s", got, want)
	}
}

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

func TestPrepareHTTPServerRejectsResolvedNonLoopbackListener(t *testing.T) {
	prepared, err := prepareHTTPServer(&http.Server{Addr: "0.0.0.0:0"})
	if err == nil {
		_ = prepared.listener.Close()
		t.Fatal("prepareHTTPServer() error = nil, want non-loopback rejection")
	}
	if !strings.Contains(err.Error(), "resolved to non-loopback authority") {
		t.Fatalf("prepareHTTPServer() error = %q, want resolved authority rejection", err)
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

func TestPreparedHTTPServerRejectsRebindingHostAndRequiresCapability(t *testing.T) {
	const capability = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	handlers := httpapi.NewHandlers(nil, nil, nil,
		httpapi.WithMutationToken(capability),
		httpapi.WithRequireReadAuth(true),
	)
	server := newOrchestratorHTTPServer("127.0.0.1:0", httpapi.Routes(handlers))
	prepared, err := prepareHTTPServer(server)
	if err != nil {
		t.Fatal(err)
	}
	prepared.Start()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = prepared.Shutdown(ctx)
	})
	endpoint := "http://" + prepared.Addr().String() + "/api/capabilities"

	response, err := http.Get(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("tokenless status = %d, want %d", response.StatusCode, http.StatusUnauthorized)
	}

	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-FragForge-Token", capability)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("authenticated status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	request, err = http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = "attacker.example:" + strconv.Itoa(prepared.Addr().(*net.TCPAddr).Port)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set("X-FragForge-Token", capability)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusMisdirectedRequest {
		t.Fatalf("rebinding status = %d, want %d", response.StatusCode, http.StatusMisdirectedRequest)
	}
}
