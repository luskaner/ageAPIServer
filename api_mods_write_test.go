package ageAPIServer

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/luskaner/ageAPIServer/internal/models"
)

// writeToken is the Steam ticket the write tests authenticate with. It carries
// no SteamID64, so the store derives a stable synthetic id from it.
const writeToken = "c21va2UtdG9rZW4="

// writeAuth is the header set of an authenticated write request.
func writeAuth() map[string]string {
	return map[string]string{"Authorization": "Steam1.0 " + writeToken}
}

// newWriteServer builds a server with an upload directory and returns it with
// the database path and the upload directory.
func newWriteServer(t *testing.T) (*Server, string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	uploads := filepath.Join(dir, "uploads")
	server, err := New(Config{DBPath: path, UploadDir: uploads})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })
	return server, path, uploads
}

// seedOwnedMod inserts a mod whose creator is the user behind writeToken and
// returns that user id.
func seedOwnedMod(t *testing.T, path string, modID int64) int64 {
	t.Helper()
	stores, err := models.Open(models.Options{Path: path, MaxOpenConns: 1})
	if err != nil {
		t.Fatalf("open seed stores: %v", err)
	}
	defer func() { _ = stores.Close() }()

	ctx := context.Background()
	user, err := stores.Users.UserBySteamToken(ctx, writeToken)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	mod := models.ModInput{ModID: modID, GameTitleID: 2, ModName: "Owned", ModVersion: 1,
		ModTypeID: 15, ModStatusID: 1, CreatorID: user.UserID, TagIDs: []int64{15}}
	if err := stores.Mods.Upsert(ctx, mod); err != nil {
		t.Fatalf("seed mod %d: %v", modID, err)
	}
	return user.UserID
}

// uploadFile is one part of a multipart body. The field name and the file name
// differ because Publish matches ImageMetaData entries by file name.
type uploadFile struct {
	Field   string
	Name    string
	Content string
}

// postMultipart sends a multipart/form-data POST.
func postMultipart(t *testing.T, handler http.Handler, path string, fields map[string]string,
	files []uploadFile, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatalf("write field %s: %v", name, err)
		}
	}
	for _, file := range files {
		part, err := writer.CreateFormFile(file.Field, file.Name)
		if err != nil {
			t.Fatalf("create file %s: %v", file.Name, err)
		}
		if _, err := part.Write([]byte(file.Content)); err != nil {
			t.Fatalf("write file %s: %v", file.Name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, path, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// detail reads a mod through the public endpoint.
func detail(t *testing.T, handler http.Handler, modID int64) models.Detail {
	t.Helper()
	recorder := do(t, handler, http.MethodGet, "/api/v4/Mods/Detail/"+strconv.FormatInt(modID, 10), "", writeAuth())
	if recorder.Code != http.StatusOK {
		t.Fatalf("detail %d: %d %s", modID, recorder.Code, recorder.Body.String())
	}
	var out models.Detail
	if err := json.Unmarshal(recorder.Body.Bytes(), &out); err != nil {
		t.Fatalf("detail body: %v", err)
	}
	return out
}

func TestDeleteRemovesTheModOfItsCreator(t *testing.T) {
	server, path, _ := newWriteServer(t)
	seedOwnedMod(t, path, 400)
	handler := server.Handler()

	recorder := do(t, handler, http.MethodPost, "/api/v4/mods/Delete", `{"modId":400}`, writeAuth())
	if recorder.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if body := strings.TrimRight(recorder.Body.String(), "\n"); body != "true" {
		t.Fatalf("body = %q, want true", body)
	}

	recorder = do(t, handler, http.MethodGet, "/api/v4/Mods/Detail/400", "", writeAuth())
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("detail after delete: %d", recorder.Code)
	}
}

func TestDeleteRejectsAModOfAnotherUser(t *testing.T) {
	server, path, _ := newWriteServer(t)
	seed(t, path)
	recorder := do(t, server.Handler(), http.MethodPost, "/api/v4/mods/Delete", `{"modId":100}`, writeAuth())
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", recorder.Code)
	}
}

func TestDeleteRejectsAnUnknownMod(t *testing.T) {
	server, _, _ := newWriteServer(t)
	recorder := do(t, server.Handler(), http.MethodPost, "/api/v4/mods/Delete", `{"modId":999}`, writeAuth())
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", recorder.Code)
	}
}

func TestDeleteRejectsABadBody(t *testing.T) {
	server, _, _ := newWriteServer(t)
	handler := server.Handler()
	for name, body := range map[string]string{
		"no modId": `{}`,
		"not json": `not json`,
		"negative": `{"modId":-1}`,
		"empty":    ``,
	} {
		t.Run(name, func(t *testing.T) {
			recorder := do(t, handler, http.MethodPost, "/api/v4/mods/Delete", body, writeAuth())
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("code = %d, want 400", recorder.Code)
			}
		})
	}
}

func TestDeleteRequiresAuth(t *testing.T) {
	server, path, _ := newWriteServer(t)
	seedOwnedMod(t, path, 400)
	recorder := do(t, server.Handler(), http.MethodPost, "/api/v4/mods/Delete", `{"modId":400}`, nil)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", recorder.Code)
	}
}

