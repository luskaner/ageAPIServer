package ageAPIServer

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/luskaner/ageAPIServer/internal/models"
)

// xblHeader is the Authorization header of the Microsoft Store clients: the
// prefix, the publisher user id and the signed XToken, as the AOE3 capture
// sends it. Only the id is identity here.
const xblHeader = "XBL3.0 x=3548033865991315209;eyJhbGciOiJSU0EtT0FFUCIsImVuYyI6IkExMjhDQkMtSFMyNTYiLCJ6aXAiOiJERUYi"

// xblAuth is the header map the XBL requests use.
func xblAuth() map[string]string {
	return map[string]string{"Authorization": xblHeader}
}

// seedXBLMod publishes a mod owned by the user behind the XBL header, so My
// and Delete list and remove it.
func seedXBLMod(t *testing.T, path string) int64 {
	t.Helper()
	stores, err := models.Open(models.Options{Path: path, MaxOpenConns: 1})
	if err != nil {
		t.Fatalf("open seed stores: %v", err)
	}
	defer func() { _ = stores.Close() }()
	ctx := context.Background()
	user, err := stores.Users.EnsureUser(ctx, "3548033865991315209", "", "")
	if err != nil {
		t.Fatalf("seed xbl user: %v", err)
	}
	modID, err := stores.Mods.Publish(ctx, models.PublishInput{
		GameTitleID: 3,
		ModName:     "Test Mod",
		Description: "A test mod",
		TagIDs:      []int64{11},
		CreatorID:   user.UserID,
	})
	if err != nil {
		t.Fatalf("seed xbl mod: %v", err)
	}
	return modID
}

// TestXBLAuthenticationRunsMyAndDelete walks the AOE3 capture: My and Publish
// and Delete authenticate with the XBL3.0 header, My echoes the caller as the
// creator with the resolved count 10, and Delete answers the bare true even
// with the extra game field of the capture.
func TestXBLAuthenticationRunsMyAndDelete(t *testing.T) {
	server, path, _ := newWriteServer(t)
	handler := server.Handler()
	modID := seedXBLMod(t, path)

	// My: the echo of the capture carries the caller as the creator and the
	// resolved paging of the endpoint.
	mine := do(t, handler, http.MethodPost, "/api/v4/Mods/My/", `{"game":3}`, xblAuth())
	if mine.Code != http.StatusOK {
		t.Fatalf("my: %d %s", mine.Code, mine.Body.String())
	}
	var list struct {
		SearchParams sharedSearchParams `json:"searchParams"`
		ModList      []map[string]any   `json:"modList"`
		TotalCount   *int64             `json:"totalCount"`
	}
	if err := json.Unmarshal(mine.Body.Bytes(), &list); err != nil {
		t.Fatalf("my body: %v", err)
	}
	if list.SearchParams.Creator == nil || *list.SearchParams.Creator != "3548033865991315209" {
		t.Fatalf("creator echo = %v, want the caller", list.SearchParams.Creator)
	}
	if list.SearchParams.Count != 10 || list.SearchParams.Order == nil || *list.SearchParams.Order != "DESC" ||
		list.SearchParams.Sort == nil || *list.SearchParams.Sort != "popular" ||
		list.SearchParams.ModID != "0" {
		t.Fatalf("searchParams = %+v", list.SearchParams)
	}
	if len(list.ModList) != 1 || list.ModList[0]["isOwner"] != true {
		t.Fatalf("my rows = %v", list.ModList)
	}
	// The stored mod of the capture renders visibilities and counters that
	// the publish defaults added, and the tags as names plus ids.
	if list.ModList[0]["modTags"] == nil || list.ModList[0]["modTagIds"] == nil {
		t.Fatalf("my row tags = %v", list.ModList[0])
	}

	// Delete carries an extra game field and answers the bare true.
	deleted := do(t, handler, http.MethodPost, "/api/v4/Mods/Delete/",
		`{"modId":`+itoa(modID)+`,"game":3}`, xblAuth())
	if deleted.Code != http.StatusOK || strings.TrimSpace(deleted.Body.String()) != "true" {
		t.Fatalf("delete: %d %s", deleted.Code, deleted.Body.String())
	}
}

// sharedSearchParams mirrors the echo fields the captures pin.
type sharedSearchParams struct {
	Q       *string `json:"q"`
	Creator *string `json:"creator"`
	Filter  int64   `json:"filter"`
	Sort    *string `json:"sort"`
	Order   *string `json:"order"`
	Start   int64   `json:"start"`
	Count   int64   `json:"count"`
	Game    int64   `json:"game"`
	ModID   string  `json:"modid"`
}

func itoa(value int64) string {
	return strconv.FormatInt(value, 10)
}

