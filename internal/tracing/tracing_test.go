package tracing

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestInit_Disabled(t *testing.T) {
	cfg := Config{Enabled: false}

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v, want nil", err)
	}
	if shutdown == nil {
		t.Fatal("Init() returned nil shutdown, want non-nil")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown() error = %v, want nil", err)
	}
}

func TestInit_Disabled_NoGlobalChange(t *testing.T) {
	before := otel.GetTracerProvider()

	cfg := Config{Enabled: false}
	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	after := otel.GetTracerProvider()
	if before != after {
		t.Error("disabled Init should not change global TracerProvider")
	}
}

func TestInit_Enabled_SetsGlobalProvider(t *testing.T) {
	cfg := Config{
		Enabled:     true,
		Endpoint:    "localhost:4318",
		ServiceName: "test-service",
		Version:     "v0.0.1-test",
		SampleRate:  1.0,
	}

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	tp := otel.GetTracerProvider()
	if _, ok := tp.(*sdktrace.TracerProvider); !ok {
		t.Errorf("global TracerProvider type = %T, want *sdktrace.TracerProvider", tp)
	}
}

func TestInit_Enabled_FractionalSampleRate(t *testing.T) {
	cfg := Config{
		Enabled:     true,
		Endpoint:    "localhost:4318",
		ServiceName: "test-service",
		Version:     "v0.0.1-test",
		SampleRate:  0.5,
	}

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	tp := otel.GetTracerProvider()
	if _, ok := tp.(*sdktrace.TracerProvider); !ok {
		t.Errorf("global TracerProvider type = %T, want *sdktrace.TracerProvider", tp)
	}
}

func TestInit_Enabled_ShutdownIdempotent(t *testing.T) {
	cfg := Config{
		Enabled:     true,
		Endpoint:    "localhost:4318",
		ServiceName: "test-service",
		Version:     "v0.0.1-test",
		SampleRate:  1.0,
	}

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	if err := shutdown(context.Background()); err != nil {
		t.Errorf("first shutdown() error = %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("second shutdown() error = %v", err)
	}
}