func TestReportAcceptsTheCapturedBody(t *testing.T) {
	server, path, _ := newWriteServer(t)
	seed(t, path)
	body := `{"modId":"100","concernCategoryId":"7","concernCategoryName":"ReasonsOther","concernContent":" "}`
	recorder := do(t, server.Handler(), http.MethodPost, "/api/v4/mods/Report", body, writeAuth())
	if recorder.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := strings.TrimRight(recorder.Body.String(), "\n"); got != "true" {
		t.Fatalf("body = %q, want true", got)
	}
}

func TestReportRejectsAnUnknownMod(t *testing.T) {
	server, _, _ := newWriteServer(t)
	body := `{"modId":"999","concernCategoryId":"7","concernCategoryName":"ReasonsOther","concernContent":"x"}`
	recorder := do(t, server.Handler(), http.MethodPost, "/api/v4/mods/Report", body, writeAuth())
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", recorder.Code)
	}
}

func TestReportRejectsABadBody(t *testing.T) {
	server, _, _ := newWriteServer(t)
	handler := server.Handler()
	for name, body := range map[string]string{
		"no modId":         `{"concernCategoryId":"7","concernCategoryName":"ReasonsOther"}`,
		"non numeric id":   `{"modId":"abc","concernCategoryId":"7","concernCategoryName":"ReasonsOther"}`,
		"no category":      `{"modId":"100","concernCategoryName":"ReasonsOther"}`,
		"no category name": `{"modId":"100","concernCategoryId":"7"}`,
		"not json":         `not json`,
	} {
		t.Run(name, func(t *testing.T) {
			recorder := do(t, handler, http.MethodPost, "/api/v4/mods/Report", body, writeAuth())
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("code = %d, want 400", recorder.Code)
			}
		})
	}
}

func TestReportRequiresAuth(t *testing.T) {
	server, path, _ := newWriteServer(t)
	seed(t, path)
	body := `{"modId":"100","concernCategoryId":"7","concernCategoryName":"ReasonsOther","concernContent":"x"}`
	recorder := do(t, server.Handler(), http.MethodPost, "/api/v4/mods/Report", body, nil)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", recorder.Code)
	}
}

func TestPublishCreatesAMod(t *testing.T) {
	server, _, uploads := newWriteServer(t)
	handler := server.Handler()

	recorder := postMultipart(t, handler, "/api/v4/mods/Publish", map[string]string{
		"gameTitleId":   "2",
		"modType":       "15",
		"modName":       "The Roman Dawn",
		"description":   "<p>desc</p>",
		"changeList":    "<p>change</p>",
		"modVisibility": "Unlisted",
		"modDiscussion": "true",
		"modTags":       "15,16",
		"ImageMetaData": `[{"imageId":null,"displayIndex":0,"imageName":"01.png","imageThumbnail":""}]`,
	}, []uploadFile{
		{Field: "modFiles", Name: "mod.zip", Content: "archive"},
		{Field: "modImages", Name: "01.png", Content: "screenshot"},
	}, writeAuth())
	if recorder.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		ErrorCode    int64   `json:"errorCode"`
		ErrorMessage string  `json:"errorMessage"`
		ResultKey    string  `json:"resultKey"`
		ResultValue  string  `json:"resultValue"`
		ResultJSON   *string `json:"resultJson"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("body: %v", err)
	}
	if response.ErrorCode != 0 || response.ErrorMessage != "0" || response.ResultKey != "ModId" {
		t.Fatalf("response = %+v", response)
	}
	if response.ResultValue == "" || response.ResultJSON != nil {
		t.Fatalf("response = %+v", response)
	}

	var modID int64
	for _, digit := range response.ResultValue {
		modID = modID*10 + int64(digit-'0')
	}
	if modID <= 0 {
		t.Fatalf("resultValue = %q", response.ResultValue)
	}

	got := detail(t, handler, modID)
	if got.ModName != "The Roman Dawn" || got.GameTitleID != 2 {
		t.Fatalf("detail = %+v", got)
	}
	if got.ModStatusID != 2 {
		t.Fatalf("modStatusId = %d, want 2 (Published) for Unlisted", got.ModStatusID)
	}
	if got.ModVisibility == nil || *got.ModVisibility != "Unlisted" {
		t.Fatalf("modVisibility = %v", got.ModVisibility)
	}
	if !got.AllowDiscussion {
		t.Fatal("allowDiscussion = false")
	}
	if len(got.ModTagIDs) != 2 {
		t.Fatalf("modTagIds = %v", got.ModTagIDs)
	}
	if got.FileURL == nil || *got.FileURL == "" {
		t.Fatalf("fileUrl = %v", got.FileURL)
	}
	if len(got.ImageURLs) != 1 {
		t.Fatalf("imageUrls = %+v", got.ImageURLs)
	}
	if got.ImageURLs[0].ImageThumbnail == "" {
		t.Fatalf("image thumbnail not stored: %+v", got.ImageURLs[0])
	}
	if got.IsOwner == nil || !*got.IsOwner {
		t.Fatalf("isOwner = %v", got.IsOwner)
	}
	if uploads == "" {
		t.Fatal("no upload dir")
	}
}

func TestPublishUpdatesAMod(t *testing.T) {
	server, path, _ := newWriteServer(t)
	seedOwnedMod(t, path, 400)
	handler := server.Handler()

	recorder := postMultipart(t, handler, "/api/v4/mods/Publish", map[string]string{
		"modId":         "400",
		"gameTitleId":   "2",
		"modType":       "16",
		"modName":       "Renamed",
		"description":   "new",
		"modVisibility": "Private",
		"modTags":       "16",
		"ImageMetaData": `[{"imageId":"400-0","displayIndex":0,"imageName":"01.png","imageThumbnail":"kept.png"}]`,
	}, nil, writeAuth())
	if recorder.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"resultValue":"400"`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}

	got := detail(t, handler, 400)
	if got.ModName != "Renamed" {
		t.Fatalf("modName = %q", got.ModName)
	}
	if got.ModStatusID != 2 {
		t.Fatalf("modStatusId = %d, want 2 (Published) for Private", got.ModStatusID)
	}
	if len(got.ModTagIDs) != 1 || got.ModTagIDs[0] != 16 {
		t.Fatalf("modTagIds = %v", got.ModTagIDs)
	}
	// The image had no upload, so the client thumbnail is kept. The response
	// carries it as an absolute URL of the media route.
	if len(got.ImageURLs) != 1 || !strings.HasSuffix(got.ImageURLs[0].ImageThumbnail, "/kept.png") {
		t.Fatalf("imageUrls = %+v", got.ImageURLs)
	}
}

