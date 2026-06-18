package httputil

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type ipEntry struct {
	limiter    *rate.Limiter
	lastAccess time.Time
}

type IPRateLimiter struct {
	limiters sync.Map
	rate     rate.Limit
	burst    int
	stopCh   chan struct{}
}

func NewIPRateLimiter(r rate.Limit, burst int) *IPRateLimiter {
	return &IPRateLimiter{
		rate:   r,
		burst:  burst,
		stopCh: make(chan struct{}),
	}
}

func (l *IPRateLimiter) StartCleanup() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				cutoff := time.Now().Add(-10 * time.Minute)
				l.limiters.Range(func(key, value any) bool {
					entry := value.(*ipEntry)
					if entry.lastAccess.Before(cutoff) {
						l.limiters.Delete(key)
					}
					return true
				})
			case <-l.stopCh:
				return
			}
		}
	}()
}

func (l *IPRateLimiter) Stop() {
	close(l.stopCh)
}

func (l *IPRateLimiter) getLimiter(ip string) *rate.Limiter {
	now := time.Now()
	if v, ok := l.limiters.Load(ip); ok {
		entry := v.(*ipEntry)
		entry.lastAccess = now
		return entry.limiter
	}
	entry := &ipEntry{
		limiter:    rate.NewLimiter(l.rate, l.burst),
		lastAccess: now,
	}
	l.limiters.Store(ip, entry)
	return entry.limiter
}

func (l *IPRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		if forwarded := r.Header.Get("X-Real-Ip"); forwarded != "" {
			ip = forwarded
		}
		if host, _, err := net.SplitHostPort(ip); err == nil {
			ip = host
		}
		limiter := l.getLimiter(ip)
		if !limiter.Allow() {
			http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
