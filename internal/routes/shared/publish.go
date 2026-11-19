// SPDX-License-Identifier: AGPL-3.0-only

package shared

import (
	"archive/zip"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/luskaner/ageAPIServer/internal/models"
)

// maxPublishMemory is how much of a multipart body is kept in memory before the
// rest spills to a temporary file.
const maxPublishMemory = 8 << 20

// ImageMeta is one entry of the ImageMetaData field of Publish. imageId is null
// when the image is new and set when it already exists upstream. FileSize only
// carries the declared size of the legacy Create entries (imageFileSize); the
// real upload size replaces it when the entry matches an uploaded file.
type ImageMeta struct {
	ImageID        *string `json:"imageId"`
	DisplayIndex   int64   `json:"displayIndex"`
	ImageName      string  `json:"imageName"`
	ImageThumbnail string  `json:"imageThumbnail"`
	FileSize       float64 `json:"-"`
}

// PublishForm is the parsed multipart body of Publish.
type PublishForm struct {
	ModID         int64
	GameTitleID   int64
	ModName       string
	Description   string
	ChangeList    string
	ModVisibility string
	ModTypeID     int64
	ModDiscussion bool
	TagIDs        []int64
	ImageMeta     []ImageMeta
	Images        []*multipart.FileHeader
	File          *multipart.FileHeader
}

// ParsePublish reads the multipart body of Publish. Every scalar field is
// optional at this level: Validate decides which ones are required.
func ParsePublish(r *http.Request) (PublishForm, error) {
	if err := r.ParseMultipartForm(maxPublishMemory); err != nil {
		return PublishForm{}, fmt.Errorf("read multipart body: %w", err)
	}
	form := PublishForm{
		ModName:       strings.TrimSpace(decodeLocalized(formValue(r, "modName"))),
		Description:   decodeLocalized(formValue(r, "description")),
		ChangeList:    formValue(r, "changeList"),
		ModVisibility: canonicalVisibility(formValue(r, "modVisibility")),
	}

	var err error
	if form.GameTitleID, err = intFieldOf(r, "gameTitleId"); err != nil {
		return PublishForm{}, err
	}
	if form.ModID, err = intFieldOf(r, "modId"); err != nil {
		return PublishForm{}, err
	}
	if form.ModTypeID, err = intFieldOf(r, "modType"); err != nil {
		return PublishForm{}, err
	}
	if form.ModDiscussion, err = boolFieldOf(r, "modDiscussion"); err != nil {
		return PublishForm{}, err
	}
	if form.TagIDs, err = tagFields(r); err != nil {
		return PublishForm{}, err
	}
	if raw := formValue(r, "ImageMetaData"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &form.ImageMeta); err != nil {
			return PublishForm{}, fmt.Errorf("ImageMetaData: %w", err)
		}
	}
	if r.MultipartForm != nil {
		form.Images = r.MultipartForm.File["modImages"]
		if files := r.MultipartForm.File["modFiles"]; len(files) > 0 {
			form.File = files[0]
		}
	}
	return form, nil
}

// CreateImageMeta is one entry of the ImageMetaData field of the legacy
// Create. Unlike Publish the entry carries the file size as a string and no
// image id or thumbnail.
type CreateImageMeta struct {
	DisplayIndex  int64  `json:"displayIndex"`
	ImageFileSize string `json:"imageFileSize"`
	ImageName     string `json:"imageName"`
}

// formValue reads a multipart value under any casing of the field name: the
// AOE2 Create spells ModName and GameTitleId, the AOE3 client spells
// imageMetaData, and the website spells everything lowerCamel. The first
// entry wins.
func formValue(r *http.Request, name string) string {
	if r.MultipartForm != nil {
		for key, values := range r.MultipartForm.Value {
			if len(values) > 0 && strings.EqualFold(key, name) {
				return values[0]
			}
		}
	}
	return ""
}

