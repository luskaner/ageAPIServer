package ageAPIServer

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luskaner/ageAPIServer/internal/logger"
	"github.com/luskaner/ageAPIServer/internal/models"
)

func TestMain(m *testing.M) {
	logger.Initialize(io.Discard)
	m.Run()
}

// newTestServer builds a server over a temporary database and returns the path
// so a test can seed it through a second handle.
func newTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	server, err := New(Config{DBPath: path})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })
	return server, path
}

func seed(t *testing.T, path string) {
	t.Helper()
	stores, err := models.Open(models.Options{Path: path, MaxOpenConns: 1})
	if err != nil {
		t.Fatalf("open seed stores: %v", err)
	}
	defer func() { _ = stores.Close() }()

	ctx := context.Background()
	user, err := stores.Users.EnsureUser(ctx, "76561198000000001", "Tester", "")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	mods := []models.ModInput{
		{ModID: 100, GameTitleID: 2, ModName: "Chieftains", ModVersion: 1.2, ModTypeID: 1,
			ModStatusID: 1, CreatorID: user.UserID, Popular: 50, Downloads: 10, TagIDs: []int64{1, 2}},
		{ModID: 200, GameTitleID: 2, ModName: "Balance", ModVersion: 2.0, ModTypeID: 2,
			ModStatusID: 2, CreatorID: user.UserID, Popular: 90, Downloads: 40, TagIDs: []int64{2}},
	}
	for _, mod := range mods {
		if err := stores.Mods.Upsert(ctx, mod); err != nil {
			t.Fatalf("seed mod %d: %v", mod.ModID, err)
		}
	}
}

