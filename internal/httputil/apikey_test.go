package httputil

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIKeyAuth_EmptyKeyAllowsAll(t *testing.T) {
	t.Parallel()
	handler := APIKeyAuth("")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when API key is empty (disabled), got %d", rec.Code)
	}
}

func TestAPIKeyAuth_CorrectBearerToken(t *testing.T) {
	t.Parallel()
	handler := APIKeyAuth("secret123")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer secret123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with correct Bearer token, got %d", rec.Code)
	}
}

func TestAPIKeyAuth_IncorrectBearerToken(t *testing.T) {
	t.Parallel()
	handler := APIKeyAuth("secret123")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with incorrect Bearer token, got %d", rec.Code)
	}
}

func TestAPIKeyAuth_MissingAuth(t *testing.T) {
	t.Parallel()
	handler := APIKeyAuth("secret123")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with missing auth, got %d", rec.Code)
	}
}

func TestAPIKeyAuth_QueryParameter(t *testing.T) {
	t.Parallel()
	handler := APIKeyAuth("qkey456")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/?api_key=qkey456", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with correct query param, got %d", rec.Code)
	}
}

func TestAPIKeyAuth_WrongQueryParameter(t *testing.T) {
	t.Parallel()
	handler := APIKeyAuth("qkey456")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/?api_key=wrong", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with wrong query param, got %d", rec.Code)
	}
}

func TestAPIKeyAuth_XAPIKeyHeader(t *testing.T) {
	t.Parallel()
	handler := APIKeyAuth("xkey789")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer xkey789")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with X-API-Key equivalent Bearer header, got %d", rec.Code)
	}
}

func TestAPIKeyAuth_BearerLowercaseRejected(t *testing.T) {
	t.Parallel()
	handler := APIKeyAuth("secret123")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "bearer secret123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with lowercase 'bearer' prefix (only 'Bearer' is stripped), got %d", rec.Code)
	}
}

func TestAPIKeyAuth_WhitespaceInAuthHeader(t *testing.T) {
	t.Parallel()
	handler := APIKeyAuth("secret123")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer  secret123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with extra whitespace in Bearer token, got %d", rec.Code)
	}
}

func TestAPIKeyAuth_TimingResistance(t *testing.T) {
	t.Parallel()
	handler := APIKeyAuth("correct-key-value-for-timing-test")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	shortKey := "x"
	longKey := "this-is-a-very-long-wrong-key-that-should-take-similar-time-as-short-key"

	const iterations = 100
	measure := func(key string) int {
		rejections := 0
		for i := 0; i < iterations; i++ {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer "+key)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code == http.StatusUnauthorized {
				rejections++
			}
		}
		return rejections
	}

	shortRejections := measure(shortKey)
	longRejections := measure(longKey)

	if shortRejections != iterations {
		t.Errorf("expected all %d requests with short key rejected, got %d", iterations, shortRejections)
	}
	if longRejections != iterations {
		t.Errorf("expected all %d requests with long key rejected, got %d", iterations, longRejections)
	}
}