// decodeLocalized stores the value of the AOE4 Publish fields, which the
// client sends as a JSON localization map: the capture carries modName
// {"es":"moddisplayname"} and description {"es":"moddesc"} while every later
// render shows the plain string. A non-object value - the plain text AOE2 and
// AOE3 send - passes through. Inside the object the "en" entry wins when
// present, otherwise the first entry in the order the client wrote them.
func decodeLocalized(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "{") {
		return raw
	}
	type entry struct {
		key   string
		value string
	}
	var entries []entry
	decoder := json.NewDecoder(strings.NewReader(raw))
	if _, err := decoder.Token(); err != nil { // {
		return raw
	}
	for decoder.More() {
		var key string
		if err := decoder.Decode(&key); err != nil {
			return raw
		}
		var value any
		if err := decoder.Decode(&value); err != nil {
			return raw
		}
		text, ok := value.(string)
		if !ok {
			encoded, err := json.Marshal(value)
			if err != nil {
				return raw
			}
			text = string(encoded)
		}
		entries = append(entries, entry{key: key, value: text})
	}
	if len(entries) == 0 {
		return raw
	}
	for _, item := range entries {
		if item.key == "en" {
			return item.value
		}
	}
	return entries[0].value
}

// canonicalVisibility folds the visibility casing: the AOE4 client spells the
// field lowercase ("public") while the storage and the renders of the capture
// keep "Public", "Private" and "Unlisted".
func canonicalVisibility(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "public":
		return "Public"
	case "private":
		return "Private"
	case "unlisted":
		return "Unlisted"
	default:
		return strings.TrimSpace(raw)
	}
}

// legacyInt reads an optional integer legacy form field.
func legacyInt(r *http.Request, name string) (int64, error) {
	raw := strings.TrimSpace(formValue(r, name))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number", name)
	}
	return value, nil
}

// ParseCreate reads the multipart body of the legacy /api/v1/Mods/Create. The
// fields are the ones of the capture: the modImages screenshot (paired with
// ImageMetaData by file name), the modFiles archive, ModName, GameTitleId,
// ModType, Description, modVisibility and ChangeList. The scalar fields are
// optional at this level: Validate decides which ones are required.
func ParseCreate(r *http.Request) (PublishForm, error) {
	if err := r.ParseMultipartForm(maxPublishMemory); err != nil {
		return PublishForm{}, fmt.Errorf("read multipart body: %w", err)
	}
	form := PublishForm{
		ModName:       strings.TrimSpace(decodeLocalized(formValue(r, "ModName"))),
		Description:   decodeLocalized(formValue(r, "Description")),
		ChangeList:    formValue(r, "ChangeList"),
		ModVisibility: canonicalVisibility(formValue(r, "modVisibility")),
	}
	var err error
	if form.GameTitleID, err = legacyInt(r, "GameTitleId"); err != nil {
		return PublishForm{}, err
	}
	// The AOE2 client updates through this endpoint: the November 19 capture
	// carries ModId of the existing mod, so the field decides between create
	// and update exactly like the v4 Publish form.
	if form.ModID, err = legacyInt(r, "ModId"); err != nil {
		return PublishForm{}, err
	}
	if form.ModTypeID, err = legacyInt(r, "ModType"); err != nil {
		return PublishForm{}, err
	}
	if raw := formValue(r, "ImageMetaData"); raw != "" {
		var images []CreateImageMeta
		if err := json.Unmarshal([]byte(raw), &images); err != nil {
			return PublishForm{}, fmt.Errorf("ImageMetaData: %w", err)
		}
		form.ImageMeta = make([]ImageMeta, 0, len(images))
		for _, image := range images {
			meta := ImageMeta{DisplayIndex: image.DisplayIndex, ImageName: image.ImageName}
			if size, err := strconv.ParseFloat(strings.TrimSpace(image.ImageFileSize), 64); err == nil {
				meta.FileSize = size
			}
			form.ImageMeta = append(form.ImageMeta, meta)
		}
	}
	if r.MultipartForm != nil {
		form.Images = r.MultipartForm.File["modImages"]
		if files := r.MultipartForm.File["modFiles"]; len(files) > 0 {
			form.File = files[0]
		}
	}
	return form, nil
}

// DiskSize returns the uncompressed size of an uploaded archive, which the
// capture carries as modDiskSize of a created mod. A body that is not a zip
// archive answers 0 and no error: the size is then left unset.
func DiskSize(header *multipart.FileHeader) float64 {
	source, err := header.Open()
	if err != nil {
		return 0
	}
	defer func() { _ = source.Close() }()
	reader, ok := source.(io.ReaderAt)
	if !ok {
		return 0
	}
	archive, err := zip.NewReader(reader, header.Size)
	if err != nil {
		return 0
	}
	var total uint64
	for _, entry := range archive.File {
		total += entry.UncompressedSize64
	}
	return float64(total)
}

