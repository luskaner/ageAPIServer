// SPDX-License-Identifier: AGPL-3.0-only

package ageAPIServer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func newMatchContentServer(t *testing.T, maxFileSize int64) *Server {
	t.Helper()
	server, err := New(Config{
		DBPath: filepath.Join(t.TempDir(), "match.db"),
		MatchContent: MatchContentConfig{
			Enabled:      true,
			Folder:       t.TempDir(),
			MaxFileSize:  maxFileSize,
			MaxTotalSize: 64 << 20,
			Expiry:       time.Hour,
		},
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })
	return server
}

// buildMatchUpload reproduces the multipart capture: fileContent first, then
// matchId.
func buildMatchUpload(t *testing.T, filename string, content []byte, matchId string, withFile, withMatch bool) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if withFile {
		fw, err := mw.CreateFormFile("fileContent", filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = fw.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if withMatch {
		if err := mw.WriteField("matchId", matchId); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, mw.FormDataContentType()
}

func doMatchRequest(handler http.Handler, method, path string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, body)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

type matchManifest struct {
	IsError bool `json:"isError"`
	Files   []struct {
		Name string `json:"fileName"`
		Size int    `json:"fileSize"`
		Path string `json:"filePath"`
		CRC  string `json:"fileCRC"`
	} `json:"fileList"`
}

// downloadMatchFile follows the two-stage flow the game uses: metadata, then
// the payload at the absolute URL the metadata carries.
func downloadMatchFile(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	metadata := doMatchRequest(handler, http.MethodGet, path, nil, nil)
	if metadata.Code != http.StatusOK {
		t.Fatalf("metadata status=%d body=%q", metadata.Code, metadata.Body.String())
	}
	var manifest matchManifest
	if err := json.Unmarshal(metadata.Body.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.IsError || len(manifest.Files) != 1 {
		t.Fatalf("invalid manifest: %+v", manifest)
	}
	f := manifest.Files[0]
	u, err := url.Parse(f.Path)
	if err != nil || u.Host != "example.com" || u.Scheme != "http" {
		t.Fatalf("invalid file URL: %q (%v)", f.Path, err)
	}
	result := doMatchRequest(handler, http.MethodGet, u.RequestURI(), nil, nil)
	if result.Code != http.StatusOK {
		t.Fatalf("payload status=%d body=%q", result.Code, result.Body.String())
	}
	if f.Size != result.Body.Len() || f.CRC != fmt.Sprintf("%08x", crc32.ChecksumIEEE(result.Body.Bytes())) {
		t.Fatalf("metadata differs from payload: %+v", f)
	}
	return result
}

func newRateLimitedMatchServer(t *testing.T, perMinute int) *Server {
	t.Helper()
	server, err := New(Config{
		DBPath: filepath.Join(t.TempDir(), "match.db"),
		MatchContent: MatchContentConfig{
			Enabled:            true,
			Folder:             t.TempDir(),
			Expiry:             time.Hour,
			RateLimitPerMinute: perMinute,
		},
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })
	return server
}

// TestMatchContentRateLimit pins the per-IP bucket: the N+1th request answers
// 429 with Retry-After, and another client IP keeps its own allowance.
func TestMatchContentRateLimit(t *testing.T) {
	server := newRateLimitedMatchServer(t, 2)
	handler := server.Handler()

	for index := 0; index < 2; index++ {
		recorder := doMatchRequest(handler, http.MethodGet, "/api/MatchContent/1", nil, nil)
		if recorder.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d was limited", index+1)
		}
	}
	recorder := doMatchRequest(handler, http.MethodGet, "/api/MatchContent/1", nil, nil)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("code = %d, want 429", recorder.Code)
	}
	if got := recorder.Header().Get("Retry-After"); got == "" {
		t.Fatal("429 without Retry-After")
	}
	if body := recorder.Body.String(); !strings.Contains(body, `"errorCode":429`) {
		t.Fatalf("body = %s", body)
	}

	// The upload shares the same bucket, so it is limited from the same IP.
	upload, contentType := buildMatchUpload(t, "TEST2.aoe2mpgame", []byte("data"), "7", true, true)
	recorder = doMatchRequest(handler, http.MethodPost, "/api/MatchContent/", upload,
		map[string]string{"Content-Type": contentType})
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("upload code = %d, want 429", recorder.Code)
	}

	// Another client has its own bucket.
	request := httptest.NewRequest(http.MethodGet, "/api/MatchContent/1", nil)
	request.RemoteAddr = "198.51.100.7:1234"
	other := httptest.NewRecorder()
	handler.ServeHTTP(other, request)
	if other.Code == http.StatusTooManyRequests {
		t.Fatal("a different IP was limited")
	}
}

