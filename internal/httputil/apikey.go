package httputil

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

func APIKeyAuth(apiKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if apiKey == "" {
				next.ServeHTTP(w, r)
				return
			}

			auth := r.Header.Get("Authorization")
			if auth == "" {
				auth = r.URL.Query().Get("api_key")
			} else {
				auth = strings.TrimPrefix(auth, "Bearer ")
			}

			if subtle.ConstantTimeCompare([]byte(auth), []byte(apiKey)) != 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"unauthorized: invalid or missing API key"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
