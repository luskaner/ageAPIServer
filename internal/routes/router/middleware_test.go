// SPDX-License-Identifier: AGPL-3.0-only

package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/luskaner/ageAPIServer/internal/logger"
)

func TestLoggerAddsARequestID(t *testing.T) {
	logger.Initialize(io.Discard)
	t.Cleanup(func() { logger.Initialize(nil) })

	handler := Logger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/x", nil))

	if got := recorder.Header().Get("X-Request-Id"); got == "" {
		t.Fatal("missing X-Request-Id")
	}
	if recorder.Code != http.StatusCreated {
		t.Fatalf("code = %d", recorder.Code)
	}
}

// TestLoggerSeesAnImplicit200 pins the statusRecorder: a handler that writes
// without WriteHeader is logged as 200, not as 0.
func TestLoggerSeesAnImplicit200(t *testing.T) {
	logger.Initialize(io.Discard)
	t.Cleanup(func() { logger.Initialize(nil) })

	handler := Logger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/x", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("code = %d", recorder.Code)
	}
}

func TestRecoverAnswers500OnAPanic(t *testing.T) {
	logger.Initialize(io.Discard)
	t.Cleanup(func() { logger.Initialize(nil) })

	handler := Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/x", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "internal error") {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

// TestMaxBodySizeExemptsTheUploadPrefixes pins the exemption list against the
// global cap: those routes stream their own body to disk.
func TestMaxBodySizeExemptsTheUploadPrefixes(t *testing.T) {
	status := func(status int) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				return
			}
			w.Header().Set("X-Length", strconv.Itoa(len(body)))
			w.WriteHeader(status)
		}
	}
	exempt := []string{
		"/api/v4/mods/checkandpublishfile",
		"/api/matchcontent",
		"/api/v4/mods/publish",
		"/api/v1/mods/create",
	}
	for _, path := range exempt {
		t.Run(path, func(t *testing.T) {
			handler := MaxBodySize(4)(status(http.StatusOK))
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, strings.NewReader("0123456789")))
			if recorder.Code != http.StatusOK || recorder.Header().Get("X-Length") != "10" {
				t.Fatalf("code = %d length = %q", recorder.Code, recorder.Header().Get("X-Length"))
			}
		})
	}

	t.Run("capped", func(t *testing.T) {
		handler := MaxBodySize(4)(status(http.StatusOK))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/other", strings.NewReader("0123456789")))
		if recorder.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("code = %d, want 413", recorder.Code)
		}
	})
}
