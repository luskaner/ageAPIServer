package shared

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// maxPublishMemory is how much of a multipart body is kept in memory before the
// rest spills to a temporary file.
const maxPublishMemory = 8 << 20

// ImageMeta is one entry of the ImageMetaData field of Publish. imageId is null
// when the image is new and set when it already exists upstream.
type ImageMeta struct {
	ImageID        *string `json:"imageId"`
	DisplayIndex   int64   `json:"displayIndex"`
	ImageName      string  `json:"imageName"`
	ImageThumbnail string  `json:"imageThumbnail"`
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
		ModName:       strings.TrimSpace(r.FormValue("modName")),
		Description:   r.FormValue("description"),
		ChangeList:    r.FormValue("changeList"),
		ModVisibility: strings.TrimSpace(r.FormValue("modVisibility")),
	}

	var err error
	if form.GameTitleID, err = intField(r, "gameTitleId"); err != nil {
		return PublishForm{}, err
	}
	if form.ModID, err = intField(r, "modId"); err != nil {
		return PublishForm{}, err
	}
	if form.ModTypeID, err = intField(r, "modType"); err != nil {
		return PublishForm{}, err
	}
	if form.ModDiscussion, err = boolField(r, "modDiscussion"); err != nil {
		return PublishForm{}, err
	}
	if form.TagIDs, err = tagFields(r); err != nil {
		return PublishForm{}, err
	}
	if raw := r.FormValue("ImageMetaData"); raw != "" {
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

// SavedFile is the result of storing an uploaded file.
type SavedFile struct {
	// Path is the location relative to the upload directory.
	Path string
	Size float64
	CRC  string
}

// SaveUpload stores an uploaded file under dir and returns its relative path,
// size and CRC32. The stored name is the content hash, so uploading the same
// bytes twice does not duplicate the file.
func SaveUpload(dir string, header *multipart.FileHeader) (SavedFile, error) {
	if dir == "" {
		return SavedFile{}, errors.New("no upload directory configured")
	}
	source, err := header.Open()
	if err != nil {
		return SavedFile{}, fmt.Errorf("open upload: %w", err)
	}
	defer func() { _ = source.Close() }()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return SavedFile{}, fmt.Errorf("create upload dir: %w", err)
	}
	temp, err := os.CreateTemp(dir, "upload-*")
	if err != nil {
		return SavedFile{}, fmt.Errorf("create upload: %w", err)
	}
	tempName := temp.Name()
	defer func() { _ = os.Remove(tempName) }()

	hash := crc32.NewIEEE()
	written, err := io.Copy(io.MultiWriter(temp, hash), source)
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

// intField reads an optional integer form field.
func intField(r *http.Request, name string) (int64, error) {
	raw := strings.TrimSpace(r.FormValue(name))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number", name)
	}
	return value, nil
}

// boolField reads an optional boolean form field.
func boolField(r *http.Request, name string) (bool, error) {
	raw := strings.TrimSpace(r.FormValue(name))
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
// as one comma-separated value.
func tagFields(r *http.Request) ([]int64, error) {
	if r.MultipartForm == nil {
		return nil, nil
	}
	var out []int64
	for _, raw := range r.MultipartForm.Value["modTags"] {
		for _, piece := range strings.Split(raw, ",") {
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
	return out, nil
}
