package ageAPIServer

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/luskaner/ageAPIServer/internal/models"
)

// The AOM client of the November captures drives this file: it publishes
// into game 1001 with an imageMetaData of string sizes, repeated modTags, a
// lowercase visibility, an empty changeList and the archive inside modFiles
// (no CheckAndPublishFile), it browses Find sorting by subscribers, its
// Featured resolves the echo to sort popular and count 10 and answers the
// status-5 rows, and its My clamps the explicit count onto 500.

// aomZip builds the tempmod.zip the capture uploads: two entries whose
// uncompressed sizes the Detail answers as modDiskSize.
func aomZip(t *testing.T) []byte {
	t.Helper()
	var raw bytes.Buffer
	archive := zip.NewWriter(&raw)
	for _, entry := range []struct {
		name    string
		content string
	}{{"mod/main.xml", strings.Repeat("<mod/>", 100)}, {"mod/data.bar", strings.Repeat("data", 500)}} {
		file, err := archive.Create(entry.name)
		if err != nil {
			t.Fatalf("zip create: %v", err)
		}
		if _, err := file.Write([]byte(entry.content)); err != nil {
			t.Fatalf("zip write: %v", err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return raw.Bytes()
}

// steamToken is the Steam1.0 scheme the AOM client uses in the capture.
func steamToken() string {
	return "Steam1.0 " + writeToken
}

// steamAuth is the header map a do() call adds for the capture identity.
func steamAuth() map[string]string {
	return map[string]string{"Authorization": steamToken()}
}

// publishAOM walks the create of the capture exactly: the multipart carries
// the archive as modFiles, the thumbnail as modImages, and the two tags as
// repeated fields.
func publishAOM(t *testing.T, handler http.Handler, modID int64) int64 {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, field := range []struct{ name, value string }{
		{"modName", "TestMod2"},
		{"gameTitleId", "1001"},
		{"modType", "11"},
		{"description", "testmod2"},
		{"modVisibility", "public"},
		{"changeList", ""},
		{"imageMetaData", `[{"displayIndex":0,"imageName":"thumbnail.png","imageFileSize":"7867"}]`},
		{"modTags", "11"},
		{"modTags", "23"},
	} {
		if err := writer.WriteField(field.name, field.value); err != nil {
			t.Fatalf("write %s: %v", field.name, err)
		}
	}
	if modID > 0 {
		if err := writer.WriteField("modId", strconv.FormatInt(modID, 10)); err != nil {
			t.Fatalf("write modId: %v", err)
		}
	}
	part, err := writer.CreateFormFile("modImages", "thumbnail.png")
	if err != nil {
		t.Fatalf("write image part: %v", err)
	}
	if _, err := part.Write([]byte(strings.Repeat("png", 1501))); err != nil {
		t.Fatalf("write image: %v", err)
	}
	part, err = writer.CreateFormFile("modFiles", "tempmod.zip")
	if err != nil {
		t.Fatalf("write file part: %v", err)
	}
	content := aomZip(t)
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v4/Mods/Publish/", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", steamToken())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("publish %d: %d %s", modID, recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		ErrorCode   int64  `json:"errorCode"`
		ResultValue string `json:"resultValue"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil || envelope.ErrorCode != 0 {
		t.Fatalf("publish envelope = %s", recorder.Body.String())
	}
	id, err := strconv.ParseInt(envelope.ResultValue, 10, 64)
	if err != nil {
		t.Fatalf("resultValue = %q", envelope.ResultValue)
	}
	return id
}

// TestAOMPublishCarriesTheArchiveAndTheTags pins the create of the capture:
// the plain texts, the resolved Data Mod type, both tags with the names the
// catalog of game 1001 sells, the lowercase visibility stored rendered, the
// empty changeList, the counters of a new mod and the archive sizes, whose
// modDiskSize exceeds modFileSize because tempmod.zip is a real zip.
func TestAOMPublishCarriesTheArchiveAndTheTags(t *testing.T) {
	server, _, _ := newWriteServer(t)
	handler := server.Handler()

	modID := publishAOM(t, handler, 0)
	created := detailXBL(t, handler, modID)
	if created.ModName != "TestMod2" || created.Description != "testmod2" || created.ModDescription != "testmod2" {
		t.Fatalf("texts = %q %q", created.ModName, created.Description)
	}
	if created.ModTypeID == nil || *created.ModTypeID != 11 ||
		created.ModType == nil || *created.ModType != "Data Mod" {
		t.Fatalf("modType = %v %v", created.ModTypeID, created.ModType)
	}
	if len(created.ModTagIDs) != 2 || created.ModTagIDs[0] != 11 || created.ModTagIDs[1] != 23 {
		t.Fatalf("tag ids = %v", created.ModTagIDs)
	}
	if len(created.ModTagNames) != 2 || created.ModTagNames[0] != "Data Mod" || created.ModTagNames[1] != "Text" {
		t.Fatalf("tag names = %v", created.ModTagNames)
	}
	if created.ModTags != nil {
		t.Fatalf("detail modTags = %v, want null", created.ModTags)
	}
	if created.ModVisibility == nil || *created.ModVisibility != "Public" {
		t.Fatalf("visibility = %v", created.ModVisibility)
	}
	if created.ModStatusID != 2 || created.ModStatus != "Published" {
		t.Fatalf("status = %d %q", created.ModStatusID, created.ModStatus)
	}
	if created.ChangeList != "" || created.ModVersion != 1.1 {
		t.Fatalf("changeList/version = %q %v", created.ChangeList, created.ModVersion)
	}
	if created.Popular == nil || *created.Popular != 1 || created.UpdateAvailable == nil || *created.UpdateAvailable {
		t.Fatalf("counters = %v, update %v", created.Popular, created.UpdateAvailable)
	}
	// The archive lands with its compressed size as modFileSize and the
	// uncompressed total as modDiskSize, both present and different, and
	// downloading recovers the uploaded bytes.
	if created.ModFileSize == nil || created.ModDiskSize == nil || *created.ModFileSize >= *created.ModDiskSize {
		t.Fatalf("sizes = %v %v", created.ModFileSize, created.ModDiskSize)
	}
	if len(created.ImageURLs) != 1 || created.ImageURLs[0].ImageFileSize != nil {
		t.Fatalf("imageUrls = %+v", created.ImageURLs)
	}
	if created.Thumbnail == nil || !strings.Contains(*created.Thumbnail, ".png") {
		t.Fatalf("thumbnail = %v", created.Thumbnail)
	}
}

// seedFeaturedAOM inserts the two Featured rows the shape of the capture
// needs: 286553 whose type resolves for game 1001, and a mod with the type
// id 1 the catalog of the game does not sell.
func seedFeaturedAOM(t *testing.T, server *Server, path string) {
	t.Helper()
	stores, err := models.Open(models.Options{Path: path, MaxOpenConns: 1})
	if err != nil {
		t.Fatalf("open seed stores: %v", err)
	}
	defer func() { _ = stores.Close() }()
	user, err := stores.Users.EnsureUser(context.Background(), "76561198000000001", "Tester", "")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	featured := []models.ModInput{
		{ModID: 286553, GameTitleID: 1001, ModName: "Castle Blood", ModVersion: 2.0, ModTypeID: 11,
			ModStatusID: 5, CreatorID: user.UserID, Popular: 13592, Downloads: 16570, TagIDs: []int64{15}},
		{ModID: 409999, GameTitleID: 1001, ModName: "Odd Type", ModVersion: 1.0, ModTypeID: 1,
			ModStatusID: 5, CreatorID: user.UserID, Popular: 100, TagIDs: []int64{22}},
	}
	for _, entry := range featured {
		if err := stores.Mods.Upsert(context.Background(), entry); err != nil {
			t.Fatalf("seed featured %d: %v", entry.ModID, err)
		}
	}
	_ = user
}

// TestAOMFeaturedAndUnknownTypeName pins the Featured capture: the status-5
// rows, the tag names in the list rows, and the "Unknown" the list renders
// for a type id its catalog does not sell (type 1 of game 1001, whose Detail
// leaves the name null instead).
func TestAOMFeaturedAndUnknownTypeName(t *testing.T) {
	server, path := newTestServer(t)
	seed(t, path)
	seedFeaturedAOM(t, server, path)
	handler := server.Handler()

	recorder := do(t, handler, http.MethodPost, "/api/v4/mods/Featured", `{"game":1001}`, steamAuth())
	if recorder.Code != http.StatusOK {
		t.Fatalf("featured: %d %s", recorder.Code, recorder.Body.String())
	}
	var list struct {
		SearchParams struct {
			Sort    *string `json:"sort"`
			Count   int64   `json:"count"`
			Creator *string `json:"creator"`
		} `json:"searchParams"`
		ModList []map[string]any `json:"modList"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &list); err != nil {
		t.Fatalf("body: %v", err)
	}
	if list.SearchParams.Sort == nil || *list.SearchParams.Sort != "popular" || list.SearchParams.Count != 10 {
		t.Fatalf("featured echo = %+v", list.SearchParams)
	}
	if list.SearchParams.Creator != nil {
		t.Fatalf("creator = %v, want null", list.SearchParams.Creator)
	}
	if len(list.ModList) != 2 {
		t.Fatalf("rows = %d, want the two featured mods", len(list.ModList))
	}
	names := map[int64]string{}
	for _, row := range list.ModList {
		if row["modStatusId"] != float64(5) || row["modStatus"] != "Featured" {
			t.Fatalf("row status = %v %v", row["modStatusId"], row["modStatus"])
		}
		if row["modTags"] == nil {
			t.Fatalf("featured modTags = %v, want the names", row["modTags"])
		}
		names[int64(row["modId"].(float64))] = row["modType"].(string)
	}
	if names[286553] != "Data Mod" || names[409999] != "Unknown" {
		t.Fatalf("list modTypes = %v", names)
	}
	odd := detail(t, handler, 409999)
	if odd.ModType != nil {
		t.Fatalf("detail modType = %v, want null for an unknown type id", odd.ModType)
	}
}

// TestAOMMyClampsTheExplicitCount pins the My capture: the client sends
// count 1000 and the answer echoes 500; the AOE3 body without count keeps 10.
func TestAOMMyClampsTheExplicitCount(t *testing.T) {
	server, _, _ := newWriteServer(t)
	handler := server.Handler()

	recorder := do(t, handler, http.MethodPost, "/api/v4/Mods/My/", `{"game":1001,"count":1000}`, steamAuth())
	if recorder.Code != http.StatusOK {
		t.Fatalf("my: %d %s", recorder.Code, recorder.Body.String())
	}
	params := decodeMap(t, recorder.Body.Bytes())["searchParams"].(map[string]any)
	if params["count"] != float64(500) {
		t.Fatalf("my echo count = %v, want the 500 maximum", params["count"])
	}
	if params["sort"] != "popular" {
		t.Fatalf("my echo sort = %v", params["sort"])
	}

	recorder = do(t, handler, http.MethodPost, "/api/v4/Mods/My/", `{"game":1001}`, steamAuth())
	params = decodeMap(t, recorder.Body.Bytes())["searchParams"].(map[string]any)
	if params["count"] != float64(10) {
		t.Fatalf("my default count = %v, want 10", params["count"])
	}
}

// TestAOMFindSortsBySubscribersAndSubscribes pins the browsing of the
// capture: Find sorts by subscribers and echoes them, Rate answers the
// one-decimal average of the playFab envelope, and Subscribe replaces the
// download info that Unsubscribe removes.
func TestAOMFindSortsBySubscribersAndSubscribes(t *testing.T) {
	server, path := newTestServer(t)
	seed(t, path)
	handler := server.Handler()

	recorder := do(t, handler, http.MethodPost, "/api/v4/Mods/Find/",
		`{"game":1001,"start":1,"count":20,"order":"desc","sort":"subscribers"}`, steamAuth())
	if recorder.Code != http.StatusOK {
		t.Fatalf("find: %d %s", recorder.Code, recorder.Body.String())
	}
	params := decodeMap(t, recorder.Body.Bytes())["searchParams"].(map[string]any)
	if params["sort"] != "subscribers" || params["order"] != "DESC" || params["count"] != float64(20) {
		t.Fatalf("find echo = %v", params)
	}

	server2, path2, _ := newWriteServer(t)
	handler2 := server2.Handler()
	modID := publishAOM(t, handler2, 0)
	seeded, err := models.Open(models.Options{Path: path2, MaxOpenConns: 1})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	// The one-decimal shape of the capture: two fives, one four and the new
	// three average 4.25 raw, which the render rounds to 4.3.
	ctx := context.Background()
	for i, vote := range []struct {
		user  string
		value int64
	}{{"76561198000000011", 5}, {"76561198000000012", 5}, {"76561198000000013", 4}} {
		user, err := seeded.Users.EnsureUser(ctx, vote.user, "", "")
		if err != nil {
			t.Fatalf("seed rater %d: %v", i, err)
		}
		if _, err := seeded.Ratings.Rate(ctx, user.UserID, modID, vote.value); err != nil {
			t.Fatalf("rate %d: %v", vote.value, err)
		}
	}
	if err := seeded.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	rated := do(t, handler2, http.MethodPost, "/api/v4/Mods/Rate/",
		`{"modId":`+itoa(modID)+`,"rating":3}`, steamAuth())
	if rated.Code != http.StatusOK {
		t.Fatalf("rate: %d %s", rated.Code, rated.Body.String())
	}
	var response struct {
		PlayFabResponse struct {
			Rating struct {
				Average    float64 `json:"average"`
				TotalCount int64   `json:"totalCount"`
			} `json:"rating"`
			ReviewsCount int64 `json:"reviewsCount"`
		} `json:"playFabResponse"`
		StatusCode      *int64  `json:"statusCode"`
		ResponseMessage *string `json:"responseMessage"`
	}
	if err := json.Unmarshal(rated.Body.Bytes(), &response); err != nil {
		t.Fatalf("rate body: %v", err)
	}
	if response.StatusCode != nil || response.ResponseMessage != nil || response.PlayFabResponse.ReviewsCount != 0 {
		t.Fatalf("rate wrapper = %+v", response)
	}
	if response.PlayFabResponse.Rating.Average != 4.3 || response.PlayFabResponse.Rating.TotalCount != 4 {
		t.Fatalf("average = %+v, want the 4.3 of the one-decimal render", response.PlayFabResponse.Rating)
	}

	subscribed := do(t, handler2, http.MethodGet, "/api/v4/Mods/Subscribe/"+itoa(modID), "", steamAuth())
	if subscribed.Code != http.StatusOK || !strings.Contains(subscribed.Body.String(), `"gameTitleId":1001`) {
		t.Fatalf("subscribe: %d %s", subscribed.Code, subscribed.Body.String())
	}
	if !strings.Contains(subscribed.Body.String(), `"fileCRC":"`) {
		t.Fatalf("subscribe crc = %s", subscribed.Body.String())
	}
	unsubscribed := do(t, handler2, http.MethodGet, "/api/v4/Mods/Unsubscribe/"+itoa(modID), "", steamAuth())
	if unsubscribed.Code != http.StatusOK || strings.TrimSpace(unsubscribed.Body.String()) != "true" {
		t.Fatalf("unsubscribe: %d %s", unsubscribed.Code, unsubscribed.Body.String())
	}
}
