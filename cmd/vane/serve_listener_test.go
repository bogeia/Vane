package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func serveConfig(t *testing.T, address, database string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"config", "sources"} {
		body, err := os.ReadFile(filepath.Join("..", "..", "configs", name+".example.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if name == "config" {
			text := strings.Replace(string(body), "address: 127.0.0.1:8080", "address: "+address, 1)
			text = strings.Replace(text, "postgres://localhost:5432/vane?sslmode=disable", database, 1)
			text = strings.Replace(text, "filename: logs/app.log", "filename: "+filepath.Join(dir, "app.log"), 1)
			body = []byte(text)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".yaml"), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, "config.yaml")
}

func TestServeOccupiedPortDoesNotConnectToDatabase(t *testing.T) {
	port, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer port.Close()
	database, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	path := serveConfig(t, port.Addr().String(), "postgres://postgres@"+database.Addr().String()+"/vane_test?sslmode=disable")
	// Queue any connection without answering PostgreSQL: initialization would otherwise block here.
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := serve(ctx, path); err == nil {
		t.Fatal("occupied port accepted")
	}
	if err := database.(*net.TCPListener).SetDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	connection, err := database.Accept()
	if err == nil {
		connection.Close()
		t.Fatal("database contacted before port reservation")
	}
	var timeout net.Error
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatal(err)
	}
	// A connection-free failure also prevents constructing the job runner after NewData.
}

func TestServeInitializationFailureAndCancellationReleasePort(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "initialization failure", true: "cancelled"}[cancelled], func(t *testing.T) {
			port, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			address := port.Addr().String()
			port.Close()
			database, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			dbAddress := database.Addr().String()
			database.Close()
			path := serveConfig(t, address, "postgres://postgres@"+dbAddress+"/vane_test?sslmode=disable")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if cancelled {
				cancel()
			}
			if err := serve(ctx, path); err == nil {
				t.Fatal("failed startup succeeded")
			}
			rebound, err := net.Listen("tcp", address)
			if err != nil {
				t.Fatalf("port leaked: %v", err)
			}
			rebound.Close()
		})
	}
}

func TestServeCancellationDuringInitializationReleasesReservation(t *testing.T) {
	port, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := port.Addr().String()
	port.Close()
	database, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	path := serveConfig(t, address, "postgres://postgres@"+database.Addr().String()+"/vane_test?sslmode=disable")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, path) }()
	if err := database.(*net.TCPListener).SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	connection, err := database.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if duplicate, err := net.Listen("tcp", address); err == nil {
		duplicate.Close()
		t.Fatal("port not held during initialization")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled startup succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("initialization did not cancel")
	}
	rebound, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("port leaked: %v", err)
	}
	rebound.Close()
}
