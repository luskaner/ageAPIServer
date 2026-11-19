// SPDX-License-Identifier: AGPL-3.0-only

package shared

import (
	"bytes"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luskaner/ageAPIServer/internal/models"
)

// multipartRequest builds a POST with the given text fields and files.
func multipartRequest(t *testing.T, fields map[string]string, files map[string]string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatalf("write field %s: %v", name, err)
		}
	}
	for name, content := range files {
		part, err := writer.CreateFormFile(name, name)
		if err != nil {
			t.Fatalf("create file %s: %v", name, err)
		}
		if _, err := part.Write([]byte(content)); err != nil {
			t.Fatalf("write file %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v4/mods/Publish", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func TestParsePublishReadsEveryField(t *testing.T) {
	request := multipartRequest(t, map[string]string{
		"gameTitleId":   "2",
		"modId":         "583325",
		"modType":       "15",
		"modName":       "  The Roman Dawn  ",
		"description":   "<p>desc</p>",
		"changeList":    "<p>change</p>",
		"modVisibility": "Private",
		"modDiscussion": "true",
		"modTags":       "15, 16",
		"ImageMetaData": `[{"imageId":null,"displayIndex":0,"imageName":"01.png","imageThumbnail":""}]`,
	}, map[string]string{
		"modImages": "png",
		"modFiles":  "zip",
	})

	form, err := ParsePublish(request)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if form.GameTitleID != 2 || form.ModID != 583325 || form.ModTypeID != 15 {
		t.Fatalf("ids = %+v", form)
	}
	if form.ModName != "The Roman Dawn" {
		t.Fatalf("modName = %q, want it trimmed", form.ModName)
	}
	if form.Description != "<p>desc</p>" || form.ChangeList != "<p>change</p>" {
		t.Fatalf("text = %+v", form)
	}
	if form.ModVisibility != "Private" || !form.ModDiscussion {
		t.Fatalf("flags = %+v", form)
	}
	if len(form.TagIDs) != 2 || form.TagIDs[0] != 15 || form.TagIDs[1] != 16 {
		t.Fatalf("tags = %v", form.TagIDs)
	}
	if len(form.ImageMeta) != 1 || form.ImageMeta[0].ImageName != "01.png" {
		t.Fatalf("image meta = %+v", form.ImageMeta)
	}
	if len(form.Images) != 1 || form.File == nil {
		t.Fatalf("files = %d/%v", len(form.Images), form.File)
	}
}

func TestParsePublishRejectsBadFields(t *testing.T) {
	cases := map[string]map[string]string{
		"gameTitleId":   {"gameTitleId": "abc"},
		"modId":         {"modId": "abc"},
		"modType":       {"modType": "abc"},
		"modDiscussion": {"modDiscussion": "maybe"},
		"modTags":       {"modTags": "15,abc"},
		"ImageMetaData": {"ImageMetaData": "not json"},
	}
	for name, fields := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParsePublish(multipartRequest(t, fields, nil)); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
	}
}

func TestParsePublishRejectsANonMultipartBody(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v4/mods/Publish", strings.NewReader("{}"))
	request.Header.Set("Content-Type", "application/json")
	if _, err := ParsePublish(request); err == nil {
		t.Fatal("a JSON body was accepted")
	}
}

func TestPublishFormValidate(t *testing.T) {
	valid := PublishForm{GameTitleID: 2, ModName: "ok"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid form rejected: %v", err)
	}
	for _, visibility := range []string{"Public", "Private", "Unlisted"} {
		form := valid
		form.ModVisibility = visibility
		if err := form.Validate(); err != nil {
			t.Fatalf("%s rejected: %v", visibility, err)
		}
	}

	noGame := valid
	noGame.GameTitleID = 0
	if err := noGame.Validate(); err == nil {
		t.Fatal("a form without gameTitleId was accepted")
	}

	noName := valid
	noName.ModName = ""
	if err := noName.Validate(); err == nil {
		t.Fatal("a form without modName was accepted")
	}

	badVisibility := valid
	badVisibility.ModVisibility = "Secret"
	if err := badVisibility.Validate(); err == nil {
		t.Fatal("an unknown visibility was accepted")
	}
}

func TestSaveUploadStoresTheContentOnce(t *testing.T) {
	dir := t.TempDir()
	request := multipartRequest(t, nil, map[string]string{"modFiles": "payload"})
	if err := request.ParseMultipartForm(maxPublishMemory); err != nil {
		t.Fatalf("parse: %v", err)
	}
	header := request.MultipartForm.File["modFiles"][0]

	first, err := SaveUpload(dir, header, 0)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if first.Size != float64(len("payload")) {
		t.Fatalf("size = %v", first.Size)
	}
	if first.CRC == "" || first.Path != first.CRC {
		t.Fatalf("saved = %+v", first)
	}
	if _, err := os.Stat(filepath.Join(dir, first.Path)); err != nil {
		t.Fatalf("stored file: %v", err)
	}

	// The same bytes must not create a second file.
	second, err := SaveUpload(dir, header, 0)
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if second.Path != first.Path {
		t.Fatalf("second path = %q, want %q", second.Path, first.Path)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("dir holds %d entries, want 1", len(entries))
	}
}

// TestSaveUploadEnforcesTheStreamedLimit pins the cap of an upload: the error
// is the sentinel the handlers map to 400, and no temporary file survives.
func TestSaveUploadEnforcesTheStreamedLimit(t *testing.T) {
	dir := t.TempDir()
	request := multipartRequest(t, nil, map[string]string{"modFiles": strings.Repeat("x", 64)})
	if err := request.ParseMultipartForm(maxPublishMemory); err != nil {
		t.Fatalf("parse: %v", err)
	}
	header := request.MultipartForm.File["modFiles"][0]

	if _, err := SaveUpload(dir, header, 16); !errors.Is(err, models.ErrFileTooLarge) {
		t.Fatalf("err = %v, want ErrFileTooLarge", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("oversized upload left %d files", len(entries))
	}
}

// TestValidateWebPublish pins the rules of the website form on their own: the
// handler only chooses when to apply them.
func TestValidateWebPublish(t *testing.T) {
	file := func(name string) *multipart.FileHeader { return &multipart.FileHeader{Filename: name} }
	base := func() PublishForm {
		return PublishForm{
			Description: "a description",
			TagIDs:      []int64{1},
			ImageMeta:   []ImageMeta{{ImageName: "shot.png"}},
			Images:      []*multipart.FileHeader{file("shot.png")},
			File:        file("mod.zip"),
		}
	}
	if err := ValidateWebPublish(base()); err != nil {
		t.Fatalf("valid form rejected: %v", err)
	}

	cases := []struct {
		name   string
		want   string
		mutate func(*PublishForm)
	}{
		{"description", "description is required", func(f *PublishForm) { f.Description = "  " }},
		{"no images", "between 1 and 5 images are required", func(f *PublishForm) {
			f.ImageMeta = nil
			f.Images = nil
		}},
		{"six images", "between 1 and 5 images are required", func(f *PublishForm) {
			f.ImageMeta = make([]ImageMeta, 6)
			f.Images = make([]*multipart.FileHeader, 6)
			for i := 0; i < 6; i++ {
				name := fmt.Sprintf("shot%d.png", i)
				f.ImageMeta[i] = ImageMeta{ImageName: name}
				f.Images[i] = file(name)
			}
		}},
		{"tags", "at most 7 tags are allowed", func(f *PublishForm) {
			f.TagIDs = []int64{1, 2, 3, 4, 5, 6, 7, 8}
		}},
		{"image format", "unsupported image format", func(f *PublishForm) {
			f.ImageMeta = []ImageMeta{{ImageName: "shot.exe"}}
			f.Images = []*multipart.FileHeader{file("shot.exe")}
		}},
		{"archive format", "mod file must be a .zip", func(f *PublishForm) { f.File = file("mod.sga") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			form := base()
			tc.mutate(&form)
			err := ValidateWebPublish(form)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestParseBlockListAcceptsNewlineIDs(t *testing.T) {
	ids := ParseBlockList([]byte("00000000\n00000001\r\n\n"))
	if len(ids) != 2 || ids[0] != "00000000" || ids[1] != "00000001" {
		t.Fatalf("ids = %v", ids)
	}
}

func TestStageBlockNeedsADirectory(t *testing.T) {
	if _, err := StageBlock("", 1, "00000000", strings.NewReader("data")); err == nil {
		t.Fatal("an empty directory was accepted")
	}
}

func TestSaveStagedBlocksReportsAMissingPart(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-block")
	if _, err := SaveStagedBlocks(t.TempDir(), []string{missing}); err == nil {
		t.Fatal("a missing block was accepted")
	}
}

func TestDecodeLocalizedKeepsAnInvalidObject(t *testing.T) {
	for _, raw := range []string{"plain text", `{"es":`, "{"} {
		if got := decodeLocalized(raw); got != raw {
			t.Fatalf("decodeLocalized(%q) = %q", raw, got)
		}
	}
}

func TestSaveUploadNeedsADirectory(t *testing.T) {
	request := multipartRequest(t, nil, map[string]string{"modFiles": "payload"})
	if err := request.ParseMultipartForm(maxPublishMemory); err != nil {
		t.Fatalf("parse: %v", err)
	}
	header := request.MultipartForm.File["modFiles"][0]
	if _, err := SaveUpload("", header, 0); err == nil {
		t.Fatal("an empty directory was accepted")
	}
}

func TestSaveUploadRejectsADirectoryThatIsAFile(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	request := multipartRequest(t, nil, map[string]string{"modFiles": "payload"})
	if err := request.ParseMultipartForm(maxPublishMemory); err != nil {
		t.Fatalf("parse: %v", err)
	}
	header := request.MultipartForm.File["modFiles"][0]
	if _, err := SaveUpload(blocker, header, 0); err == nil {
		t.Fatal("a directory that is a file was accepted")
	}
}

func TestSaveUploadRejectsAnUnreadableHeader(t *testing.T) {
	// A header whose temporary file is gone cannot be opened.
	header := &multipart.FileHeader{Filename: "gone.zip"}
	if _, err := SaveUpload(t.TempDir(), header, 0); err == nil {
		t.Fatal("a header without content was accepted")
	}
}

func TestTagFieldsWithoutMultipartForm(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	tags, err := tagFields(request)
	if err != nil {
		t.Fatalf("tags: %v", err)
	}
	if tags != nil {
		t.Fatalf("tags = %v, want nil", tags)
	}
}

func TestTagFieldsSkipsEmptyPieces(t *testing.T) {
	request := multipartRequest(t, map[string]string{"modTags": " , 15 ,"}, nil)
	if err := request.ParseMultipartForm(maxPublishMemory); err != nil {
		t.Fatalf("parse: %v", err)
	}
	tags, err := tagFields(request)
	if err != nil {
		t.Fatalf("tags: %v", err)
	}
	if len(tags) != 1 || tags[0] != 15 {
		t.Fatalf("tags = %v", tags)
	}
}
