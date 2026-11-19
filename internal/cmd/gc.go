package cmd

import (
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dustin/go-humanize"

	i "github.com/luskaner/ageAPIServer/internal"
	"github.com/luskaner/ageAPIServer/internal/logger"
)

// gcStaleTempAge is how old an upload-* temporary has to be before the
// collector may remove it: a live publish is still writing to a fresh one.
const gcStaleTempAge = time.Hour

// gcReferences is the union the collector keeps: the stored names still
// referenced by a row and the ids of the mods that exist. It is a set, never a
// count: the stored name is the CRC32 of the content, so two mods can share a
// file and deleting one must not delete it.
type gcReferences struct {
	names map[string]bool
	mods  map[string]bool
}

// GCEntry is one file or directory the collector collected.
type GCEntry struct {
	Path  string
	Size  int64
	IsDir bool
}

// Garbage is the report of one scan.
type Garbage struct {
	Entries []GCEntry
	Bytes   int64
}

func (g *Garbage) add(path string, size int64, isDir bool) {
	g.Entries = append(g.Entries, GCEntry{Path: path, Size: size, IsDir: isDir})
	g.Bytes += size
}

// loadGCReferences reads the names and mod ids the database still owns. The
// rows are closed before the next query: the store runs on a single connection,
// and a second query while a rows handle is open would block forever.
func loadGCReferences(db *sql.DB) (gcReferences, error) {
	refs := gcReferences{
		names: make(map[string]bool),
		mods:  make(map[string]bool),
	}
	addName := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		refs.names[value] = true
		// A seeded row can carry an absolute URL; the file it names, when it
		// exists locally, is the base name.
		refs.names[filepath.Base(value)] = true
	}
	collect := func(query string, each func(string)) error {
		rows, err := db.Query(query)
		if err != nil {
			return err
		}
		for rows.Next() {
			var value string
			if err := rows.Scan(&value); err != nil {
				_ = rows.Close()
				return err
			}
			each(value)
		}
		err = rows.Err()
		if closeErr := rows.Close(); err == nil {
			err = closeErr
		}
		return err
	}
	if err := collect(`SELECT COALESCE(fileUrl, '') FROM mods`, addName); err != nil {
		return gcReferences{}, fmt.Errorf("read mod files: %w", err)
	}
	if err := collect(`SELECT COALESCE(imageName, '') FROM mod_images`, addName); err != nil {
		return gcReferences{}, fmt.Errorf("read mod images: %w", err)
	}
	if err := collect(`SELECT CAST(modId AS TEXT) FROM mods`, func(value string) {
		refs.mods[value] = true
	}); err != nil {
		return gcReferences{}, fmt.Errorf("read mod ids: %w", err)
	}
	return refs, nil
}

// CollectGarbage walks UploadDir and reports what nothing references. The
// calling server must be stopped: a publish in flight could be writing one of
// the files the scan classifies.
//
// The MatchContent files are out of scope: that store already expires and
// evicts them. A blocks/<modId> directory belongs to a mod whose upload never
// committed (or to a deleted one) when no such mod exists.
func CollectGarbage(uploadDir string, refs gcReferences, now time.Time) Garbage {
	var garbage Garbage
	if uploadDir == "" {
		return garbage
	}
	entries, err := os.ReadDir(uploadDir)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Warn("gc: cannot read %s: %s", uploadDir, err)
		}
		return garbage
	}
	for _, entry := range entries {
		name := entry.Name()
		full := filepath.Join(uploadDir, name)
		info, err := entry.Info()
		if err != nil {
			logger.Warn("gc: cannot stat %s: %s", full, err)
			continue
		}
		switch {
		case entry.IsDir() && name == "blocks":
			collectBlockDirs(full, refs, &garbage)
		case entry.IsDir() && name == "custom-files":
			// The MatchContent store owns its own lifetime.
		case entry.IsDir():
			logger.Detail("gc: leaving unknown directory %s", full)
		case strings.HasPrefix(name, "upload-") && now.Sub(info.ModTime()) < gcStaleTempAge:
			// A live upload is still writing to it.
		case refs.names[name]:
		default:
			garbage.add(full, info.Size(), false)
		}
	}
	return garbage
}

// collectBlockDirs adds the staging trees of the mods that no longer exist.
func collectBlockDirs(blocksDir string, refs gcReferences, garbage *Garbage) {
	entries, err := os.ReadDir(blocksDir)
	if err != nil {
		logger.Warn("gc: cannot read %s: %s", blocksDir, err)
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || refs.mods[entry.Name()] {
			continue
		}
		full := filepath.Join(blocksDir, entry.Name())
		garbage.add(full, dirSize(full), true)
	}
}

func dirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if info, infoErr := entry.Info(); infoErr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// ApplyGarbage removes what the scan reported. One failure does not stop the
// rest: the entry is logged and the loop continues.
func ApplyGarbage(garbage Garbage) {
	for _, entry := range garbage.Entries {
		var err error
		if entry.IsDir {
			err = os.RemoveAll(entry.Path)
		} else {
			err = os.Remove(entry.Path)
		}
		if err != nil {
			logger.Warn("gc: cannot remove %s: %s", entry.Path, err)
			continue
		}
		logger.Detail("gc: removed %s", entry.Path)
	}
}

// uploadGC implements --gc-uploads. It reports the orphans and, with apply,
// deletes exactly what it reported. The database is the only fatal part: a
// filesystem entry that cannot be read or removed is logged and skipped.
func uploadGC(cfg i.Config, apply bool) (error, int) {
	stores, err := openStoresFn(cfg.DB.Path, cfg.DB.BusyTimeoutMs, cfg.DB.MaxOpenConns)
	if err != nil {
		return fmt.Errorf("open database: %w", err), i.ErrDatabase
	}
	defer func() { _ = closeStoresFn(stores) }()

	refs, err := loadGCReferences(stores.DB)
	if err != nil {
		return err, i.ErrDatabase
	}
	garbage := CollectGarbage(cfg.Storage.UploadDir, refs, time.Now())
	for _, entry := range garbage.Entries {
		kind := "file"
		if entry.IsDir {
			kind = "directory"
		}
		logger.Info("gc: orphan %s %s (%s)", kind, entry.Path, humanize.Bytes(uint64(entry.Size)))
	}
	logger.Info("gc: %d files, %s", len(garbage.Entries), humanize.Bytes(uint64(garbage.Bytes)))
	if !apply {
		logger.Info("gc: dry run, pass --gc-apply to delete")
		return nil, 0
	}
	ApplyGarbage(garbage)
	logger.Ok("gc: removed %d entries", len(garbage.Entries))
	return nil, 0
}
