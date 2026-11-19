// SPDX-License-Identifier: AGPL-3.0-only

package ageAPIServer

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/luskaner/ageAPIServer/internal/models"
)

// createForm is the multipart body of the Create capture: two files and seven
// scalar fields, with the ImageMetaData entry naming the image file.
func createForm() (map[string]string, []uploadFile) {
	archive := &bytes.Buffer{}
	writer := zip.NewWriter(archive)
	for _, entry := range []struct {
		name    string
		content string
	}{{"mod.json", `{"modName":"test_mod"}`}, {"preview.txt", "test_mod"}} {
		file, err := writer.Create(entry.name)
		if err == nil {
			_, _ = file.Write([]byte(entry.content))
		}
	}
	_ = writer.Close()

	fields := map[string]string{
		"ImageMetaData": `[{"displayIndex":0,"imageFileSize":"25261","imageName":"_preview-icon.jpg"}]`,
		"ModName":       "test_mod",
		"GameTitleId":   "2",
		"ModType":       "1",
		"Description":   "des",
		"modVisibility": "Unlisted",
		"ChangeList":    "<p>changelog</p>",
	}
	files := []uploadFile{
		{Field: "modImages", Name: "_preview-icon.jpg", Content: "png-bytes"},
		{Field: "modFiles", Name: "test_mod.zip", Content: archive.String()},
	}
	return fields, files
}

