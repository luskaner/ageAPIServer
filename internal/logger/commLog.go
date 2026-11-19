// SPDX-License-Identifier: AGPL-3.0-only

package logger

import (
	"log/slog"
	"time"
)

// CommLog records one request/response exchange. It never receives headers or
// bodies: the caller passes the already redacted summary, so a credential
// cannot reach this function by accident.
func CommLog(requestID string, method string, path string, status int, duration time.Duration) {
	write(slog.LevelInfo, "[HTTP]", "%s %s %s -> %d (%s)", requestID, method, path, status, duration.Round(time.Microsecond))
}
