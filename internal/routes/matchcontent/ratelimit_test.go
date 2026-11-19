// SPDX-License-Identifier: AGPL-3.0-only

package matchcontent

import (
	"net/http"
	"testing"
	"time"
)

// fakeClock makes the token bucket deterministic.
func fakeClock(limiter *rateLimiter) *time.Time {
	now := time.Unix(1_700_000_000, 0)
	limiter.now = func() time.Time { return now }
	limiter.lastPurge = now
	return &now
}

func TestRateLimiterAllowsTheBurstThenDenies(t *testing.T) {
	limiter := newRateLimiter(3)
	fakeClock(limiter)

	for index := 0; index < 3; index++ {
		if !limiter.allow("192.0.2.1") {
			t.Fatalf("request %d was denied inside the burst", index+1)
		}
	}
	if limiter.allow("192.0.2.1") {
		t.Fatal("the request past the burst was allowed")
	}
	if !limiter.allow("198.51.100.2") {
		t.Fatal("another IP was denied")
	}
}

func TestRateLimiterRefillsOverTime(t *testing.T) {
	limiter := newRateLimiter(2)
	now := fakeClock(limiter)

	if !limiter.allow("a") || !limiter.allow("a") {
		t.Fatal("burst denied")
	}
	if limiter.allow("a") {
		t.Fatal("empty bucket allowed a request")
	}
	// Half a minute at 2 tokens per minute is one more token.
	*now = now.Add(30 * time.Second)
	if !limiter.allow("a") {
		t.Fatal("refilled token was denied")
	}
	if limiter.allow("a") {
		t.Fatal("a second request without refill was allowed")
	}
}

// TestRateLimiterPurgesIdleClients pins the cleanup: a bucket idle for a whole
// minute is dropped, since it would be full again anyway.
func TestRateLimiterPurgesIdleClients(t *testing.T) {
	limiter := newRateLimiter(2)
	now := fakeClock(limiter)

	if !limiter.allow("idle") || !limiter.allow("active") {
		t.Fatal("burst denied")
	}
	*now = now.Add(2 * time.Minute)
	if !limiter.allow("active") {
		t.Fatal("the active client was denied")
	}
	if len(limiter.buckets) != 1 {
		t.Fatalf("buckets = %d, want 1", len(limiter.buckets))
	}
	if _, ok := limiter.buckets["idle"]; ok {
		t.Fatal("the idle bucket was kept")
	}
}

func TestRateLimiterDisabledIsNil(t *testing.T) {
	if newRateLimiter(0) != nil || newRateLimiter(-1) != nil {
		t.Fatal("a non-positive limit created a limiter")
	}
}

func TestClientIPDropsThePort(t *testing.T) {
	if got := clientIP(&http.Request{RemoteAddr: "198.51.100.4:1234"}); got != "198.51.100.4" {
		t.Fatalf("clientIP = %q", got)
	}
	// A request built without a port keeps the raw value.
	if got := clientIP(&http.Request{RemoteAddr: "unix"}); got != "unix" {
		t.Fatalf("clientIP = %q", got)
	}
}
