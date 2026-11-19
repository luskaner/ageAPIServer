package models

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/luskaner/ageAPIServer/internal/logger"
)

// ErrFileTooLarge is returned by CustomFilesStore.Stage and Put when the
// stream exceeds the configured per-file limit.
var ErrFileTooLarge = errors.New("custom file exceeds the maximum size")

const (
	autoFolder           = "auto"
	customFilesSubfolder = "custom-files"
	appFolderName        = "ageAPIServer"
	partPrefix           = "upload-"
	partSuffix           = ".part"
	fallbackSuffix       = ".bin"
	// removeAttempts and removeRetryDelay bound how long a delete keeps trying
	// while an in-flight download holds the file open.
	removeAttempts   = 3
	removeRetryDelay = 50 * time.Millisecond
)

// customFileExtensions is the allowlist of extensions accepted for the
// filename sent by the client. An empty extension is allowed: the file is then
// stored under the fallback name <matchId>.bin.
var customFileExtensions = map[string]bool{
	"aoe2mpgame":   true,
	"mpgame":       true,
	"aoe2rpl":      true,
	"aoe3rpl":      true,
	"aoe4rpl":      true,
	"aomrpl":       true,
	"aoe1rpl":      true,
	"rpl":          true,
	"aoe2scenario": true,
	"aoe3scenario": true,
	"aoe4scenario": true,
	"aomscenario":  true,
	"aoe1scenario": true,
	"scenario":     true,
	"scn":          true,
	"savegame":     true,
	"campaign":     true,
}

// CustomFile is the metadata of one stored custom file. The payload itself
// lives on disk; the index only keeps the bookkeeping needed to serve and
// expire it.
type CustomFile struct {
	Id       int32
	Filename string
	// OriginalFilename is the basename advertised to the game. Filename and
	// Path remain sanitized for disk storage; stripping Unicode from the
	// advertised name would make the client look for a different savegame.
	OriginalFilename string
	ContentType      string
	// Path is relative to the CustomFilesStore root.
	Path    string
	MD5     string
	CRC32   string
	Length  int
	Created time.Time
	expiry  time.Time
}

// Expiry is when the file becomes eligible for deletion by the sweeper.
func (f *CustomFile) Expiry() time.Time {
	return f.expiry
}

// CustomFileStage is a fully written but not yet indexed file. It is created by
// Stage (before the matchId is known) and turned into a CustomFile by Adopt.
type CustomFileStage struct {
	path   string
	md5    string
	crc32  string
	length int
	done   bool
}

// Length is the number of bytes streamed into the stage.
func (s *CustomFileStage) Length() int {
	return s.length
}

// CustomFilesOpts configures one CustomFilesStore.
type CustomFilesOpts struct {
	Enabled      bool
	Folder       string
	MaxFileSize  int64
	MaxTotalSize int64
	Expiry       time.Duration
	// Now overrides the clock. Tests use it to drive expiry deterministically.
	Now func() time.Time
}

// CustomFilesStore is the disk-backed store behind /api/MatchContent/. Each
// deployment gets one instance with its own root, index and sweeper.
type CustomFilesStore struct {
	mu           sync.Mutex
	files        map[int32]*CustomFile
	root         *os.Root
	folder       string
	totalBytes   int64
	expiry       time.Duration
	maxFileSize  int64
	maxTotalSize int64
	now          func() time.Time
	enabled      bool
	stop         chan struct{}
	// stopped is closed by the sweeper on its way out, so Close can wait for
	// the goroutine to be gone before dropping the state it reads.
	stopped   chan struct{}
	closeOnce sync.Once
}

// Enabled reports whether the store is open and accepting files.
func (m *CustomFilesStore) Enabled() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enabled && m.root != nil
}

// MaxFileSize is the per-file limit. Zero means no per-file limit.
func (m *CustomFilesStore) MaxFileSize() int64 {
	return m.maxFileSize
}

// TotalBytes is the accumulated size of the indexed files.
func (m *CustomFilesStore) TotalBytes() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.totalBytes
}

