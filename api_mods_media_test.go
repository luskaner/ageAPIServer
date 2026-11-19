// SPDX-License-Identifier: AGPL-3.0-only

package ageAPIServer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/luskaner/ageAPIServer/internal/models"
)

// mediaServer builds a server over a temporary upload directory holding the
// files Publish would have stored, plus one published mod that points at them.
func mediaServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(t.TempDir(), "media.db")
	server, err := New(Config{DBPath: path, UploadDir: dir})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })

	stores, err := models.Open(models.Options{Path: path, MaxOpenConns: 1})
	if err != nil {
		t.Fatalf("open stores: %v", err)
	}
	ctx := context.Background()
	user, err := stores.Users.EnsureUser(ctx, "76561198000000042", "Media", "")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	modID, err := stores.Mods.Publish(ctx, models.PublishInput{
		GameTitleID:   2,
		ModName:       "With file",
		ModVisibility: "Public",
		CreatorID:     user.UserID,
		FileURL:       "abc.zip",
		FileCRC:       "deadbeef",
		FileSize:      5,
		Images: []models.PublishImage{
			{DisplayIndex: 0, ImageName: "shot.png", Thumbnail: "shot.png"},
		},
	})
	if err != nil {
		t.Fatalf("publish mod: %v", err)
	}
	if err := stores.Close(); err != nil {
		t.Fatalf("close stores: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "abc.zip"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shot.png"), []byte("png"), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}
	return server, strconv.FormatInt(modID, 10)
}

// TestDownloadFileStreamsTheArchive checks the route behind downloadUrl.
func TestDownloadFileStreamsTheArchive(t *testing.T) {
	server, modID := mediaServer(t)
	auth := map[string]string{"Authorization": "Steam1.0 test-token"}

	recorder := do(t, server.Handler(), http.MethodGet, "/api/v4/Mods/Download/"+modID+"/file", "", auth)
	if recorder.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Body.String(); got != "hello" {
		t.Fatalf("body = %q", got)
	}
	if got := recorder.Header().Get("Content-Disposition"); !strings.Contains(got, "abc.zip") {
		t.Fatalf("content-disposition = %q", got)
	}

	anonymous := do(t, server.Handler(), http.MethodGet, "/api/v4/Mods/Download/"+modID+"/file", "", nil)
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous code = %d, want 401", anonymous.Code)
	}
}

// TestDownloadFileMissingArchive answers 404 when the mod has no file on disk.
func TestDownloadFileMissingArchive(t *testing.T) {
	server, _ := mediaServer(t)
	auth := map[string]string{"Authorization": "Steam1.0 test-token"}

	// No mod carries that id, so the store answers not found.
	recorder := do(t, server.Handler(), http.MethodGet, "/api/v4/Mods/Download/99999/file", "", auth)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", recorder.Code)
	}
}

// TestImageServesTheStoredScreenshot checks the anonymous thumbnail route.
func TestImageServesTheStoredScreenshot(t *testing.T) {
	server, _ := mediaServer(t)

	recorder := do(t, server.Handler(), http.MethodGet, "/api/v4/mods/Image/shot.png", "", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.String() != "png" {
		t.Fatalf("body = %q", recorder.Body.String())
	}

	missing := do(t, server.Handler(), http.MethodGet, "/api/v4/mods/image/missing.png", "", nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing code = %d, want 404", missing.Code)
	}
}

// TestListResponsesCarryAbsoluteMediaURLs pins the URL the client is handed:
// a bare name would leave the screenshot unreachable.
func TestListResponsesCarryAbsoluteMediaURLs(t *testing.T) {
	server, _ := mediaServer(t)

	body := `{"q":"","filter":0,"sort":"popular","order":"desc","start":1,"count":30,"game":2,"modid":""}`
	recorder := do(t, server.Handler(), http.MethodPost, "/api/v4/Mods/Find/", body, steamAuth())
	if recorder.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		ModList []models.Mod `json:"modList"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("not json: %v", err)
	}
	if len(response.ModList) != 1 || len(response.ModList[0].ImageURLs) != 1 {
		t.Fatalf("modList = %+v", response.ModList)
	}
	wantURL := url.URL{Scheme: "http", Host: "example.com", Path: "/api/v4/mods/image/shot.png"}
	want := wantURL.String()
	if got := response.ModList[0].ImageURLs[0].ImageThumbnail; got != want {
		t.Fatalf("imageThumbnail = %q, want %q", got, want)
	}
}
