package matchcontent

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// rateLimiter is a token bucket per client IP. A bucket holds one minute's
// allowance and refills continuously at limit tokens per minute, so a client may
// burst its whole allowance and then waits for the next token.
type rateLimiter struct {
	limit      float64
	mu         sync.Mutex
	buckets    map[string]*bucket
	lastPurge  time.Time
	now        func() time.Time
	retryAfter string
}

type bucket struct {
	tokens float64
	last   time.Time
	seen   time.Time
}

// newRateLimiter returns nil when the limit is disabled, which keeps the
// historical behaviour (no 429 ever, as no capture shows one).
func newRateLimiter(perMinute int) *rateLimiter {
	if perMinute <= 0 {
		return nil
	}
	seconds := int(math.Ceil(60 / float64(perMinute)))
	if seconds < 1 {
		seconds = 1
	}
	return &rateLimiter{
		limit:      float64(perMinute),
		buckets:    make(map[string]*bucket),
		lastPurge:  time.Now(),
		now:        time.Now,
		retryAfter: strconv.Itoa(seconds),
	}
}

func (l *rateLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b, ok := l.buckets[ip]
	if !ok {
		b = &bucket{tokens: l.limit, last: now}
		l.buckets[ip] = b
	}
	if elapsed := now.Sub(b.last).Minutes(); elapsed > 0 {
		b.tokens = min(l.limit, b.tokens+elapsed*l.limit)
		b.last = now
	}
	b.seen = now
	if now.Sub(l.lastPurge) >= time.Minute {
		l.purge(now)
		l.lastPurge = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// purge forgets the IPs idle for a whole minute: an idle bucket is full again
// anyway, so dropping it changes no answer and keeps the map small.
func (l *rateLimiter) purge(now time.Time) {
	for ip, b := range l.buckets {
		if now.Sub(b.seen) >= time.Minute {
			delete(l.buckets, ip)
		}
	}
}

func (l *rateLimiter) middleware() router.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !l.allow(clientIP(r)) {
				w.Header().Set("Retry-After", l.retryAfter)
				shared.WriteError(w, http.StatusTooManyRequests, shared.CodeTooManyRequests, "too many requests")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientIP is the address without its port: the whole point is one bucket per
// client, not per connection.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