// PublishImages stores the uploaded screenshots and pairs them with their
// metadata. An entry without a matching upload keeps the thumbnail the client
// sent, which is how an update reuses an image that already exists, and falls
// back to the declared size. max caps each file as it is streamed, like
// SaveUpload.
func PublishImages(dir string, form PublishForm, max int64) ([]models.PublishImage, error) {
	if len(form.ImageMeta) == 0 {
		return nil, nil
	}
	uploads := make(map[string]*multipart.FileHeader, len(form.Images))
	for _, header := range form.Images {
		uploads[header.Filename] = header
	}

	images := make([]models.PublishImage, 0, len(form.ImageMeta))
	for _, meta := range form.ImageMeta {
		image := models.PublishImage{
			DisplayIndex: meta.DisplayIndex,
			ImageName:    meta.ImageName,
			Thumbnail:    meta.ImageThumbnail,
			FileSize:     meta.FileSize,
		}
		if header, ok := uploads[meta.ImageName]; ok {
			saved, err := SaveUpload(dir, header, max)
			if err != nil {
				return nil, err
			}
			// The stored name replaces both references: the capture renders
			// imageName and imageThumbnail as the same URL of the stored
			// image, so a name the client never sent back as a URL also
			// resolves through /image.
			image.ImageName = saved.Path
			image.Thumbnail = saved.Path
			image.FileSize = saved.Size
		}
		images = append(images, image)
	}
	return images, nil
}

// Validate rejects a publish that misses a required field.
func (f PublishForm) Validate() error {
	if f.GameTitleID <= 0 {
		return errors.New("gameTitleId is required")
	}
	if f.ModName == "" {
		return errors.New("modName is required")
	}
	switch f.ModVisibility {
	case "", "Public", "Private", "Unlisted":
	default:
		return errors.New("modVisibility must be Public, Private or Unlisted")
	}
	return nil
}

// webImageExtensions is the list the official website file picker accepts,
// verbatim from its banner.
var webImageExtensions = map[string]bool{
	"apng": true, "avif": true, "bmp": true, "gif": true, "heic": true,
	"heif": true, "ico": true, "jpe": true, "jpeg": true, "jpg": true,
	"jxl": true, "pjpeg": true, "pjp": true, "png": true, "svg": true,
	"svgz": true, "tif": true, "tiff": true, "webp": true, "xbm": true,
}

// extensionOf is the lowercase extension of a client filename without the dot.
func extensionOf(name string) string {
	return strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
}

// ValidateWebPublish applies the rules of the official website upload form,
// which only a cookie-authenticated publish sees: a required description, one
// to five screenshots from the picker's closed list of formats, at most seven
// tags and a .zip archive. The ticket path keeps the permissive contract the
// captures encode (AOE4 sends a .sga and no images), so this is never called
// from there.
func ValidateWebPublish(form PublishForm) error {
	if strings.TrimSpace(form.Description) == "" {
		return errors.New("description is required")
	}
	uploads := make(map[string]bool, len(form.Images))
	for _, header := range form.Images {
		uploads[header.Filename] = true
	}
	paired := 0
	for _, meta := range form.ImageMeta {
		if !uploads[meta.ImageName] {
			continue
		}
		if !webImageExtensions[extensionOf(meta.ImageName)] {
			return errors.New("unsupported image format")
		}
		paired++
	}
	if paired < 1 || paired > 5 || len(form.ImageMeta) > 5 {
		return errors.New("between 1 and 5 images are required")
	}
	if len(form.TagIDs) > 7 {
		return errors.New("at most 7 tags are allowed")
	}
	if form.File != nil && extensionOf(form.File.Filename) != "zip" {
		return errors.New("mod file must be a .zip")
	}
	return nil
}

