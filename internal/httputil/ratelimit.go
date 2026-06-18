package httputil

import (
	"net/http"
	"sync"

	"golang.org/x/time/rate"
)

type IPRateLimiter struct {
	limiters sync.Map
	rate     rate.Limit
	burst    int
}

func NewIPRateLimiter(r rate.Limit, burst int) *IPRateLimiter {
	return &IPRateLimiter{rate: r, burst: burst}
}

func (l *IPRateLimiter) getLimiter(ip string) *rate.Limiter {
	if v, ok := l.limiters.Load(ip); ok {
		return v.(*rate.Limiter) //nolint:errcheck // sync.Map value is always *rate.Limiter
	}
	limiter := rate.NewLimiter(l.rate, l.burst)
	l.limiters.Store(ip, limiter)
	return limiter
}

func (l *IPRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		if forwarded := r.Header.Get("X-Real-Ip"); forwarded != "" {
			ip = forwarded
		}
		limiter := l.getLimiter(ip)
		if !limiter.Allow() {
			http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