// TestCreateMatchesTheLegacyCapture walks the lifecycle of the capture: the
// multipart Create answers the envelope with the new id, the mod shows up in
// the v4 Detail with the stored defaults, the v1 Detail renders the sparse
// wrapper with the metaData snapshot, the Install counts one download and one
// install, the Delete answers the Deleted envelope and every later lookup
// answers 404/204 like the captures of a deleted mod.
// TestUpdateThroughCreateReusesTheModId pins the November 19 launcher flow
// that crashed the game before: the AOE2 client re-publishes through the
// legacy Create carrying ModId of the existing mod, the identity of the image
// it already has (imageId with the absolute URLs, no upload) and no file.
// The endpoint must answer the SAME id - the launcher capture shows the
// server before the fix handing out 1000000002 for the next one - and must
// not spend an id on the update.
func TestUpdateThroughCreateReusesTheModId(t *testing.T) {
	server, _, _ := newWriteServer(t)
	handler := server.Handler()

	fields, files := createForm()
	create := postMultipart(t, handler, "/api/v1/Mods/Create/", fields, files, writeAuth())
	if create.Code != http.StatusOK {
		t.Fatalf("create: %d %s", create.Code, create.Body.String())
	}
	envelope := struct {
		ResultValue string `json:"resultValue"`
	}{}
	if err := json.Unmarshal(create.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("create envelope: %v", create.Body.String())
	}
	modID, err := strconv.ParseInt(envelope.ResultValue, 10, 64)
	if err != nil {
		t.Fatalf("create resultValue = %q", envelope.ResultValue)
	}

	// The identity of the stored image, exactly what the game echoes back.
	rendered := detail(t, handler, modID)
	if rendered.Thumbnail == nil {
		t.Fatalf("created mod has no thumbnail")
	}
	thumbnail := *rendered.Thumbnail

	var update bytes.Buffer
	writer := multipart.NewWriter(&update)
	imageMeta, err := json.Marshal([]map[string]any{{
		"displayIndex":   0,
		"imageFileSize":  nil,
		"imageId":        fmt.Sprintf("%d-0", modID),
		"imageName":      thumbnail,
		"imageThumbnail": thumbnail,
	}})
	if err != nil {
		t.Fatalf("imageMeta: %v", err)
	}
	for _, field := range []struct{ name, value string }{
		{"ImageMetaData", string(imageMeta)},
		{"ModTags", "26"},
		{"ModId", strconv.FormatInt(modID, 10)},
		{"ModName", "test_mod_v2"},
		{"GameTitleId", "2"},
		{"ModType", "1"},
		{"Description", "descripta"},
		{"modVisibility", "Public"},
		{"ChangeList", "<p>fgdfgdfgdfdg</p>"},
	} {
		if err := writer.WriteField(field.name, field.value); err != nil {
			t.Fatalf("write %s: %v", field.name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/Mods/Create/", &update)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	for name, value := range writeAuth() {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("update: %d %s", recorder.Code, recorder.Body.String())
	}
	cameBack := struct {
		ErrorCode   int64  `json:"errorCode"`
		ResultValue string `json:"resultValue"`
	}{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &cameBack); err != nil || cameBack.ErrorCode != 0 {
		t.Fatalf("update envelope = %s", recorder.Body.String())
	}
	if id, err := strconv.ParseInt(cameBack.ResultValue, 10, 64); err != nil || id != modID {
		t.Fatalf("update answered id %q, want %d", cameBack.ResultValue, modID)
	}

	// The mod keeps its single image and gains the update "version tick" and
	// the rewraped change list, like every update publish.
	after := detail(t, handler, modID)
	if after.ModVersion != 1.2 {
		t.Fatalf("modVersion = %v, want 1.2", after.ModVersion)
	}
	if after.ModName != "test_mod_v2" {
		t.Fatalf("modName = %q", after.ModName)
	}
	if after.Thumbnail == nil || *after.Thumbnail != thumbnail {
		t.Fatalf("thumbnail = %v, want the stored image untouched", after.Thumbnail)
	}
	if len(after.ImageURLs) != 1 || after.ImageURLs[0].ImageName != thumbnail {
		t.Fatalf("images = %+v, want the same single image", after.ImageURLs)
	}
	if !strings.HasPrefix(after.ChangeList, `<h4 class="mod-update-date">`) ||
		!strings.HasSuffix(after.ChangeList, `<p>fgdfgdfgdfdg</p>`) {
		t.Fatalf("changeList = %q", after.ChangeList)
	}

	// The update spends no id: the next create takes the very next one.
	fields, files = createForm()
	fields["ModName"] = "second_mod"
	second := postMultipart(t, handler, "/api/v1/Mods/Create/", fields, files, writeAuth())
	if second.Code != http.StatusOK {
		t.Fatalf("second create: %d %s", second.Code, second.Body.String())
	}
	next := struct {
		ResultValue string `json:"resultValue"`
	}{}
	if err := json.Unmarshal(second.Body.Bytes(), &next); err != nil {
		t.Fatalf("second envelope: %v %s", err, second.Body.String())
	}
	secondID, err := strconv.ParseInt(next.ResultValue, 10, 64)
	if err != nil || secondID != modID+1 {
		t.Fatalf("second id = %q, want %d", next.ResultValue, modID+1)
	}
}

func TestCreateMatchesTheLegacyCapture(t *testing.T) {
	server, _, _ := newWriteServer(t)
	handler := server.Handler()

	fields, files := createForm()
	recorder := postMultipart(t, handler, "/api/v1/Mods/Create/", fields, files, writeAuth())
	if recorder.Code != http.StatusOK {
		t.Fatalf("create: %d %s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		ErrorCode    int64   `json:"errorCode"`
		ErrorMessage string  `json:"errorMessage"`
		ResultKey    string  `json:"resultKey"`
		ResultValue  string  `json:"resultValue"`
		ResultJSON   *string `json:"resultJson"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("create body: %v (%s)", err, recorder.Body.String())
	}
	if envelope.ErrorCode != 0 || envelope.ErrorMessage != "0" || envelope.ResultKey != "ModId" ||
		envelope.ResultJSON != nil || envelope.ResultValue == "" {
		t.Fatalf("create envelope = %+v", envelope)
	}
	var modID int64
	for _, digit := range envelope.ResultValue {
		modID = modID*10 + int64(digit-'0')
	}
	if modID <= 0 {
		t.Fatalf("resultValue = %q", envelope.ResultValue)
	}

	// The created mod keeps the upload defaults the capture shows: Published
	// despite the Unlisted visibility, modVersion 1.1, the mirrored
	// modDescription, the changeList header, the "Other" tag and a viewer
	// state of downloaded and installed for its creator.
	detail := detail(t, handler, modID)
	if detail.ModStatusID != 2 || detail.ModStatus != "Published" {
		t.Fatalf("status = %d/%s, want the published pair", detail.ModStatusID, detail.ModStatus)
	}
	if detail.ModVersion != 1.1 {
		t.Fatalf("modVersion = %v, want 1.1", detail.ModVersion)
	}
	if detail.ModDescription != "des" {
		t.Fatalf("modDescription = %q, want des", detail.ModDescription)
	}
	if detail.VerifiedVersion == nil || *detail.VerifiedVersion != "" {
		t.Fatalf("verifiedVersion = %v, want empty", detail.VerifiedVersion)
	}
	if !strings.HasPrefix(detail.ChangeList, `<h4 class="mod-update-date">11/`) ||
		!strings.HasSuffix(detail.ChangeList, `<p>changelog</p>`) {
		t.Fatalf("changeList = %q", detail.ChangeList)
	}
	if len(detail.ModTagIDs) != 1 || detail.ModTagIDs[0] != 26 {
		t.Fatalf("modTagIds = %v, want the Other default", detail.ModTagIDs)
	}
	if detail.ModVisibility == nil || *detail.ModVisibility != "Unlisted" {
		t.Fatalf("modVisibility = %v", detail.ModVisibility)
	}
	if detail.Popular == nil || *detail.Popular != 1 || detail.Downloads == nil || *detail.Downloads != 1 ||
		detail.Installs == nil || *detail.Installs != 1 {
		t.Fatalf("counters = %v/%v/%v, want 1/1/1", detail.Popular, detail.Downloads, detail.Installs)
	}
	if detail.UserInstalled == nil || !*detail.UserInstalled || detail.UserDownloaded == nil || !*detail.UserDownloaded {
		t.Fatalf("viewer state = %v/%v", detail.UserDownloaded, detail.UserInstalled)
	}
	if detail.Thumbnail == nil || !strings.HasPrefix(*detail.Thumbnail, "http") {
		t.Fatalf("thumbnail = %v", detail.Thumbnail)
	}
	if detail.ModDiskSize == nil || *detail.ModDiskSize != float64(len(`{"modName":"test_mod"}`)+len("test_mod")) {
		t.Fatalf("modDiskSize = %v, want the zip uncompressed size", detail.ModDiskSize)
	}

	// The legacy Detail wraps the sparse outer body around the metaData
	// snapshot: the viewer fields live only inside the snapshot.
	legacy := do(t, handler, http.MethodPost, "/api/v1/Mods/Detail/", `{"modid":`+envelope.ResultValue+`}`, writeAuth())
	if legacy.Code != http.StatusOK {
		t.Fatalf("legacy detail: %d %s", legacy.Code, legacy.Body.String())
	}
	var outer map[string]any
	if err := json.Unmarshal(legacy.Body.Bytes(), &outer); err != nil {
		t.Fatalf("legacy body: %v", err)
	}
	for _, key := range []string{"gameTitleName", "modVisibility", "creatorId", "createDate", "lastUpdate",
		"downloads", "popular", "thumbnail", "userLiked", "ratings", "isOwner"} {
		if value, present := outer[key]; !present || value != nil {
			t.Fatalf("legacy outer %s = %v, want null", key, value)
		}
	}
	if outer["gameTitleId"] != float64(2) || outer["modStatusId"] != float64(2) ||
		outer["isLoggedOn"] != false || outer["currentRating"] != float64(0) {
		t.Fatalf("legacy outer identity = %v", outer)
	}
	meta, ok := outer["metaData"].(map[string]any)
	if !ok {
		t.Fatalf("legacy metaData = %v", outer["metaData"])
	}
	if meta["modId"] != float64(modID) || meta["modVisibility"] != "Unlisted" ||
		meta["isOwner"] != true || meta["isLoggedOn"] != true ||
		meta["modStatusId"] != nil || meta["changeList"] != nil ||
		meta["modVersion"] != float64(0) ||
		meta["modDiskSize"] != *detail.ModDiskSize {
		t.Fatalf("legacy metaData = %v", meta)
	}
	tags, ok := outer["modTags"].([]any)
	if !ok || len(tags) != 1 || tags[0] != "26" {
		t.Fatalf("legacy modTags = %v, want the string id", outer["modTags"])
	}
	if outer["modTagIds"] != nil {
		t.Fatalf("legacy modTagIds = %v, want null", outer["modTagIds"])
	}

	// Install counts one download and one install for the creator.
	installed := do(t, handler, http.MethodPost, "/api/v1/Mods/Install/",
		`{"id":`+envelope.ResultValue+`}`, writeAuth())
	if installed.Code != http.StatusOK {
		t.Fatalf("install: %d %s", installed.Code, installed.Body.String())
	}
	var installBody struct {
		Value *models.DownloadInfo `json:"value"`
	}
	if err := json.Unmarshal(installed.Body.Bytes(), &installBody); err != nil {
		t.Fatalf("install body: %v (%s)", err, installed.Body.String())
	}
	if installBody.Value == nil || installBody.Value.DownloadURL == "" ||
		installBody.Value.DownloadCount != 1 || installBody.Value.FileCRC == "" ||
		strings.HasSuffix(installBody.Value.DownloadURL, "file") == false {
		t.Fatalf("install value = %+v", installBody.Value)
	}

	// My shows the mod with the legacy render and totalCount null.
	mine := do(t, handler, http.MethodPost, "/api/v1/Mods/My/",
		`{"game":2,"q":"*","start":1,"count":45}`, writeAuth())
	if mine.Code != http.StatusOK {
		t.Fatalf("my: %d %s", mine.Code, mine.Body.String())
	}
	var legacyList struct {
		ModList    []map[string]any `json:"modList"`
		TotalCount *int64           `json:"totalCount"`
	}
	if err := json.Unmarshal(mine.Body.Bytes(), &legacyList); err != nil {
		t.Fatalf("my body: %v", err)
	}
	if legacyList.TotalCount != nil {
		t.Fatalf("my totalCount = %v, want null", legacyList.TotalCount)
	}
	var row map[string]any
	for _, entry := range legacyList.ModList {
		if entry["modId"] == float64(modID) {
			row = entry
			break
		}
	}
	if row == nil {
		t.Fatalf("my is missing the created mod: %v", legacyList.ModList)
	}
	if row["changeList"] != nil || row["ratings"] != nil || row["popular"] != nil ||
		row["gameTitleId"] != nil || row["modTypeId"] != nil || row["modStatusId"] != nil {
		t.Fatalf("my row nulls = %v", row)
	}
	if row["isOwner"] != true || row["modStatus"] != "Published" {
		t.Fatalf("my row = %v", row)
	}

	// Delete answers the Deleted envelope and later lookups answer 204/404.
	deleted := do(t, handler, http.MethodPost, "/api/v1/Mods/Delete/",
		`{"id":`+envelope.ResultValue+`}`, writeAuth())
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", deleted.Code, deleted.Body.String())
	}
	if got := strings.TrimRight(deleted.Body.String(), "\n"); got !=
		`{"errorCode":0,"errorMessage":"0","resultKey":"Deleted","resultValue":"true","resultJson":null}` {
		t.Fatalf("delete body = %s", got)
	}

	gone := do(t, handler, http.MethodGet, "/api/v4/mods/Detail/"+envelope.ResultValue, "", writeAuth())
	if gone.Code != http.StatusNotFound {
		t.Fatalf("deleted detail: %d, want 404", gone.Code)
	}
	var problem struct {
		Type    string `json:"type"`
		Title   string `json:"title"`
		Status  int    `json:"status"`
		TraceID string `json:"traceId"`
	}
	if err := json.Unmarshal(gone.Body.Bytes(), &problem); err != nil {
		t.Fatalf("problem body: %v (%s)", err, gone.Body.String())
	}
	if problem.Type != "https://tools.ietf.org/html/rfc9110#section-15.5.5" || problem.Title != "Not Found" ||
		problem.Status != 404 || !strings.HasPrefix(problem.TraceID, "00-") {
		t.Fatalf("problem details = %+v", problem)
	}

	related := do(t, handler, http.MethodGet, "/api/v4/mods/Related/"+envelope.ResultValue, "", writeAuth())
	if related.Code != http.StatusNoContent {
		t.Fatalf("related of deleted: %d, want 204", related.Code)
	}
}

// TestCreateWithoutUploadsCreatesAMod checks that the scalar-only create
// works without an upload directory, which the upload rules of Publish allow.
func TestCreateWithoutUploadsCreatesAMod(t *testing.T) {
	server, _, uploads := newWriteServer(t)
	if uploads != "" {
		_ = uploads
	}
	recorder := postMultipart(t, server.Handler(), "/api/v1/Mods/Create/", map[string]string{
		"ModName":       "scalar",
		"GameTitleId":   "2",
		"modVisibility": "Public",
	}, nil, writeAuth())
	if recorder.Code != http.StatusOK {
		t.Fatalf("create: %d %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"resultKey":"ModId"`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}