// limitedWriter fails as soon as a write would push the total past limit. A
// non-positive limit means no limit, the same convention as
// models.CustomFilesStore. The multipart size header is client supplied, so the
// upload cap is applied to the bytes actually streamed.
type limitedWriter struct {
	limit int64
	n     int64
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	if lw.limit > 0 && lw.n+int64(len(p)) > lw.limit {
		return 0, models.ErrFileTooLarge
	}
	lw.n += int64(len(p))
	return len(p), nil
}

// SavedFile is the result of storing an uploaded file.
type SavedFile struct {
	// Path is the location relative to the upload directory.
	Path string
	Size float64
	CRC  string
}

// newUploadTemp creates dir when it is missing and opens the temporary file
// that SaveUpload and SaveStagedBlocks rename into place once its bytes are
// stored. The caller removes the returned name when it bails out.
func newUploadTemp(dir string) (*os.File, string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, "", fmt.Errorf("create upload dir: %w", err)
	}
	temp, err := os.CreateTemp(dir, "upload-*")
	if err != nil {
		return nil, "", fmt.Errorf("create upload: %w", err)
	}
	return temp, temp.Name(), nil
}

// SaveUpload stores an uploaded file under dir and returns its relative path,
// size and CRC32. The stored name is the content hash, so uploading the same
// bytes twice does not duplicate the file. max caps the bytes streamed to disk:
// a non-positive value means no limit, never "reject everything".
func SaveUpload(dir string, header *multipart.FileHeader, max int64) (SavedFile, error) {
	if dir == "" {
		return SavedFile{}, errors.New("no upload directory configured")
	}
	source, err := header.Open()
	if err != nil {
		return SavedFile{}, fmt.Errorf("open upload: %w", err)
	}
	defer func() { _ = source.Close() }()

	temp, tempName, err := newUploadTemp(dir)
	if err != nil {
		return SavedFile{}, err
	}
	defer func() { _ = os.Remove(tempName) }()

	hash := crc32.NewIEEE()
	sink := io.MultiWriter(temp, hash)
	if max > 0 {
		// The multipart size header is client supplied, so the cap applies to
		// the bytes actually copied. The limiter goes first so an oversized
		// file never reaches the temporary file.
		sink = io.MultiWriter(&limitedWriter{limit: max}, temp, hash)
	}
	written, err := io.Copy(sink, source)
	if err != nil {
		_ = temp.Close()
		return SavedFile{}, fmt.Errorf("write upload: %w", err)
	}
	if err := temp.Close(); err != nil {
		return SavedFile{}, fmt.Errorf("close upload: %w", err)
	}

	crc := fmt.Sprintf("%08x", hash.Sum32())
	name := crc + filepath.Ext(header.Filename)
	saved := SavedFile{Path: name, Size: float64(written), CRC: crc}

	final := filepath.Join(dir, name)
	if _, err := os.Stat(final); err == nil {
		// The same bytes are already stored.
		return saved, nil
	}
	if err := os.Rename(tempName, final); err != nil {
		return SavedFile{}, fmt.Errorf("store upload: %w", err)
	}
	return saved, nil
}

// BlockIDName maps a block id to a safe file name: the ids a client sends are
// Azure style base64 ("00000000" up to 64 chars), which is only hex encoded
// here so no separator trick can escape the staging directory.
func BlockIDName(blockID string) string {
	return hex.EncodeToString([]byte(blockID))
}

// StageBlock stores one raw block of CheckAndPublishFile under
// dir/blocks/<modId>/<blockid> and reports its size. Rewriting the same block
// id replaces its bytes, which is how the capture uploads twice.
func StageBlock(dir string, modID int64, blockID string, source io.Reader) (float64, error) {
	if dir == "" {
		return 0, errors.New("no upload directory configured")
	}
	if strings.TrimSpace(blockID) == "" {
		blockID = "00000000"
	}
	blockDir := filepath.Join(dir, "blocks", strconv.FormatInt(modID, 10))
	if err := os.MkdirAll(blockDir, 0o755); err != nil {
		return 0, fmt.Errorf("create block dir: %w", err)
	}
	temp, err := os.CreateTemp(blockDir, "block-*")
	if err != nil {
		return 0, fmt.Errorf("create block: %w", err)
	}
	tempName := temp.Name()
	written, err := io.Copy(temp, source)
	if err != nil {
		_ = temp.Close()
		_ = os.Remove(tempName)
		return 0, fmt.Errorf("write block: %w", err)
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempName)
		return 0, fmt.Errorf("close block: %w", err)
	}
	final := filepath.Join(blockDir, BlockIDName(blockID))
	if err := os.Rename(tempName, final); err != nil {
		_ = os.Remove(tempName)
		return 0, fmt.Errorf("store block: %w", err)
	}
	return float64(written), nil
}

