package queue

import (
	"context"
	"testing"
	"time"
)

func TestNew_InvalidURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := New(ctx, "nats://127.0.0.1:0", "TEST")
	if err == nil {
		t.Fatal("expected error when connecting to an invalid NATS URL, got nil")
	}
}

func TestSanitizeDurable(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "dots replaced",
			input:  "INVESTIGATIONS.new",
			expect: "INVESTIGATIONS-new",
		},
		{
			name:   "wildcard replaced",
			input:  "STREAM.*",
			expect: "STREAM--",
		},
		{
			name:   "greater-than replaced",
			input:  "STREAM.>",
			expect: "STREAM--",
		},
		{
			name:   "no special chars",
			input:  "simple",
			expect: "simple",
		},
		{
			name:   "multiple dots",
			input:  "a.b.c.d",
			expect: "a-b-c-d",
		},
		{
			name:   "empty string",
			input:  "",
			expect: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeDurable(tc.input)
			if got != tc.expect {
				t.Errorf("sanitizeDurable(%q) = %q, want %q", tc.input, got, tc.expect)
			}
		})
	}
}

func TestQueue_MethodsExist(t *testing.T) {
	var _ interface {
		Publish(ctx context.Context, subject string, data []byte) error
		Subscribe(ctx context.Context, subject string, handler func(msg []byte) error) error
		Close()
	} = (*Queue)(nil)
}