func do(t *testing.T, handler http.Handler, method string, path string, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestGamesReturnsTheCatalog(t *testing.T) {
	server, _ := newTestServer(t)
	recorder := do(t, server.Handler(), http.MethodGet, "/api/v4/mods/Games", "", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var games []models.Game
	if err := json.Unmarshal(recorder.Body.Bytes(), &games); err != nil {
		t.Fatalf("not json: %v", err)
	}
	if len(games) != 5 {
		t.Fatalf("got %d games", len(games))
	}
}

func TestFindReturnsModList(t *testing.T) {
	server, path := newTestServer(t)
	seed(t, path)

	body := `{"q":"","filter":0,"sort":"popular","order":"desc","start":1,"count":30,"game":2,"modid":""}`
	recorder := do(t, server.Handler(), http.MethodPost, "/api/v4/Mods/Find/", body, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		SearchParams map[string]any `json:"searchParams"`
		ModList      []models.Mod   `json:"modList"`
		TotalCount   *int64         `json:"totalCount"`
		IsLoggedOn   bool           `json:"isLoggedOn"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("not json: %v", err)
	}
	if len(response.ModList) != 2 {
		t.Fatalf("got %d mods", len(response.ModList))
	}
	if response.TotalCount == nil || *response.TotalCount != 2 {
		t.Fatalf("totalCount = %v", response.TotalCount)
	}
	if response.IsLoggedOn {
		t.Fatal("anonymous request reported as logged on")
	}
	if response.ModList[0].ModID != 200 {
		t.Fatalf("popular sort put %d first", response.ModList[0].ModID)
	}
}

func TestFindRejectsMissingGame(t *testing.T) {
	server, _ := newTestServer(t)
	recorder := do(t, server.Handler(), http.MethodPost, "/api/v4/Mods/Find/", `{"count":10}`, nil)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", recorder.Code)
	}
}

func TestDetailReturnsNotFound(t *testing.T) {
	server, _ := newTestServer(t)
	recorder := do(t, server.Handler(), http.MethodGet, "/api/v4/Mods/Detail/999999", "", nil)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", recorder.Code)
	}
}

func TestNormalizationIsTransparent(t *testing.T) {
	server, _ := newTestServer(t)
	for _, path := range []string{
		"/API/V4/MODS/GAMES",
		"//api//v4//mods//games//",
		"/api/v4/mods/Games/",
	} {
		recorder := do(t, server.Handler(), http.MethodGet, path, "", nil)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: code = %d", path, recorder.Code)
		}
	}
}

func TestUnknownPathIsJSON404(t *testing.T) {
	server, _ := newTestServer(t)
	recorder := do(t, server.Handler(), http.MethodGet, "/nope", "", nil)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "errorCode") {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestHealthz(t *testing.T) {
	server, _ := newTestServer(t)
	recorder := do(t, server.Handler(), http.MethodGet, "/healthz", "", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("code = %d", recorder.Code)
	}
}

func TestMountUnderPrefix(t *testing.T) {
	server, _ := newTestServer(t)
	mux := http.NewServeMux()
	mux.Handle("/aoeMods/", http.StripPrefix("/aoeMods", server.Handler()))

	request := httptest.NewRequest(http.MethodGet, "/aoeMods/api/v4/mods/Games", nil)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", recorder.Code, recorder.Body.String())
	}

	// The bare prefix must answer 404 JSON, not panic.
	request = httptest.NewRequest(http.MethodGet, "/aoeMods/", nil)
	recorder = httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("root of the mount: code = %d", recorder.Code)
	}
}

func TestTextModerationAllows(t *testing.T) {
	server, _ := newTestServer(t)
	body := `{"conversationId":"0","textContent":"Player 1","language":"es-ES","textType":"SanitisationInput","includeResponse":true}`
	recorder := do(t, server.Handler(), http.MethodPost, "/textmoderation", body,
		map[string]string{"Authorization": "Steam1.0 test-token"})
	if recorder.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"filterResult":"Allow"`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestTextModerationRejectsBadTextType(t *testing.T) {
	server, _ := newTestServer(t)
	body := `{"conversationId":"0","textContent":"x","language":"en","textType":"Nope"}`
	recorder := do(t, server.Handler(), http.MethodPost, "/textmoderation", body,
		map[string]string{"Authorization": "Steam1.0 test-token"})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", recorder.Code)
	}
}

func TestRoutesInventoryIsServed(t *testing.T) {
	server, _ := newTestServer(t)
	if len(server.Routes()) == 0 {
		t.Fatal("empty inventory")
	}
	for _, route := range server.Routes() {
		if route.Path == "" || route.Method == "" {
			t.Fatalf("incomplete route: %+v", route)
		}
	}
}

// TestAuthenticatedFlow walks the endpoints that need a session: subscribe,
// download, rate, reviews, installed, my, the legacy install/uninstall/flag and
// the polls. It is the HTTP-level counterpart of the store tests.
func TestAuthenticatedFlow(t *testing.T) {
	server, path := newTestServer(t)
	seed(t, path)
	handler := server.Handler()
	auth := map[string]string{"Authorization": "Steam1.0 c21va2UtdG9rZW4="}

	// Subscribe does not count; Download counts once.
	recorder := do(t, handler, http.MethodGet, "/api/v4/Mods/Subscribe/100", "", auth)
	if recorder.Code != http.StatusOK {
		t.Fatalf("subscribe: %d %s", recorder.Code, recorder.Body.String())
	}
	var info models.DownloadInfo
	if err := json.Unmarshal(recorder.Body.Bytes(), &info); err != nil {
		t.Fatalf("subscribe body: %v", err)
	}
	if info.DownloadCount != 10 {
		t.Fatalf("subscribe changed the counter: %d", info.DownloadCount)
	}
	if info.DownloadURL == "" {
		t.Fatal("empty downloadUrl")
	}

	recorder = do(t, handler, http.MethodGet, "/api/v4/Mods/Download/100/", "", auth)
	if recorder.Code != http.StatusOK {
		t.Fatalf("download: %d %s", recorder.Code, recorder.Body.String())
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &info); err != nil {
		t.Fatalf("download body: %v", err)
	}
	if info.DownloadCount != 11 {
		t.Fatalf("download count = %d, want 11", info.DownloadCount)
	}

	// Rate refreshes the aggregates.
	recorder = do(t, handler, http.MethodPost, "/api/v4/Mods/Rate/", `{"modId":100,"rating":5}`, auth)
	if recorder.Code != http.StatusOK {
		t.Fatalf("rate: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = do(t, handler, http.MethodGet, "/api/v4/Mods/Detail/100", "", auth)
	if recorder.Code != http.StatusOK {
		t.Fatalf("detail: %d %s", recorder.Code, recorder.Body.String())
	}
	var detail models.Detail
	if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil {
		t.Fatalf("detail body: %v", err)
	}
	if detail.CurrentRating != 5 || detail.TotalRatings != 1 {
		t.Fatalf("aggregates: rating=%v total=%d", detail.CurrentRating, detail.TotalRatings)
	}
	if !detail.IsLoggedOn {
		t.Fatal("authenticated request reported as anonymous")
	}

	// Reviews accepts ratingNum 7 without validating it against 1..5.
	recorder = do(t, handler, http.MethodPost, "/api/v4/mods/Reviews",
		`{"modId":"100","page":1,"count":5,"ratingNum":7,"ratingLabel":"ratings"}`, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("reviews: %d %s", recorder.Code, recorder.Body.String())
	}

	// Installed and My answer with a ModList.
	for _, endpoint := range []string{"/api/v4/Mods/Installed/", "/api/v4/Mods/My/"} {
		recorder = do(t, handler, http.MethodPost, endpoint, `{"game":2}`, auth)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", endpoint, recorder.Code, recorder.Body.String())
		}
		if !strings.Contains(recorder.Body.String(), `"modList"`) {
			t.Fatalf("%s body = %s", endpoint, recorder.Body.String())
		}
	}

	// Legacy install, uninstall and flag.
	recorder = do(t, handler, http.MethodPost, "/api/v1/Mods/Install/", `{"id":100}`, auth)
	if recorder.Code != http.StatusOK {
		t.Fatalf("install: %d %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"value"`) {
		t.Fatalf("install body = %s", recorder.Body.String())
	}
	recorder = do(t, handler, http.MethodPost, "/api/v1/Mods/Uninstall/", `{"id":100}`, auth)
	if recorder.Code != http.StatusOK {
		t.Fatalf("uninstall: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = do(t, handler, http.MethodPost, "/api/v1/Mods/Flag/",
		`{"boolValue":true,"id":100,"stringValue":"Poor Performance"}`, auth)
	if recorder.Code != http.StatusOK {
		t.Fatalf("flag: %d %s", recorder.Code, recorder.Body.String())
	}

	// Unsubscribe answers the boolean of the schema.
	recorder = do(t, handler, http.MethodGet, "/api/v4/Mods/Unsubscribe/100", "", auth)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unsubscribe: %d %s", recorder.Code, recorder.Body.String())
	}
	if strings.TrimSpace(recorder.Body.String()) != "true" {
		t.Fatalf("unsubscribe body = %q, want true", recorder.Body.String())
	}

	// The legacy detail body carries only modid.
	recorder = do(t, handler, http.MethodPost, "/api/v1/Mods/Detail/", `{"modid":100}`, auth)
	if recorder.Code != http.StatusOK {
		t.Fatalf("v1 detail: %d %s", recorder.Code, recorder.Body.String())
	}

	// The legacy find walks pages with start 0 and 1.
	for _, start := range []string{"0", "1"} {
		body := `{"count":45,"game":2,"q":"","start":` + start + `}`
		recorder = do(t, handler, http.MethodPost, "/api/v1/Mods/FindAsUser/", body, auth)
		if recorder.Code != http.StatusOK {
			t.Fatalf("findasuser start=%s: %d %s", start, recorder.Code, recorder.Body.String())
		}
	}
}