// SaveStagedBlocks concatenates the staged block files in the order their
// block list names them and stores the result like SaveUpload would: content
// addressed by its CRC32, carrying the .sga extension of the capture. The
// caller removes the staging files it is done with.
func SaveStagedBlocks(dir string, parts []string) (SavedFile, error) {
	if dir == "" {
		return SavedFile{}, errors.New("no upload directory configured")
	}
	temp, tempName, err := newUploadTemp(dir)
	if err != nil {
		return SavedFile{}, err
	}
	defer func() { _ = os.Remove(tempName) }()

	hash := crc32.NewIEEE()
	var written int64
	for _, part := range parts {
		source, err := os.Open(part)
		if err != nil {
			_ = temp.Close()
			return SavedFile{}, fmt.Errorf("read staged block: %w", err)
		}
		count, err := io.Copy(io.MultiWriter(temp, hash), source)
		_ = source.Close()
		if err != nil {
			_ = temp.Close()
			return SavedFile{}, fmt.Errorf("append staged block: %w", err)
		}
		written += count
	}
	if err := temp.Close(); err != nil {
		return SavedFile{}, fmt.Errorf("close upload: %w", err)
	}

	crc := fmt.Sprintf("%08x", hash.Sum32())
	name := crc + ".sga"
	saved := SavedFile{Path: name, Size: float64(written), CRC: crc}
	final := filepath.Join(dir, name)
	if _, err := os.Stat(final); err == nil {
		// The same bytes are already stored.
		return saved, nil
	}
	if err := os.Rename(tempName, final); err != nil {
		return SavedFile{}, fmt.Errorf("store upload: %w", err)
	}
	return saved, nil
}

// ParseBlockList reads the body of a CheckAndPublishFile commit: the XML the
// capture sends (`<?xml ...?><BlockList><Latest>00000000</Latest></BlockList>`)
// or the newline separated block ids Azure also accepts. The returned order
// is the order the blocks are stored in.
func ParseBlockList(body []byte) []string {
	var ids []string
	matches := blockListPattern.FindAllSubmatch(body, -1)
	for _, match := range matches {
		ids = append(ids, string(match[1]))
	}
	if len(ids) == 0 {
		for line := range strings.SplitSeq(string(body), "\n") {
			line = strings.TrimSuffix(strings.TrimSpace(line), "\r")
			if line != "" {
				ids = append(ids, line)
			}
		}
	}
	return ids
}

var blockListPattern = regexp.MustCompile(`<(?:Latest|Uncommitted|Committed)>([^<]+)<\/(?:Latest|Uncommitted|Committed)>`)

// intFieldOf reads an optional integer form field under any casing.
func intFieldOf(r *http.Request, name string) (int64, error) {
	raw := strings.TrimSpace(formValue(r, name))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number", name)
	}
	return value, nil
}

// boolFieldOf reads an optional boolean form field under any casing.
func boolFieldOf(r *http.Request, name string) (bool, error) {
	raw := strings.TrimSpace(formValue(r, name))
	if raw == "" {
		return false, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean", name)
	}
	return value, nil
}

// tagFields reads modTags, which the client sends either as repeated fields or
// as one comma-separated value, under any casing.
func tagFields(r *http.Request) ([]int64, error) {
	if r.MultipartForm == nil {
		return nil, nil
	}
	var out []int64
	for key, keyValues := range r.MultipartForm.Value {
		if !strings.EqualFold(key, "modTags") {
			continue
		}
		for _, raw := range keyValues {
			for piece := range strings.SplitSeq(raw, ",") {
				piece = strings.TrimSpace(piece)
				if piece == "" {
					continue
				}
				value, err := strconv.ParseInt(piece, 10, 64)
				if err != nil {
					return nil, errors.New("modTags must be numbers")
				}
				out = append(out, value)
			}
		}
	}
	return out, nil
}