// TestInstalledEchoResolvesTheDefaults pins the AOE3 installed capture: a
// body that only carries the game resolves the echo to creator "", sort
// popular, order DESC, count 1000 and modid "0", and an empty page answers
// totalCount null.
func TestInstalledEchoResolvesTheDefaults(t *testing.T) {
	server, _, _ := newWriteServer(t)
	recorder := do(t, server.Handler(), http.MethodPost, "/api/v4/Mods/Installed/", `{"game":3}`, xblAuth())
	if recorder.Code != http.StatusOK {
		t.Fatalf("installed: %d %s", recorder.Code, recorder.Body.String())
	}
	var list struct {
		SearchParams sharedSearchParams `json:"searchParams"`
		TotalCount   *int64             `json:"totalCount"`
		ModList      []json.RawMessage  `json:"modList"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &list); err != nil {
		t.Fatalf("body: %v", err)
	}
	if list.SearchParams.Creator == nil || *list.SearchParams.Creator != "" {
		t.Fatalf("creator echo = %v, want the empty string", list.SearchParams.Creator)
	}
	if list.SearchParams.Count != 1000 || list.SearchParams.Start != 1 ||
		list.SearchParams.ModID != "0" ||
		list.SearchParams.Order == nil || *list.SearchParams.Order != "DESC" ||
		list.SearchParams.Sort == nil || *list.SearchParams.Sort != "popular" {
		t.Fatalf("searchParams = %+v", list.SearchParams)
	}
	if list.TotalCount != nil {
		t.Fatalf("totalCount = %v, want null on an empty page", list.TotalCount)
	}
}

// TestFindEchoNormalizesModIdAndOrder pins the normalization the AOE3 Find
// capture shows: the order echoes upper-cased (`desc` in, `DESC` out) and the
// mod id echoes as the string "0" both when the body omits it and when it
// sends the number 0.
func TestFindEchoNormalizesModIdAndOrder(t *testing.T) {
	server, path := newTestServer(t)
	seed(t, path)

	recorder := do(t, server.Handler(), http.MethodPost, "/api/v4/Mods/Find/",
		`{"game":3,"start":1,"count":30,"order":"desc","sort":"createDate"}`, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("find: %d %s", recorder.Code, recorder.Body.String())
	}
	params := decodeMap(t, recorder.Body.Bytes())["searchParams"].(map[string]any)
	if params["modid"] != "0" {
		t.Fatalf("modid echo = %v, want the string", params["modid"])
	}
	if params["order"] != "DESC" {
		t.Fatalf("order echo = %v, want DESC", params["order"])
	}

	recorder = do(t, server.Handler(), http.MethodPost, "/api/v4/Mods/Find/",
		`{"game":3,"modid":0}`, nil)
	params = decodeMap(t, recorder.Body.Bytes())["searchParams"].(map[string]any)
	if params["modid"] != "0" {
		t.Fatalf("numeric modid echo = %v, want the string", params["modid"])
	}
}

// TestFeaturedEchoResolvesTheDefaults pins the Featured captures: the AOM
// client sends only the game and the echo resolves sort popular and count 10,
// while the website sends its sort and count explicitly (recentpopular/3) and
// echoes them as sent.
func TestFeaturedEchoResolvesTheDefaults(t *testing.T) {
	server, _, _ := newWriteServer(t)
	recorder := do(t, server.Handler(), http.MethodPost, "/api/v4/mods/Featured", `{"game":1001}`, xblAuth())
	if recorder.Code != http.StatusOK {
		t.Fatalf("featured: %d %s", recorder.Code, recorder.Body.String())
	}
	params := decodeMap(t, recorder.Body.Bytes())["searchParams"].(map[string]any)
	if params["sort"] != "popular" || params["count"] != float64(10) || params["modid"] != "0" {
		t.Fatalf("client searchParams = %v", params)
	}
	if params["creator"] != nil {
		t.Fatalf("creator echo = %v, want null (only My echoes the caller)", params["creator"])
	}

	recorder = do(t, server.Handler(), http.MethodPost, "/api/v4/mods/Featured",
		`{"game":1001,"sort":"recentpopular","count":3,"q":"aoe1"}`, xblAuth())
	params = decodeMap(t, recorder.Body.Bytes())["searchParams"].(map[string]any)
	if params["sort"] != "recentpopular" || params["count"] != float64(3) || params["q"] != "aoe1" {
		t.Fatalf("website searchParams = %v", params)
	}
}

// TestRelatedHidesTheTags pins the Related capture: the rows carry modTags
// null while Find and My carry the names.
func TestRelatedHidesTheTags(t *testing.T) {
	server, path := newTestServer(t)
	seed(t, path)

	recorder := do(t, server.Handler(), http.MethodGet, "/api/v4/mods/Related/100", "", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("related: %d %s", recorder.Code, recorder.Body.String())
	}
	var list struct {
		ModList []map[string]any `json:"modList"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &list); err != nil {
		t.Fatalf("body: %v", err)
	}
	for _, row := range list.ModList {
		if value, present := row["modTags"]; !present || value != nil {
			t.Fatalf("related modTags = %v, want null", value)
		}
	}
}

// TestTextModerationAnswersTheBaseLanguage pins the subtag of the detected
// language: the carries answer a base tag ("en"), never the regional input.
func TestTextModerationAnswersTheBaseLanguage(t *testing.T) {
	server, _, _ := newWriteServer(t)
	recorder := do(t, server.Handler(), http.MethodPost, "/textmoderation",
		`{"conversationId":"0","includeResponse":true,"language":"es-ES","textContent":"Test Mod","textType":"SanitisationInput"}`,
		xblAuth())
	if recorder.Code != http.StatusOK {
		t.Fatalf("moderation: %d %s", recorder.Code, recorder.Body.String())
	}
	response := decodeMap(t, recorder.Body.Bytes())
	if response["language"] != "es" {
		t.Fatalf("language = %v, want the base tag", response["language"])
	}
	if response["filterResult"] != "Allow" {
		t.Fatalf("filterResult = %v", response["filterResult"])
	}
}