func TestPublishRejectsAModOfAnotherUser(t *testing.T) {
	server, path, _ := newWriteServer(t)
	seed(t, path)
	recorder := postMultipart(t, server.Handler(), "/api/v4/mods/Publish", map[string]string{
		"modId":       "100",
		"gameTitleId": "2",
		"modName":     "Hijack",
	}, nil, writeAuth())
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", recorder.Code)
	}
}

func TestPublishRejectsAnUnknownMod(t *testing.T) {
	server, _, _ := newWriteServer(t)
	recorder := postMultipart(t, server.Handler(), "/api/v4/mods/Publish", map[string]string{
		"modId":       "999",
		"gameTitleId": "2",
		"modName":     "Ghost",
	}, nil, writeAuth())
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", recorder.Code)
	}
}

func TestPublishRejectsABadTag(t *testing.T) {
	server, _, _ := newWriteServer(t)
	recorder := postMultipart(t, server.Handler(), "/api/v4/mods/Publish", map[string]string{
		"gameTitleId": "2",
		"modName":     "Bad tag",
		"modTags":     "999",
	}, nil, writeAuth())
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", recorder.Code)
	}
}

func TestPublishRejectsABadForm(t *testing.T) {
	server, _, _ := newWriteServer(t)
	handler := server.Handler()
	for name, fields := range map[string]map[string]string{
		"no game":        {"modName": "x"},
		"no name":        {"gameTitleId": "2"},
		"bad visibility": {"gameTitleId": "2", "modName": "x", "modVisibility": "Secret"},
		"bad game id":    {"gameTitleId": "abc", "modName": "x"},
		"bad metadata":   {"gameTitleId": "2", "modName": "x", "ImageMetaData": "nope"},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := postMultipart(t, handler, "/api/v4/mods/Publish", fields, nil, writeAuth())
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("code = %d, want 400", recorder.Code)
			}
		})
	}
}

func TestPublishRejectsANonMultipartBody(t *testing.T) {
	server, _, _ := newWriteServer(t)
	recorder := do(t, server.Handler(), http.MethodPost, "/api/v4/mods/Publish",
		`{"gameTitleId":2,"modName":"x"}`, writeAuth())
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", recorder.Code)
	}
}

func TestPublishRequiresAuth(t *testing.T) {
	server, _, _ := newWriteServer(t)
	recorder := postMultipart(t, server.Handler(), "/api/v4/mods/Publish", map[string]string{
		"gameTitleId": "2",
		"modName":     "x",
	}, nil, nil)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", recorder.Code)
	}
}

func TestPublishWithoutUploadDirFails(t *testing.T) {
	server, _ := newTestServer(t)
	recorder := postMultipart(t, server.Handler(), "/api/v4/mods/Publish", map[string]string{
		"gameTitleId": "2",
		"modName":     "No storage",
	}, []uploadFile{{Field: "modFiles", Name: "mod.zip", Content: "archive"}}, writeAuth())
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", recorder.Code)
	}
}

func TestPublishWithoutUploadDirFailsOnAnImage(t *testing.T) {
	server, _ := newTestServer(t)
	recorder := postMultipart(t, server.Handler(), "/api/v4/mods/Publish", map[string]string{
		"gameTitleId":   "2",
		"modName":       "No storage",
		"ImageMetaData": `[{"imageId":null,"displayIndex":0,"imageName":"01.png","imageThumbnail":""}]`,
	}, []uploadFile{{Field: "modImages", Name: "01.png", Content: "screenshot"}}, writeAuth())
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", recorder.Code)
	}
}
