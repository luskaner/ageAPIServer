package ageAPIServer

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// webBody is the form of one website publish: the official site always sends a
// description, one to five screenshots and a .zip archive.
type webBody struct {
	description string
	images      []string
	tags        []int64
	modFile     string
	modContent  string
}

func validWebBody() webBody {
	return webBody{
		description: "A web mod",
		images:      []string{"shot.png"},
		tags:        []int64{15},
		modFile:     "mod.zip",
		modContent:  "zip bytes",
	}
}

// buildWebPublish renders the multipart fields and files of a web publish. Each
// image name is both the ImageMetaData entry and the uploaded file name, which
// is how Publish pairs them.
func buildWebPublish(t *testing.T, body webBody) (map[string]string, []uploadFile) {
	t.Helper()
	fields := map[string]string{
		"gameTitleId":   "2",
		"modName":       "Web Mod",
		"modVisibility": "Public",
	}
	if body.description != "" {
		fields["description"] = body.description
	}
	var files []uploadFile
	metas := make([]map[string]any, 0, len(body.images))
	for index, name := range body.images {
		metas = append(metas, map[string]any{
			"imageId":        nil,
			"displayIndex":   index,
			"imageName":      name,
			"imageThumbnail": "",
		})
		files = append(files, uploadFile{Field: "modImages", Name: name, Content: "image bytes"})
	}
	if len(metas) > 0 {
		encoded, err := json.Marshal(metas)
		if err != nil {
			t.Fatalf("encode ImageMetaData: %v", err)
		}
		fields["ImageMetaData"] = string(encoded)
	}
	if len(body.tags) > 0 {
		ids := make([]string, 0, len(body.tags))
		for _, tag := range body.tags {
			ids = append(ids, strconv.FormatInt(tag, 10))
		}
		fields["modTags"] = strings.Join(ids, ",")
	}
	if body.modFile != "" {
		files = append(files, uploadFile{Field: "modFiles", Name: body.modFile, Content: body.modContent})
	}
	return fields, files
}

// webCookieAuth creates a real website session through the stores the server
// already holds, the way --new-session does, and returns the cookie header.
func webCookieAuth(t *testing.T, server *Server) map[string]string {
	t.Helper()
	ctx := context.Background()
	user, err := server.stores.Users.UserBySteamToken(ctx, writeToken)
	if err != nil {
		t.Fatalf("web user: %v", err)
	}
	token, _, err := server.stores.Users.CreateSession(ctx, user.UserID, time.Hour)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return map[string]string{"Cookie": DefaultCookieName + "=" + token}
}

// TestWebPublishContract pins the rules of the official website upload form:
// they apply only to a cookie-authenticated publish, while the game tickets
// keep the permissive contract of the captures.
func TestWebPublishContract(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*webBody)
		cookie  bool
		want    int
		message string
	}{
		{"cookie without description", func(b *webBody) { b.description = "" }, true, http.StatusBadRequest, "description is required"},
		{"cookie without images", func(b *webBody) { b.images = nil }, true, http.StatusBadRequest, "between 1 and 5 images are required"},
		{"cookie with six images", func(b *webBody) {
			b.images = []string{"1.png", "2.png", "3.png", "4.png", "5.png", "6.png"}
		}, true, http.StatusBadRequest, "between 1 and 5 images are required"},
		{"cookie with eight tags", func(b *webBody) {
			b.tags = []int64{15, 16, 15, 16, 15, 16, 15, 16}
		}, true, http.StatusBadRequest, "at most 7 tags are allowed"},
		{"cookie with an exe image", func(b *webBody) { b.images = []string{"shot.exe"} },
			true, http.StatusBadRequest, "unsupported image format"},
		{"cookie with an sga archive", func(b *webBody) { b.modFile = "mod.sga" },
			true, http.StatusBadRequest, "mod file must be a .zip"},
		{"cookie with a valid form", func(b *webBody) {}, true, http.StatusOK, ""},
		{"ticket without images", func(b *webBody) { b.images = nil }, false, http.StatusOK, ""},
		{"ticket with eight tags", func(b *webBody) {
			b.tags = []int64{15, 16, 15, 16, 15, 16, 15, 16}
		}, false, http.StatusOK, ""},
		{"ticket with an sga archive", func(b *webBody) { b.modFile = "mod.sga" }, false, http.StatusOK, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, _, _ := newWriteServer(t)
			headers := writeAuth()
			if tc.cookie {
				headers = webCookieAuth(t, server)
			}
			body := validWebBody()
			tc.mutate(&body)
			fields, files := buildWebPublish(t, body)
			recorder := postMultipart(t, server.Handler(), "/api/v4/mods/Publish", fields, files, headers)
			if recorder.Code != tc.want {
				t.Fatalf("code = %d, want %d body=%s", recorder.Code, tc.want, recorder.Body.String())
			}
			if tc.message != "" && !strings.Contains(recorder.Body.String(), tc.message) {
				t.Fatalf("body = %s, want message %q", recorder.Body.String(), tc.message)
			}
			if tc.want == http.StatusOK && !strings.Contains(recorder.Body.String(), `"resultValue"`) {
				t.Fatalf("body = %s", recorder.Body.String())
			}
		})
	}
}

// TestWebPublishStreamedSizeLimit is the regression for the upload cap: the
// limit is applied to the bytes copied, an oversized file answers 400 (not
// 500), and its temporary file never survives. The body is larger than
// MaxBodySize on purpose: Publish is exempt from the global cap.
func TestWebPublishStreamedSizeLimit(t *testing.T) {
	dir := t.TempDir()
	uploads := filepath.Join(dir, "uploads")
	server, err := New(Config{
		DBPath:          filepath.Join(dir, "test.db"),
		UploadDir:       uploads,
		MaxModFileBytes: 16,
		MaxBodySize:     256,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })

	body := validWebBody()
	body.modContent = strings.Repeat("x", 64)
	fields, files := buildWebPublish(t, body)
	recorder := postMultipart(t, server.Handler(), "/api/v4/mods/Publish", fields, files, writeAuth())
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400 body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "mod file is too large") {
		t.Fatalf("body = %s", recorder.Body.String())
	}
	entries, readErr := os.ReadDir(uploads)
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("read uploads: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("oversized upload left %d files", len(entries))
	}

	// Under the limit the same flow publishes: the 256-byte body cap would
	// otherwise reject the multipart framing.
	body.modContent = "tiny"
	fields, files = buildWebPublish(t, body)
	recorder = postMultipart(t, server.Handler(), "/api/v4/mods/Publish", fields, files, writeAuth())
	if recorder.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 body=%s", recorder.Code, recorder.Body.String())
	}
}
