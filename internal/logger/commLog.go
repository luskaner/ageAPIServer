package logger

import (
	"fmt"
	"time"
)

// CommLog records one request/response exchange. It never receives headers or
// bodies: the caller passes the already redacted summary, so a credential
// cannot reach this function by accident.
func CommLog(requestID string, method string, path string, status int, duration time.Duration) {
	write("[HTTP]", "%s %s %s -> %d (%s)", requestID, method, path, status, duration.Round(time.Microsecond))
}

// CommLogf records an exchange with an extra, already redacted detail.
func CommLogf(requestID string, method string, path string, status int, duration time.Duration, format string, a ...any) {
	write("[HTTP]", "%s %s %s -> %d (%s) %s", requestID, method, path, status,
		duration.Round(time.Microsecond), fmt.Sprintf(format, a...))
}
