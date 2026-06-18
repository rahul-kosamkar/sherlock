//go:build integration

package queue_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"

	"github.com/rahulkosamkar/sherlock/internal/queue"
)

func startEmbeddedNATS(t *testing.T) *natsserver.Server {
	t.Helper()
	opts := &natsserver.Options{
		Host:      "127.0.0.1",
		Port:      -1,
		NoLog:     true,
		NoSigs:    true,
		JetStream: true,
		StoreDir:  t.TempDir(),
	}

	srv, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatalf("failed to create embedded NATS server: %v", err)
	}
	srv.Start()

	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("embedded NATS server not ready")
	}
	t.Cleanup(srv.Shutdown)
	return srv
}

func natsURL(srv *natsserver.Server) string {
	return fmt.Sprintf("nats://%s", srv.Addr().String())
}

func TestPublishSubscribeAck(t *testing.T) {
	srv := startEmbeddedNATS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	q, err := queue.New(ctx, natsURL(srv), "TESTSTREAM")
	if err != nil {
		t.Fatalf("failed to create queue: %v", err)
	}
	defer q.Close()

	subject := "TESTSTREAM.test"
	payload := []byte(`{"msg":"hello"}`)

	var (
		mu       sync.Mutex
		received []byte
		done     = make(chan struct{})
	)

	err = q.Subscribe(ctx, subject, func(msg []byte, ack func(), nak func()) error {
		mu.Lock()
		defer mu.Unlock()
		received = msg
		ack()
		close(done)
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if err := q.Publish(ctx, subject, payload); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("timed out waiting for message")
	}

	mu.Lock()
	defer mu.Unlock()
	if string(received) != string(payload) {
		t.Errorf("received %q, want %q", string(received), string(payload))
	}
}

func TestPublishSubscribeNakRedelivery(t *testing.T) {
	srv := startEmbeddedNATS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	q, err := queue.New(ctx, natsURL(srv), "NAKSTREAM")
	if err != nil {
		t.Fatalf("failed to create queue: %v", err)
	}
	defer q.Close()

	subject := "NAKSTREAM.redelivery"
	payload := []byte(`{"msg":"retry-me"}`)

	var (
		mu       sync.Mutex
		attempts int
		done     = make(chan struct{})
	)

	err = q.Subscribe(ctx, subject, func(msg []byte, ack func(), nak func()) error {
		mu.Lock()
		attempts++
		a := attempts
		mu.Unlock()

		if a == 1 {
			nak()
			return nil
		}
		ack()
		close(done)
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if err := q.Publish(ctx, subject, payload); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("timed out waiting for redelivery")
	}

	mu.Lock()
	defer mu.Unlock()
	if attempts < 2 {
		t.Errorf("expected at least 2 delivery attempts, got %d", attempts)
	}
}

func TestSubscribeHandlerError(t *testing.T) {
	srv := startEmbeddedNATS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	q, err := queue.New(ctx, natsURL(srv), "ERRSTREAM")
	if err != nil {
		t.Fatalf("failed to create queue: %v", err)
	}
	defer q.Close()

	subject := "ERRSTREAM.errors"
	payload := []byte(`{"msg":"error-me"}`)

	var (
		mu       sync.Mutex
		attempts int
		done     = make(chan struct{})
	)

	err = q.Subscribe(ctx, subject, func(msg []byte, ack func(), nak func()) error {
		mu.Lock()
		attempts++
		a := attempts
		mu.Unlock()

		if a == 1 {
			return fmt.Errorf("simulated handler error")
		}
		ack()
		close(done)
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if err := q.Publish(ctx, subject, payload); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("timed out waiting for redelivery after handler error")
	}

	mu.Lock()
	defer mu.Unlock()
	if attempts < 2 {
		t.Errorf("expected at least 2 attempts after handler error, got %d", attempts)
	}
}
