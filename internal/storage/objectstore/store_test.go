package objectstore

import (
	"context"
	"testing"
)

func TestNew_ReturnsStore(t *testing.T) {
	store, err := New(context.Background(), "http://localhost:9000", "test-bucket", "us-east-1", "minioadmin", "minioadmin", true)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if store == nil {
		t.Fatal("New() returned nil store")
	}
	if store.bucket != "test-bucket" {
		t.Errorf("store.bucket = %q, want %q", store.bucket, "test-bucket")
	}
}

func TestNew_WithoutEndpoint(t *testing.T) {
	store, err := New(context.Background(), "", "bucket", "us-west-2", "key", "secret", false)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if store == nil {
		t.Fatal("New() returned nil store")
	}
	if store.client == nil {
		t.Fatal("store.client should not be nil")
	}
}