// TestMatchContentRateLimitDisabled is the regression of the default: no 429
// ever, whatever the request count.
func TestMatchContentRateLimitDisabled(t *testing.T) {
	server := newMatchContentServer(t, 0)
	handler := server.Handler()
	for index := 0; index < 10; index++ {
		recorder := doMatchRequest(handler, http.MethodGet, "/api/MatchContent/1", nil, nil)
		if recorder.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d was limited with the limit off", index+1)
		}
	}
}

func TestMatchContentUploadAndDownload(t *testing.T) {
	server := newMatchContentServer(t, 0)
	handler := server.Handler()
	content := []byte("AOE2 multiplayer savegame bytes")

	// The game posts to /api/MatchContent/ with mixed casing; the normalizer
	// has to reach the lowercased pattern.
	body, contentType := buildMatchUpload(t, "TEST2.aoe2mpgame", content, "234876462", true, true)
	recorder := doMatchRequest(handler, http.MethodPost, "/API/MatchContent/", body,
		map[string]string{"Content-Type": contentType})
	if recorder.Code != http.StatusOK {
		t.Fatalf("upload code = %d want 200 body=%q", recorder.Code, recorder.Body.String())
	}
	var upload struct {
		IsError *bool `json:"isError"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &upload); err != nil || upload.IsError == nil || *upload.IsError {
		t.Fatalf("upload response must contain boolean isError:false: %s (%v)", recorder.Body.String(), err)
	}

	// Download must work without a session: the game does not send one.
	download := downloadMatchFile(t, handler, "/api/MatchContent/234876462")
	if !bytes.Equal(download.Body.Bytes(), content) {
		t.Fatalf("downloaded %q want %q", download.Body.Bytes(), content)
	}
	if got := download.Header().Get("Content-Length"); got != strconv.Itoa(len(content)) {
		t.Fatalf("Content-Length = %q want %d", got, len(content))
	}
	if got := download.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("Content-Type = %q", got)
	}
	if download.Header().Get("Content-MD5") == "" || download.Header().Get("ETag") == "" {
		t.Fatal("download is missing MD5/ETag headers")
	}

	// Unknown id => 404.
	missing := doMatchRequest(handler, http.MethodGet, "/api/MatchContent/999", nil, nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing id code = %d want 404", missing.Code)
	}

	// A metadata URL from before an overwrite must not fetch the new bytes.
	manifest := doMatchRequest(handler, http.MethodGet, "/api/MatchContent/234876462", nil, nil)
	var parsed matchManifest
	if err := json.Unmarshal(manifest.Body.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	stale := doMatchRequest(handler, http.MethodGet, parsed.Files[0].Path+"/stale", nil, nil)
	if stale.Code != http.StatusNotFound {
		t.Fatalf("unknown digest code = %d want 404", stale.Code)
	}
}

func TestMatchContentUploadInvalid(t *testing.T) {
	server := newMatchContentServer(t, 0)
	handler := server.Handler()

	cases := []struct {
		name      string
		filename  string
		content   []byte
		matchId   string
		withFile  bool
		withMatch bool
	}{
		{"no_match_id", "TEST2.aoe2mpgame", []byte("data"), "", true, false},
		{"zero_match_id", "TEST2.aoe2mpgame", []byte("data"), "0", true, true},
		{"invalid_match_id", "TEST2.aoe2mpgame", []byte("data"), "abc", true, true},
		{"no_file", "", nil, "55", false, true},
		{"empty_file", "TEST2.aoe2mpgame", nil, "55", true, true},
		{"unknown_extension", "save.zip", []byte("data"), "55", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, contentType := buildMatchUpload(t, tc.filename, tc.content, tc.matchId, tc.withFile, tc.withMatch)
			recorder := doMatchRequest(handler, http.MethodPost, "/api/MatchContent/", body,
				map[string]string{"Content-Type": contentType})
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("code = %d want 400 body=%q", recorder.Code, recorder.Body.String())
			}
			missing := doMatchRequest(handler, http.MethodGet, "/api/MatchContent/55", nil, nil)
			if missing.Code != http.StatusNotFound {
				t.Fatalf("rejected upload was stored: %d", missing.Code)
			}
		})
	}

	t.Run("not_multipart", func(t *testing.T) {
		recorder := doMatchRequest(handler, http.MethodPost, "/api/MatchContent/",
			bytes.NewReader([]byte("not multipart")), nil)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("code = %d want 400", recorder.Code)
		}
	})
}

func TestMatchContentUploadTooLarge(t *testing.T) {
	server := newMatchContentServer(t, 16)
	handler := server.Handler()
	content := bytes.Repeat([]byte("A"), 32)

	body, contentType := buildMatchUpload(t, "TEST2.aoe2mpgame", content, "1", true, true)
	recorder := doMatchRequest(handler, http.MethodPost, "/api/MatchContent/", body,
		map[string]string{"Content-Type": contentType})
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("code = %d want 413 body=%q", recorder.Code, recorder.Body.String())
	}
	missing := doMatchRequest(handler, http.MethodGet, "/api/MatchContent/1", nil, nil)
	if missing.Code != http.StatusNotFound {
		t.Fatal("oversized upload was stored")
	}
}

// TestMatchContentUploadOverGlobalCap is the regression guard for the body-cap
// exemption: a savegame bigger than the server-wide 1 MiB cap has to reach the
// handler and be judged by MatchContent.MaxFileSize.
func TestMatchContentUploadOverGlobalCap(t *testing.T) {
	server := newMatchContentServer(t, 8<<20)
	handler := server.Handler()

	content := bytes.Repeat([]byte{0x11}, 3<<20)
	body, contentType := buildMatchUpload(t, "TEST2.aoe2mpgame", content, "9", true, true)
	recorder := doMatchRequest(handler, http.MethodPost, "/api/MatchContent/", body,
		map[string]string{"Content-Type": contentType})
	if recorder.Code != http.StatusOK {
		t.Fatalf("code = %d want 200 body=%q", recorder.Code, recorder.Body.String())
	}
	download := downloadMatchFile(t, handler, "/api/MatchContent/9")
	if !bytes.Equal(download.Body.Bytes(), content) {
		t.Fatalf("download len=%d want %d", download.Body.Len(), len(content))
	}

	tooBig := bytes.Repeat([]byte{0x22}, 9<<20)
	body, contentType = buildMatchUpload(t, "TEST2.aoe2mpgame", tooBig, "10", true, true)
	recorder = doMatchRequest(handler, http.MethodPost, "/api/MatchContent/", body,
		map[string]string{"Content-Type": contentType})
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("code = %d want 413 body=%q", recorder.Code, recorder.Body.String())
	}
	if missing := doMatchRequest(handler, http.MethodGet, "/api/MatchContent/10", nil, nil); missing.Code != http.StatusNotFound {
		t.Fatal("rejected oversized upload was stored")
	}
}

func TestMatchContentOverwrite(t *testing.T) {
	server := newMatchContentServer(t, 0)
	handler := server.Handler()
	first := []byte("first version")
	second := []byte("second version longer")
	for _, payload := range [][]byte{first, second} {
		body, contentType := buildMatchUpload(t, "TEST2.aoe2mpgame", payload, "42", true, true)
		recorder := doMatchRequest(handler, http.MethodPost, "/api/MatchContent/", body,
			map[string]string{"Content-Type": contentType})
		if recorder.Code != http.StatusOK {
			t.Fatalf("upload code = %d body=%q", recorder.Code, recorder.Body.String())
		}
	}
	download := downloadMatchFile(t, handler, "/api/MatchContent/42")
	if !bytes.Equal(download.Body.Bytes(), second) {
		t.Fatalf("content = %q want %q", download.Body.Bytes(), second)
	}
}

func TestMatchContentDownloadInvalid(t *testing.T) {
	server := newMatchContentServer(t, 0)
	handler := server.Handler()
	for _, path := range []string{"0", "-1", "2147483648", "abc"} {
		recorder := doMatchRequest(handler, http.MethodGet, "/api/MatchContent/"+path, nil, nil)
		if recorder.Code == http.StatusOK {
			t.Fatalf("invalid path accepted: %s", path)
		}
	}
}

func TestMatchContentDisabledAnswersNotFound(t *testing.T) {
	server, _ := newTestServer(t)
	handler := server.Handler()

	body, contentType := buildMatchUpload(t, "TEST2.aoe2mpgame", []byte("data"), "1", true, true)
	recorder := doMatchRequest(handler, http.MethodPost, "/api/MatchContent/", body,
		map[string]string{"Content-Type": contentType})
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("upload code = %d want 404", recorder.Code)
	}
	metadata := doMatchRequest(handler, http.MethodGet, "/api/MatchContent/1", nil, nil)
	if metadata.Code != http.StatusNotFound {
		t.Fatalf("metadata code = %d want 404", metadata.Code)
	}
}
