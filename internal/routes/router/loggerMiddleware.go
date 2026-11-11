package router

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/luskaner/ageAPIServer/internal/logger"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// statusRecorder captures the status code for the access log.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(data)
}

// Logger records one line per request. It logs the method, the path, the status
// and the duration, and never the headers or the body: the captures show that
// the Steam token and the session cookie travel in them.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		requestID := newRequestID()
		recorder := &statusRecorder{ResponseWriter: w}
		recorder.Header().Set("X-Request-Id", requestID)

		next.ServeHTTP(recorder, r)

		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		logger.CommLog(requestID, r.Method, r.URL.Path, status, time.Since(start))
	})
}

// Recover turns a panic into a 500 JSON response instead of a dropped
// connection.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Fault("panic serving %s %s: %v", r.Method, r.URL.Path, recovered)
				shared.WriteError(w, http.StatusInternalServerError, shared.CodeInternal, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func newRequestID() string {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(buffer)
}