// Initialize opens the store. When Enabled is false, or the folder cannot be
// opened, the instance stays disabled and the endpoints answer 404.
func (m *CustomFilesStore) Initialize(opts *CustomFilesOpts) {
	m.files = make(map[int32]*CustomFile)
	m.now = time.Now
	if opts == nil {
		return
	}
	if opts.Now != nil {
		m.now = opts.Now
	}
	m.maxFileSize = opts.MaxFileSize
	m.maxTotalSize = opts.MaxTotalSize
	m.expiry = opts.Expiry
	if !opts.Enabled {
		return
	}
	base := opts.Folder
	if base == "" || base == autoFolder {
		base = filepath.Join(os.TempDir(), appFolderName, customFilesSubfolder)
	}
	if err := os.MkdirAll(base, os.ModePerm); err != nil {
		logger.Warn("MatchContent disabled: cannot create %s: %s", base, err)
		return
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		logger.Warn("MatchContent disabled: cannot open %s: %s", base, err)
		return
	}
	m.folder = base
	m.root = root
	m.enabled = true
	m.purgeStale()
	m.stop = make(chan struct{})
	m.stopped = make(chan struct{})
	go m.sweep()
}

// Stage streams write into a provisional file and returns it. The file is not
// visible to Open until Adopt is called; Discard removes it. maxFileSize is
// enforced while streaming and MD5 is computed in the same pass.
func (m *CustomFilesStore) Stage(write func(w io.Writer) error) (*CustomFileStage, error) {
	m.mu.Lock()
	if !m.enabled || m.root == nil {
		m.mu.Unlock()
		return nil, fs.ErrNotExist
	}
	folder := m.folder
	m.mu.Unlock()

	tmp, err := os.CreateTemp(folder, partPrefix+"*"+partSuffix)
	if err != nil {
		return nil, err
	}
	name := filepath.Base(tmp.Name())
	hash := md5.New()
	crc := crc32.NewIEEE()
	counter := &limitedWriter{limit: m.maxFileSize}
	writeErr := write(io.MultiWriter(hash, crc, counter, tmp))
	if writeErr == nil {
		writeErr = tmp.Sync()
	}
	closeErr := tmp.Close()
	if writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		m.removeStaged(name)
		if errors.Is(writeErr, ErrFileTooLarge) {
			return nil, ErrFileTooLarge
		}
		return nil, writeErr
	}
	return &CustomFileStage{
		path:   name,
		md5:    hex.EncodeToString(hash.Sum(nil)),
		crc32:  fmt.Sprintf("%08x", crc.Sum32()),
		length: int(counter.n),
	}, nil
}

// Adopt moves a staged file under its final name, indexes it and accounts for
// its size. The previous file with the same id, if any, is replaced.
func (m *CustomFilesStore) Adopt(stage *CustomFileStage, id int32, filename string, contentType string) (*CustomFile, error) {
	if stage == nil {
		return nil, fs.ErrNotExist
	}
	originalFilename := filepath.Base(strings.ReplaceAll(filename, "\\", "/"))
	filename = sanitizeFilename(filename)
	if filename == "" {
		filename = strconv.FormatInt(int64(id), 10) + fallbackSuffix
	}
	if originalFilename == "" || originalFilename == "." || originalFilename == ".." {
		originalFilename = filename
	}
	finalName := strconv.FormatInt(int64(id), 10) + "-" + filename
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	cf := &CustomFile{
		Id:               id,
		Filename:         filename,
		OriginalFilename: originalFilename,
		ContentType:      contentType,
		Path:             finalName,
		MD5:              stage.md5,
		CRC32:            stage.crc32,
		Length:           stage.length,
	}
	// The rename belongs under the same lock as the index update. Two uploads
	// racing on one id both write the same final name, so renaming outside the
	// lock lets the index be published for a file whose bytes are no longer the
	// ones on disk, and the download then announces a Content-Length it cannot
	// honour.
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.enabled || m.root == nil {
		return nil, fs.ErrNotExist
	}
	if err := m.root.Rename(stage.path, finalName); err != nil {
		return nil, err
	}
	stage.done = true
	now := m.now().UTC()
	cf.Created = now
	if m.expiry > 0 {
		cf.expiry = now.Add(m.expiry)
	}
	if old, ok := m.files[id]; ok {
		// Rename already replaced the target when the name is unchanged; only
		// remove the old payload when it lives under a different name.
		if old.Path != finalName {
			m.removeFile(old.Path)
		}
		m.totalBytes -= int64(old.Length)
	}
	m.files[id] = cf
	m.totalBytes += int64(cf.Length)
	m.evictLocked()
	return cf, nil
}

