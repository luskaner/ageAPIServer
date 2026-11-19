package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	i "github.com/luskaner/ageAPIServer/internal"
	"github.com/luskaner/ageAPIServer/internal/models"
)

// newGCStores opens a real database for a GC test and returns it with its
// upload directory. The caller closes the stores before running the GC, which
// opens its own handle.
func newGCStores(t *testing.T) (*models.Stores, string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	uploads := filepath.Join(dir, "uploads")
	if err := os.MkdirAll(uploads, 0o755); err != nil {
		t.Fatal(err)
	}
	stores, err := openStoresFn(path, 5000, 1)
	if err != nil {
		t.Fatalf("open stores: %v", err)
	}
	return stores, path, uploads
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func garbagePaths(garbage Garbage) map[string]bool {
	out := make(map[string]bool, len(garbage.Entries))
	for _, entry := range garbage.Entries {
		out[entry.Path] = true
	}
	return out
}

// TestCollectGarbageFindsTheFilesOfADeletedMod is the main flow: a published
// mod's archive and screenshot become garbage once the mod is gone.
func TestCollectGarbageFindsTheFilesOfADeletedMod(t *testing.T) {
	stores, _, uploads := newGCStores(t)
	defer func() { _ = closeStoresFn(stores) }()
	ctx := context.Background()

	user, err := stores.Users.EnsureUser(ctx, "76561198000000042", "GC", "")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	modID, err := stores.Mods.Publish(ctx, models.PublishInput{
		GameTitleID: 2,
		ModName:     "GC mod",
		CreatorID:   user.UserID,
		FileURL:     "abcd1234.zip",
		FileCRC:     "abcd1234",
		Images:      []models.PublishImage{{ImageName: "screenshot.png"}},
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	writeFile(t, filepath.Join(uploads, "abcd1234.zip"), "mod bytes")
	writeFile(t, filepath.Join(uploads, "screenshot.png"), "image bytes")

	refs, err := loadGCReferences(stores.DB)
	if err != nil {
		t.Fatalf("references: %v", err)
	}
	if garbage := CollectGarbage(uploads, refs, time.Now()); len(garbage.Entries) != 0 {
		t.Fatalf("a live mod's files were collected: %+v", garbage.Entries)
	}

	if err := stores.Mods.Delete(ctx, user.UserID, modID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	refs, err = loadGCReferences(stores.DB)
	if err != nil {
		t.Fatalf("references after delete: %v", err)
	}
	garbage := CollectGarbage(uploads, refs, time.Now())
	paths := garbagePaths(garbage)
	if !paths[filepath.Join(uploads, "abcd1234.zip")] || !paths[filepath.Join(uploads, "screenshot.png")] {
		t.Fatalf("garbage = %+v", garbage.Entries)
	}
	if garbage.Bytes != int64(len("mod bytes")+len("image bytes")) {
		t.Fatalf("bytes = %d", garbage.Bytes)
	}
}

// TestCollectGarbageKeepsSharedContent is the invariant of the CRC names: two
// mods publishing the same bytes share one file, so deleting one of them must
// not collect it.
func TestCollectGarbageKeepsSharedContent(t *testing.T) {
	stores, _, uploads := newGCStores(t)
	defer func() { _ = closeStoresFn(stores) }()
	ctx := context.Background()

	user, err := stores.Users.EnsureUser(ctx, "76561198000000043", "GC", "")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	var modIDs []int64
	for index := 0; index < 2; index++ {
		modID, err := stores.Mods.Publish(ctx, models.PublishInput{
			GameTitleID: 2,
			ModName:     "Shared " + string(rune('A'+index)),
			CreatorID:   user.UserID,
			FileURL:     "deadbeef.zip",
			FileCRC:     "deadbeef",
		})
		if err != nil {
			t.Fatalf("publish %d: %v", index, err)
		}
		modIDs = append(modIDs, modID)
	}
	shared := filepath.Join(uploads, "deadbeef.zip")
	writeFile(t, shared, "shared bytes")

	if err := stores.Mods.Delete(ctx, user.UserID, modIDs[0]); err != nil {
		t.Fatalf("delete: %v", err)
	}
	refs, err := loadGCReferences(stores.DB)
	if err != nil {
		t.Fatalf("references: %v", err)
	}
	if garbagePaths(CollectGarbage(uploads, refs, time.Now()))[shared] {
		t.Fatal("a file still referenced by another mod was collected")
	}
}

func TestCollectGarbageRemovesTheBlocksOfAnUnknownMod(t *testing.T) {
	uploads := t.TempDir()
	blocks := filepath.Join(uploads, "blocks")
	kept := filepath.Join(blocks, "42")
	orphan := filepath.Join(blocks, "999")
	for _, dir := range []string{kept, orphan} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(kept, "00000000"), "kept block")
	writeFile(t, filepath.Join(orphan, "00000000"), "orphan block")

	garbage := CollectGarbage(uploads, gcReferences{
		names: map[string]bool{},
		mods:  map[string]bool{"42": true},
	}, time.Now())
	if len(garbage.Entries) != 1 || garbage.Entries[0].Path != orphan || !garbage.Entries[0].IsDir {
		t.Fatalf("garbage = %+v", garbage.Entries)
	}
	if garbage.Bytes != int64(len("orphan block")) {
		t.Fatalf("bytes = %d", garbage.Bytes)
	}
}

func TestCollectGarbageLeavesTheMatchContentStoreAlone(t *testing.T) {
	uploads := t.TempDir()
	customFiles := filepath.Join(uploads, "custom-files")
	if err := os.MkdirAll(customFiles, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(customFiles, "save.aoe2mpgame"), "savegame")

	garbage := CollectGarbage(uploads, gcReferences{
		names: map[string]bool{},
		mods:  map[string]bool{},
	}, time.Now())
	if len(garbage.Entries) != 0 {
		t.Fatalf("garbage = %+v", garbage.Entries)
	}
}

// TestCollectGarbageKeepsFreshTemporaries pins the age condition: a live upload
// writes an upload-* file, an abandoned one is old enough to collect.
func TestCollectGarbageKeepsFreshTemporaries(t *testing.T) {
	uploads := t.TempDir()
	fresh := filepath.Join(uploads, "upload-fresh")
	stale := filepath.Join(uploads, "upload-stale")
	writeFile(t, fresh, "in flight")
	writeFile(t, stale, "abandoned")

	now := time.Now()
	if err := os.Chtimes(stale, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	garbage := CollectGarbage(uploads, gcReferences{
		names: map[string]bool{},
		mods:  map[string]bool{},
	}, now)
	paths := garbagePaths(garbage)
	if paths[fresh] {
		t.Fatal("a fresh temporary was collected")
	}
	if !paths[stale] {
		t.Fatalf("the stale temporary was kept: %+v", garbage.Entries)
	}
}

// TestUploadGCAppliesExactlyWhatItReported is the dry-run/apply contract.
func TestUploadGCAppliesExactlyWhatItReported(t *testing.T) {
	stores, dbPath, uploads := newGCStores(t)
	if err := closeStoresFn(stores); err != nil {
		t.Fatalf("close stores: %v", err)
	}
	orphan := filepath.Join(uploads, "cafebabe.zip")
	writeFile(t, orphan, "orphan bytes")

	cfg := i.DefaultConfig()
	cfg.DB.Path = dbPath
	cfg.Storage.UploadDir = uploads

	if err, code := uploadGC(cfg, false); err != nil || code != 0 {
		t.Fatalf("dry run: err=%v code=%d", err, code)
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Fatalf("the dry run deleted the file: %v", err)
	}

	if err, code := uploadGC(cfg, true); err != nil || code != 0 {
		t.Fatalf("apply: err=%v code=%d", err, code)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("the file survived the apply: %v", err)
	}
}

// TestCollectGarbageSurvivesUnreadableRoots pins the tolerant scan: a path that
// is not a directory is reported and skipped instead of aborting the run.
func TestCollectGarbageSurvivesUnreadableRoots(t *testing.T) {
	root := filepath.Join(t.TempDir(), "uploads")
	writeFile(t, root, "not a directory")
	refs := gcReferences{names: map[string]bool{}, mods: map[string]bool{}}
	if got := CollectGarbage(root, refs, time.Now()); len(got.Entries) != 0 {
		t.Fatalf("garbage = %+v", got.Entries)
	}
	if got := CollectGarbage("", refs, time.Now()); len(got.Entries) != 0 {
		t.Fatalf("garbage = %+v", got.Entries)
	}
}

func TestCollectGarbageLeavesUnknownDirectories(t *testing.T) {
	uploads := t.TempDir()
	if err := os.MkdirAll(filepath.Join(uploads, "some-other-store"), 0o755); err != nil {
		t.Fatal(err)
	}
	garbage := CollectGarbage(uploads, gcReferences{names: map[string]bool{}, mods: map[string]bool{}}, time.Now())
	if len(garbage.Entries) != 0 {
		t.Fatalf("garbage = %+v", garbage.Entries)
	}
}

func TestCollectBlockDirsSurvivesAnUnreadableFolder(t *testing.T) {
	var garbage Garbage
	collectBlockDirs(filepath.Join(t.TempDir(), "missing"), gcReferences{}, &garbage)
	if len(garbage.Entries) != 0 {
		t.Fatalf("garbage = %+v", garbage.Entries)
	}
}

// TestApplyGarbageContinuesAfterAFailure pins the per-entry error handling: one
// entry that cannot be removed does not stop the rest.
func TestApplyGarbageContinuesAfterAFailure(t *testing.T) {
	dir := t.TempDir()
	nonEmpty := filepath.Join(dir, "dir-as-file")
	if err := os.MkdirAll(nonEmpty, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(nonEmpty, "inner"), "x")
	victim := filepath.Join(dir, "victim.zip")
	writeFile(t, victim, "x")

	ApplyGarbage(Garbage{Entries: []GCEntry{{Path: nonEmpty, IsDir: false}, {Path: victim}}})

	if _, err := os.Stat(victim); !os.IsNotExist(err) {
		t.Fatalf("the second entry survived: %v", err)
	}
}

// TestLoadGCReferencesSkipsEmptyNames covers a row with no stored file: the
// empty name must not enter the reference set.
func TestLoadGCReferencesSkipsEmptyNames(t *testing.T) {
	stores, _, _ := newGCStores(t)
	defer func() { _ = closeStoresFn(stores) }()
	if _, err := stores.DB.Exec(
		`INSERT INTO mods (modId, gameTitleId, modName, modStatusId, createDate, lastUpdate)
		 VALUES (500, 2, 'no file', 2, 'x', 'x')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	refs, err := loadGCReferences(stores.DB)
	if err != nil {
		t.Fatalf("references: %v", err)
	}
	if !refs.mods["500"] {
		t.Fatal("the mod id is missing")
	}
	if refs.names[""] {
		t.Fatal("the empty name entered the reference set")
	}
}

func TestLoadGCReferencesReportsABrokenSchema(t *testing.T) {
	stores, _, _ := newGCStores(t)
	defer func() { _ = closeStoresFn(stores) }()
	if _, err := stores.DB.Exec(`DROP TABLE mod_images`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := loadGCReferences(stores.DB); err == nil {
		t.Fatal("a missing table was accepted")
	}
}

func TestUploadGCReportsAnUnusableDatabase(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	writeFile(t, blocker, "x")
	cfg := i.DefaultConfig()
	cfg.DB.Path = blocker
	cfg.Storage.UploadDir = dir

	err, code := uploadGC(cfg, false)
	if err == nil {
		t.Fatal("a database path that is a file was accepted")
	}
	if code != i.ErrDatabase {
		t.Fatalf("code = %d, want %d", code, i.ErrDatabase)
	}
}
