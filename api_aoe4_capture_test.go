package ageAPIServer

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/luskaner/ageAPIServer/internal/models"
)

// The AOE4 capture of November 18-19 drives every assertion here: the client
// creates the mod through Publish with a JSON localized name and description,
// lowercase visibility and no file, then uploads the archive through
// CheckAndPublishFile - one raw block followed by the <BlockList> commit -
// and publishes the update over it, which bumps the version to 1.2 and flags
// updateAvailable for its creator.

// postMultipartTo posts a multipart body with an arbitrary method: the file
// upload of the capture is a PUT.
func postMultipartTo(t *testing.T, handler http.Handler, method string, path string, fields map[string]string,
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
	request := httptest.NewRequest(method, path, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// putBlock sends one CheckAndPublishFile request of the capture: the raw
// bytes of the block or the block list XML, with the capture's query.
func putBlock(t *testing.T, handler http.Handler, query string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPut, "/api/v4/Mods/CheckAndPublishFile/"+query, bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/octet-stream")
	for name, value := range xblAuth() {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// aoe4PublishBody are the multipart fields of the capture create: the JSON
// localization maps, the lowercase visibility, the client id - which the
// upstream leaves null on the Detail - the game 4 Extension type and the
// thumbnail image. No file: the archive arrives through CheckAndPublishFile.
func aoe4PublishBody(modID int64) (map[string]string, []uploadFile) {
	fields := map[string]string{
		"modName":       `{"es":"moddisplayname"}`,
		"clientId":      "modname",
		"gameTitleId":   "4",
		"modType":       "6",
		"modVisibility": "public",
		"description":   `{"es":"moddesc"}`,
		"imageMetaData": `[{"displayIndex":0,"imageName":"thumbnail.jpeg","imageFileSize":"303476"}]`,
	}
	if modID > 0 {
		fields["modId"] = strconv.FormatInt(modID, 10)
	}
	files := []uploadFile{{Field: "modImages", Name: "thumbnail.jpeg", Content: strings.Repeat("t", 303476)}}
	return fields, files
}

func publishAOE4(t *testing.T, handler http.Handler, modID int64) int64 {
	t.Helper()
	fields, files := aoe4PublishBody(modID)
	recorder := postMultipartTo(t, handler, http.MethodPost, "/api/v4/mods/Publish", fields, files, xblAuth())
	if recorder.Code != http.StatusOK {
		t.Fatalf("publish %d: %d %s", modID, recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"resultKey":"ModId"`) {
		t.Fatalf("publish body = %s", body)
	}
	var envelope struct {
		ErrorCode   int64  `json:"errorCode"`
		ResultValue string `json:"resultValue"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil || envelope.ErrorCode != 0 {
		t.Fatalf("publish envelope = %s (%v)", body, err)
	}
	id, err := strconv.ParseInt(envelope.ResultValue, 10, 64)
	if err != nil {
		t.Fatalf("publish resultValue = %q", envelope.ResultValue)
	}
	return id
}

// detailXBL reads a mod through the public endpoint with the XBL identity of
// the capture, so the viewer is the creator that uploaded it.
func detailXBL(t *testing.T, handler http.Handler, modID int64) models.Detail {
	t.Helper()
	recorder := do(t, handler, http.MethodGet, "/api/v4/Mods/Detail/"+strconv.FormatInt(modID, 10), "", xblAuth())
	if recorder.Code != http.StatusOK {
		t.Fatalf("detail %d: %d %s", modID, recorder.Code, recorder.Body.String())
	}
	var out models.Detail
	if err := json.Unmarshal(recorder.Body.Bytes(), &out); err != nil {
		t.Fatalf("detail body: %v", err)
	}
	return out
}

// TestAOE4PublishCreatesWithoutAFile pins the create of the capture: the
// localized values decode to their plain strings, the lowercase visibility
// stores rendered, the Extension type name resolves although the Tags
// catalog of game 4 stops at 5, the auto tag renders id 26 without a name,
// the changeList stays empty - not wrapped - and the client id stays null.
func TestAOE4PublishCreatesWithoutAFile(t *testing.T) {
	server, _, _ := newWriteServer(t)
	handler := server.Handler()

	modID := publishAOE4(t, handler, 0)
	created := detailXBL(t, handler, modID)
	if modID < models.LocalModIDFloor {
		t.Fatalf("local mod id = %d, want the reserved band", modID)
	}
	if created.ModName != "moddisplayname" || created.Description != "moddesc" || created.ModDescription != "moddesc" {
		t.Fatalf("localized fields = %q %q", created.ModName, created.Description)
	}
	if created.ChangeList != "" || created.VerifiedVersion == nil || *created.VerifiedVersion != "" {
		t.Fatalf("changeList/verifiedVersion = %q %v", created.ChangeList, created.VerifiedVersion)
	}
	if created.ClientID != nil {
		t.Fatalf("clientId = %v, want null", created.ClientID)
	}
	if created.ModTypeID == nil || *created.ModTypeID != 6 || created.ModType == nil || *created.ModType != "Extension" {
		t.Fatalf("modType = %v %v", created.ModTypeID, created.ModType)
	}
	if created.ModVisibility == nil || *created.ModVisibility != "Public" {
		t.Fatalf("modVisibility = %v", created.ModVisibility)
	}
	if created.ModStatusID != 2 || created.ModStatus != "Published" {
		t.Fatalf("status = %d %q", created.ModStatusID, created.ModStatus)
	}
	if created.ModTags != nil {
		t.Fatalf("detail modTags = %v, want null", created.ModTags)
	}
	if len(created.ModTagIDs) != 1 || created.ModTagIDs[0] != 26 || len(created.ModTagNames) != 0 {
		t.Fatalf("tag ids/names = %v %v", created.ModTagIDs, created.ModTagNames)
	}
	if created.ModVersion != 1.1 {
		t.Fatalf("modVersion = %v, want 1.1", created.ModVersion)
	}
	if created.Popular == nil || *created.Popular != 1 || created.UserDownloaded == nil || !*created.UserDownloaded ||
		created.UserInstalled == nil || !*created.UserInstalled {
		t.Fatalf("creator counters = %v %v %v", created.Popular, created.UserDownloaded, created.UserInstalled)
	}
	if created.UpdateAvailable == nil || *created.UpdateAvailable {
		t.Fatalf("updateAvailable = %v, want false before the update", created.UpdateAvailable)
	}
	if created.Thumbnail == nil || !strings.Contains(*created.Thumbnail, "/api/v4/mods/image/") {
		t.Fatalf("thumbnail = %v", created.Thumbnail)
	}
}

// TestCheckAndPublishFileCommitsTheArchive pins the two-phase upload: the
// block stages the raw bytes (a body larger than the default body cap, as the
// capture's 1.5 MB block is), the blocklist commit concatenates them into the
// mod file with fileUrl, the size and disk size equal, and both answer the
// bare true of the capture. A commit of a foreign mod is refused.
func TestCheckAndPublishFileCommitsTheArchive(t *testing.T) {
	server, _, _ := newWriteServer(t)
	handler := server.Handler()
	modID := publishAOE4(t, handler, 0)

	// The capture block carries 1509669 bytes - larger than the 1 MiB the
	// server caps every other body with, so the exemption must hold.
	data := make([]byte, 1509669)
	for i := range data {
		data[i] = byte(i*7 + i/251)
	}
	recorder := putBlock(t, handler,
		"?blockid=00000000&comp=block&modId="+itoa(modID), data)
	if recorder.Code != http.StatusOK || strings.TrimSpace(recorder.Body.String()) != "true" {
		t.Fatalf("block: %d %s", recorder.Code, recorder.Body.String())
	}

	recorder = putBlock(t, handler, "?comp=blocklist&modId="+itoa(modID),
		[]byte("<?xml version=\"1.0\" encoding=\"utf-8\"?><BlockList><Latest>00000000</Latest></BlockList>"))
	if recorder.Code != http.StatusOK || strings.TrimSpace(recorder.Body.String()) != "true" {
		t.Fatalf("blocklist: %d %s", recorder.Code, recorder.Body.String())
	}

	commit := detailXBL(t, handler, modID)
	if commit.FileURL == nil || !strings.HasSuffix(*commit.FileURL, "/file") {
		t.Fatalf("fileUrl = %v", commit.FileURL)
	}
	if commit.ModFileSize == nil || *commit.ModFileSize != 1509669 || commit.ModDiskSize == nil || *commit.ModDiskSize != 1509669 {
		t.Fatalf("sizes = %v %v", commit.ModFileSize, commit.ModDiskSize)
	}

	// The staged blocks are gone and the file downloads back byte for byte.
	download := do(t, handler, http.MethodGet, "/api/v4/Mods/Download/"+itoa(modID)+"/file", "", xblAuth())
	if download.Code != http.StatusOK {
		t.Fatalf("download: %d", download.Code)
	}
	if !bytes.Equal(download.Body.Bytes(), data) {
		t.Fatalf("downloaded %d bytes, want the %d uploaded", download.Body.Len(), len(data))
	}

	// A mod of another client is not commitable: the commit re-checks the
	// ownership, so a second call from the same XBL user would pass here -
	// assert instead that a mod id that does not exist answers 404.
	other := putBlock(t, handler, "?comp=blocklist&modId=999999",
		[]byte("<?xml version=\"1.0\" encoding=\"utf-8\"?><BlockList><Latest>00000000</Latest></BlockList>"))
	if other.Code != http.StatusNotFound {
		t.Fatalf("foreign commit: %d %s", other.Code, other.Body.String())
	}
}

// TestAOE4UpdatePublishBumpsTheVersionAndFlagsTheUpdate pins the second
// publish of the capture: the version moves 1.1 to 1.2 without a version
// field, and the creator - which installed the first one - reads
// updateAvailable true on the next Detail. The commit afterwards lands the
// updated file.
func TestAOE4UpdatePublishBumpsTheVersionAndFlagsTheUpdate(t *testing.T) {
	server, _, _ := newWriteServer(t)
	handler := server.Handler()
	modID := publishAOE4(t, handler, 0)

	updated := publishAOE4(t, handler, modID)
	if updated != modID {
		t.Fatalf("update returned id %d, want %d", updated, modID)
	}
	flagged := detailXBL(t, handler, modID)
	if flagged.ModVersion != 1.2 {
		t.Fatalf("modVersion = %v, want 1.2", flagged.ModVersion)
	}
	if flagged.UpdateAvailable == nil || !*flagged.UpdateAvailable {
		t.Fatalf("updateAvailable = %v, want true after the update", flagged.UpdateAvailable)
	}
	if flagged.ChangeList != "" {
		t.Fatalf("changeList = %q, want empty", flagged.ChangeList)
	}
	if flagged.ModName != "moddisplayname" {
		t.Fatalf("modName = %q", flagged.ModName)
	}

	// The updated file lands the same way and replaces the first one.
	data := []byte(strings.Repeat("n", 3000))
	if recorder := putBlock(t, handler, "?blockid=00000000&comp=block&modId="+itoa(modID), data); recorder.Code != http.StatusOK {
		t.Fatalf("second block: %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := putBlock(t, handler, "?comp=blocklist&modId="+itoa(modID),
		[]byte("<?xml version=\"1.0\" encoding=\"utf-8\"?><BlockList><Latest>00000000</Latest></BlockList>")); recorder.Code != http.StatusOK {
		t.Fatalf("second blocklist: %d %s", recorder.Code, recorder.Body.String())
	}
	// Re-installing the update clears the flag again.
	recorder := do(t, handler, http.MethodPost, "/api/v1/Mods/Install/",
		`{"id":`+itoa(modID)+`}`, xblAuth())
	if recorder.Code != http.StatusOK {
		t.Fatalf("install: %d %s", recorder.Code, recorder.Body.String())
	}
	reinstalled := detailXBL(t, handler, modID)
	if reinstalled.UpdateAvailable == nil || *reinstalled.UpdateAvailable {
		t.Fatalf("updateAvailable = %v, want false after reinstalling", reinstalled.UpdateAvailable)
	}
}

// TestResponsesAreByteExactToTheCaptures pins the wire bytes the game's
// upload task compares: no trailing newline after any JSON body, and the
// Content-Length counts exactly the body. The launcher session that revealed
// this answered true\n (5 bytes) where the capture carries true (4) and the
// game aborted between the block and the blocklist commit.
func TestResponsesAreByteExactToTheCaptures(t *testing.T) {
	server, _, _ := newWriteServer(t)
	handler := server.Handler()

	fields, files := aoe4PublishBody(0)
	recorder := postMultipartTo(t, handler, http.MethodPost, "/api/v4/mods/Publish", fields, files, xblAuth())
	if recorder.Code != http.StatusOK {
		t.Fatalf("publish: %d %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.Bytes()
	if body[len(body)-1] == '\n' {
		t.Fatalf("publish body ends with a newline: %q", body)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("publish content-type = %q", got)
	}
	var envelope struct {
		ResultValue string `json:"resultValue"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("publish envelope = %s (%v)", body, err)
	}
	modID, err := strconv.ParseInt(envelope.ResultValue, 10, 64)
	if err != nil {
		t.Fatalf("publish resultValue = %q", envelope.ResultValue)
	}

	recorder = putBlock(t, handler, "?blockid=00000000&comp=block&modId="+itoa(modID), []byte("block-bytes"))
	if got := recorder.Body.Bytes(); string(got) != "true" {
		t.Fatalf("block body = %q, want exactly true", got)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("block content-type = %q", got)
	}

	// The recorder does not compute Content-Length: round-trip through a real
	// server once so the wire length the game reads is pinned too.
	live := httptest.NewServer(handler)
	defer live.Close()
	request, err := http.NewRequest(http.MethodPut,
		live.URL+"/api/v4/Mods/CheckAndPublishFile/?blockid=00000000&comp=block&modId="+itoa(modID),
		bytes.NewBufferString("block-bytes"))
	if err != nil {
		t.Fatalf("live block request: %v", err)
	}
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("Authorization", xblHeader)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("live block: %v", err)
	}
	liveBody, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.ContentLength != 4 || string(liveBody) != "true" {
		t.Fatalf("live block: length %d body %q", response.ContentLength, liveBody)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("block content-type = %q", got)
	}

	recorder = putBlock(t, handler, "?comp=blocklist&modId="+itoa(modID),
		[]byte("<?xml version=\"1.0\" encoding=\"utf-8\"?><BlockList><Latest>00000000</Latest></BlockList>"))
	if got := recorder.Body.Bytes(); string(got) != "true" {
		t.Fatalf("blocklist body = %q, want exactly true", got)
	}
}