// Discard removes a staged file that was never adopted. It is a no-op once the
// stage has been adopted or already discarded.
func (m *CustomFilesStore) Discard(stage *CustomFileStage) {
	if stage == nil || stage.done {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.enabled || m.root == nil {
		return
	}
	stage.done = true
	m.removeFile(stage.path)
}

// Put stages and adopts a file in one call.
func (m *CustomFilesStore) Put(id int32, filename string, write func(w io.Writer) error) (*CustomFile, error) {
	stage, err := m.Stage(write)
	if err != nil {
		return nil, err
	}
	cf, err := m.Adopt(stage, id, filename, "")
	if err != nil {
		m.Discard(stage)
		return nil, err
	}
	return cf, nil
}

// Open returns the metadata and a streaming reader for one file. The caller
// owns the reader and must close it.
func (m *CustomFilesStore) Open(id int32) (*CustomFile, io.ReadCloser, bool) {
	// Keep the metadata and opened payload from the same upload generation.
	// Adopt can otherwise replace the path between lookup and Open.
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.enabled || m.root == nil {
		return nil, nil, false
	}
	cf, ok := m.files[id]
	if !ok {
		return nil, nil, false
	}
	f, err := m.root.Open(cf.Path)
	if err != nil {
		return nil, nil, false
	}
	return cf, f, true
}

// Delete removes one indexed file.
func (m *CustomFilesStore) Delete(id int32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.enabled || m.root == nil {
		return
	}
	if cf, ok := m.files[id]; ok {
		m.deleteLocked(cf)
	}
}

// Len is the number of indexed files.
func (m *CustomFilesStore) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.files)
}

// Close stops the sweeper and releases the root.
//
// It deliberately leaves the files on disk: the TTL is what removes them, not a
// clean exit. Wiping the folder here would make Expiry meaningless, and an
// unreachable leftover is purged by the next startup once it is older than the
// TTL.
func (m *CustomFilesStore) Close() {
	m.closeOnce.Do(func() {
		if m.stop != nil {
			close(m.stop)
			// The sweeper reads the index and the root under the lock, so it
			// has to be gone before the state it touches is dropped.
			<-m.stopped
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.root != nil {
			_ = m.root.Close()
		}
		m.root = nil
		m.enabled = false
		m.files = make(map[int32]*CustomFile)
		m.totalBytes = 0
	})
}

// removeStaged deletes a provisional file without holding the store lock: the
// sweeper or Close may have dropped the root while the upload was streaming.
func (m *CustomFilesStore) removeStaged(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.root == nil {
		return
	}
	m.removeFile(name)
}

// removeFile deletes name from the root, retrying a few times. Windows refuses
// to delete a file that a download still holds open, and without the retry the
// payload would stay on disk with no index entry left that could ever reach it.
func (m *CustomFilesStore) removeFile(name string) {
	var err error
	for attempt := range removeAttempts {
		if err = m.root.Remove(name); err == nil {
			return
		}
		if errors.Is(err, fs.ErrNotExist) {
			return
		}
		if attempt < removeAttempts-1 {
			time.Sleep(removeRetryDelay)
		}
	}
	logger.Detail("Could not delete match file %s: %s", name, err)
}

func (m *CustomFilesStore) deleteLocked(cf *CustomFile) {
	m.removeFile(cf.Path)
	delete(m.files, cf.Id)
	m.totalBytes -= int64(cf.Length)
}

// evictLocked deletes the oldest entries until the accumulated size fits
// maxTotalSize again. Caller must hold m.mu.
func (m *CustomFilesStore) evictLocked() {
	if m.maxTotalSize <= 0 {
		return
	}
	for m.totalBytes > m.maxTotalSize && len(m.files) > 0 {
		oldest := m.oldestLocked()
		if oldest == nil {
			return
		}
		logger.Detail("Evicting match file %d (%s) to stay under the total size limit", oldest.Id, oldest.Filename)
		m.deleteLocked(oldest)
	}
}

func (m *CustomFilesStore) oldestLocked() *CustomFile {
	var oldest *CustomFile
	for _, cf := range m.files {
		if oldest == nil || cf.Created.Before(oldest.Created) {
			oldest = cf
		}
	}
	return oldest
}

// purgeStale removes leftovers from previous runs: every provisional .part file
// and every regular file older than the TTL.
func (m *CustomFilesStore) purgeStale() {
	entries, err := os.ReadDir(m.folder)
	if err != nil {
		return
	}
	now := m.now()
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, partPrefix) && strings.HasSuffix(name, partSuffix) {
			_ = m.root.Remove(name)
			continue
		}
		if m.expiry <= 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > m.expiry {
			_ = m.root.Remove(name)
		}
	}
}

func (m *CustomFilesStore) sweep() {
	defer close(m.stopped)
	for {
		timer := time.NewTimer(m.nextExpiration())
		select {
		case <-m.stop:
			timer.Stop()
			return
		case <-timer.C:
			m.expire()
		}
	}
}

// expire deletes every entry whose TTL has passed. Exposed as a method (rather
// than inlined in sweep) so tests with an injected clock can drive it directly.
func (m *CustomFilesStore) expire() {
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, cf := range m.files {
		if cf.expiry.IsZero() || cf.expiry.After(now) {
			continue
		}
		logger.Detail("Removed expired match file %d (%s)", cf.Id, cf.Filename)
		m.deleteLocked(cf)
	}
}

func (m *CustomFilesStore) nextExpiration() time.Duration {
	const minInterval = 100 * time.Millisecond
	var next time.Time
	m.mu.Lock()
	for _, cf := range m.files {
		if cf.expiry.IsZero() {
			continue
		}
		if next.IsZero() || cf.expiry.Before(next) {
			next = cf.expiry
		}
	}
	m.mu.Unlock()
	if next.IsZero() {
		if m.expiry > minInterval {
			return m.expiry
		}
		return minInterval
	}
	d := max(next.Sub(m.now()), minInterval)
	return d
}

// limitedWriter fails as soon as a write would push the total past limit. A
// non-positive limit means no per-file limit, not "reject everything".
type limitedWriter struct {
	limit int64
	n     int64
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	if lw.limit > 0 && lw.n+int64(len(p)) > lw.limit {
		return 0, ErrFileTooLarge
	}
	lw.n += int64(len(p))
	return len(p), nil
}

var unsafeFilenameChars = strings.NewReplacer(
	"/", "_", "\\", "_",
)

// sanitizeFilename reduces a client-supplied filename to a single safe path
// component built only from [A-Za-z0-9._-]. It returns "" when nothing usable
// is left, in which case callers fall back to <matchId>.bin.
func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	name = unsafeFilenameChars.Replace(name)
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			b.WriteRune(r)
		}
	}
	name = strings.Trim(b.String(), ".")
	if name == "" || name == "." || name == ".." {
		return ""
	}
	return name
}

// KnownCustomFileExtension reports whether the client-provided filename carries
// an extension this server accepts. An empty extension is allowed because the
// store then saves it under <matchId>.bin.
func KnownCustomFileExtension(filename string) bool {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))
	if ext == "" {
		return true
	}
	return customFileExtensions[ext]
}
