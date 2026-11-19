// SPDX-License-Identifier: AGPL-3.0-only

package models

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func newStores(t *testing.T) *Stores {
	t.Helper()
	stores, err := Open(Options{Path: filepath.Join(t.TempDir(), "test.db"), MaxOpenConns: 1})
	if err != nil {
		t.Fatalf("open stores: %v", err)
	}
	t.Cleanup(func() { _ = stores.Close() })
	return stores
}

func seedMods(t *testing.T, stores *Stores) {
	t.Helper()
	ctx := context.Background()
	user, err := stores.Users.EnsureUser(ctx, "76561198000000001", "Tester", "")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	mods := []ModInput{
		{ModID: 100, GameTitleID: 2, ModName: "Chieftains", ModVersion: 1.2, ModTypeID: 15,
			ModStatusID: 1, CreatorID: user.UserID, Popular: 50, Downloads: 10, TagIDs: []int64{15, 16}},
		{ModID: 200, GameTitleID: 2, ModName: "AOE2 Balance", ModVersion: 2.0, ModTypeID: 16,
			ModStatusID: 2, CreatorID: user.UserID, Popular: 90, Downloads: 40, TagIDs: []int64{16}},
		{ModID: 300, GameTitleID: 3, ModName: "AOE3 Textures", ModVersion: 0.5, ModTypeID: 15,
			ModStatusID: 1, Popular: 10, TagIDs: []int64{15}},
	}
	for _, mod := range mods {
		if err := stores.Mods.Upsert(ctx, mod); err != nil {
			t.Fatalf("seed mod %d: %v", mod.ModID, err)
		}
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	for attempt := range 2 {
		stores, err := Open(Options{Path: path, MaxOpenConns: 1})
		if err != nil {
			t.Fatalf("open attempt %d: %v", attempt, err)
		}
		version, err := stores.System.SchemaVersion()
		if err != nil {
			t.Fatalf("schema version: %v", err)
		}
		if version != schemaVersion {
			t.Fatalf("got version %d, want %d", version, schemaVersion)
		}
		if err := stores.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	}
}

func TestSeedCatalogs(t *testing.T) {
	stores := newStores(t)
	ctx := context.Background()

	games, err := stores.Mods.Games(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 5 {
		t.Fatalf("got %d games, want 5", len(games))
	}
	if games[0].GameTitleID != 1 || games[4].GameTitleID != 1001 {
		t.Fatalf("unexpected game ids: %+v", games)
	}

	tags, err := stores.Mods.Tags(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) == 0 {
		t.Fatal("no tags seeded for game 2")
	}
	for _, tag := range tags {
		if tag.GameTitleID != 2 {
			t.Fatalf("tag %d belongs to game %d", tag.ModTypeID, tag.GameTitleID)
		}
	}

	// The catalog is per game: these are the row counts of the live response.
	wantPerGame := map[int64]int{1: 4, 2: 17, 3: 15, 4: 5, 1001: 15}
	all, err := stores.Mods.Tags(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	gotPerGame := map[int64]int{}
	for _, tag := range all {
		gotPerGame[tag.GameTitleID]++
	}
	for game, want := range wantPerGame {
		if gotPerGame[game] != want {
			t.Fatalf("game %d has %d tags, want %d", game, gotPerGame[game], want)
		}
	}
}

func TestFindAppliesFiltersAndPagination(t *testing.T) {
	stores := newStores(t)
	seedMods(t, stores)
	ctx := context.Background()

	page, err := stores.Mods.Find(ctx, FindFilter{GameTitleID: 2, Sort: "popular", Order: "desc", Limit: 10}, Viewer{})
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalCount != 2 || len(page.Mods) != 2 {
		t.Fatalf("got total=%d len=%d, want 2/2", page.TotalCount, len(page.Mods))
	}
	if page.Mods[0].ModID != 200 {
		t.Fatalf("popular sort put %d first", page.Mods[0].ModID)
	}

	page, err = stores.Mods.Find(ctx, FindFilter{GameTitleID: 2, Q: "chieftains", Limit: 10}, Viewer{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Mods) != 1 || page.Mods[0].ModID != 100 {
		t.Fatalf("q filter returned %+v", page.Mods)
	}

	page, err = stores.Mods.Find(ctx, FindFilter{GameTitleID: 2, Q: "*", Limit: 10}, Viewer{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Mods) != 2 {
		t.Fatalf("q=* must not filter, got %d", len(page.Mods))
	}

	page, err = stores.Mods.Find(ctx, FindFilter{GameTitleID: 2, Tags: []int64{16}, Limit: 10}, Viewer{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Mods) != 2 {
		t.Fatalf("tag inclusion returned %d", len(page.Mods))
	}

	page, err = stores.Mods.Find(ctx, FindFilter{GameTitleID: 2, Tags: []int64{-16}, Limit: 10}, Viewer{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Mods) != 0 {
		t.Fatalf("tag exclusion returned %d", len(page.Mods))
	}

	page, err = stores.Mods.Find(ctx, FindFilter{GameTitleID: 2, Sort: "popular", Order: "desc", Offset: 1, Limit: 1}, Viewer{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Mods) != 1 || page.Mods[0].ModID != 100 {
		t.Fatalf("second page returned %+v", page.Mods)
	}
	if page.TotalCount != 2 {
		t.Fatalf("totalCount = %d, want 2", page.TotalCount)
	}
}

func TestFindHydratesImagesAndTagsWithoutDeadlock(t *testing.T) {
	stores := newStores(t)
	seedMods(t, stores)
	ctx := context.Background()

	if err := stores.Mods.AddImage(ctx, 100, Image{ImageID: "img-1", DisplayIndex: 0, ImageName: "a.png"}); err != nil {
		t.Fatal(err)
	}
	if err := stores.Mods.AddImage(ctx, 200, Image{ImageID: "img-2", DisplayIndex: 0, ImageName: "b.png"}); err != nil {
		t.Fatal(err)
	}

	page, err := stores.Mods.Find(ctx, FindFilter{GameTitleID: 2, Limit: 10}, Viewer{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Mods) != 2 {
		t.Fatalf("got %d mods", len(page.Mods))
	}
	for _, mod := range page.Mods {
		if len(mod.ImageURLs) != 1 {
			t.Fatalf("mod %d has %d images", mod.ModID, len(mod.ImageURLs))
		}
		if len(mod.ModTagIDs) == 0 {
			t.Fatalf("mod %d has no tags", mod.ModID)
		}
	}
}

func TestSubscribeAndDownloadCountOnce(t *testing.T) {
	stores := newStores(t)
	seedMods(t, stores)
	ctx := context.Background()
	user, err := stores.Users.EnsureUser(ctx, "76561198000000002", "Player", "")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := stores.Mods.Subscribe(ctx, user.UserID, 100); err != nil {
		t.Fatal(err)
	}
	target, err := stores.Mods.DownloadTargetFor(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if target.DownloadCount != 10 {
		t.Fatalf("Subscribe changed the counter: %d", target.DownloadCount)
	}

	if _, err := stores.Mods.Download(ctx, user.UserID, 100); err != nil {
		t.Fatal(err)
	}
	target, err = stores.Mods.DownloadTargetFor(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if target.DownloadCount != 11 {
		t.Fatalf("first download did not count: %d", target.DownloadCount)
	}

	if _, err := stores.Mods.Download(ctx, user.UserID, 100); err != nil {
		t.Fatal(err)
	}
	target, err = stores.Mods.DownloadTargetFor(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if target.DownloadCount != 11 {
		t.Fatalf("second download counted twice: %d", target.DownloadCount)
	}
}

func TestInstallAndUninstallKeepTheCounterSane(t *testing.T) {
	stores := newStores(t)
	seedMods(t, stores)
	ctx := context.Background()
	user, err := stores.Users.EnsureUser(ctx, "76561198000000003", "Player", "")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := stores.Mods.Install(ctx, user.UserID, 100); err != nil {
		t.Fatal(err)
	}
	target, err := stores.Mods.DownloadTargetFor(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if target.InstallCount != 1 {
		t.Fatalf("install count = %d, want 1", target.InstallCount)
	}

	if err := stores.Mods.Uninstall(ctx, user.UserID, 100); err != nil {
		t.Fatal(err)
	}
	if err := stores.Mods.Uninstall(ctx, user.UserID, 100); err != nil {
		t.Fatal(err)
	}
	target, err = stores.Mods.DownloadTargetFor(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if target.InstallCount != 0 {
		t.Fatalf("install count = %d, want 0", target.InstallCount)
	}
}

func TestRateRefreshesAggregates(t *testing.T) {
	stores := newStores(t)
	seedMods(t, stores)
	ctx := context.Background()
	user, err := stores.Users.EnsureUser(ctx, "76561198000000004", "Rater", "")
	if err != nil {
		t.Fatal(err)
	}

	ratings, err := stores.Ratings.Rate(ctx, user.UserID, 100, 5)
	if err != nil {
		t.Fatal(err)
	}
	if ratings.Rating.TotalCount != 1 || ratings.Rating.Average != 5 {
		t.Fatalf("unexpected ratings: %+v", ratings.Rating)
	}

	detail, err := stores.Mods.Detail(ctx, 100, Viewer{UserID: user.UserID, LoggedOn: true})
	if err != nil {
		t.Fatal(err)
	}
	if detail.CurrentRating != 5 || detail.TotalRatings != 1 || detail.PositiveRatings != 1 {
		t.Fatalf("aggregates not denormalised: %+v", detail)
	}
	if detail.UserRating == nil || *detail.UserRating != 5 {
		t.Fatalf("userRating = %v", detail.UserRating)
	}

	if _, err := stores.Ratings.Rate(ctx, user.UserID, 100, 9); !errors.Is(err, ErrValidation) {
		t.Fatalf("out of range rating accepted: %v", err)
	}
}

func TestReviewsArePaged(t *testing.T) {
	stores := newStores(t)
	seedMods(t, stores)
	ctx := context.Background()

	for _, suffix := range []struct{ letter, digit string }{
		{"a", "1"},
		{"b", "2"},
		{"c", "3"},
	} {
		review := Review{
			ReviewID:   new("review-" + suffix.letter),
			ReviewerID: new("7656119800000000" + suffix.digit),
			Rating:     4,
			ReviewText: "fine",
			Submitted:  "2024-01-0" + suffix.digit + "T10:00:00Z",
		}
		if err := stores.Ratings.AddReview(ctx, review, 100); err != nil {
			t.Fatal(err)
		}
	}

	reviews, total, err := stores.Mods.Reviews(ctx, 100, 1, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(reviews) != 2 {
		t.Fatalf("got total=%d len=%d, want 3/2", total, len(reviews))
	}
	if reviews[0].Submitted < reviews[1].Submitted {
		t.Fatal("reviews are not newest first")
	}
}

func TestPollsExposeQuestionsAndVotes(t *testing.T) {
	stores := newStores(t)
	ctx := context.Background()

	_, err := stores.DB.ExecContext(ctx,
		`INSERT INTO polls (pollId, gameId, createdBy, created, startDateTime, endDateTime, expireDateTime, metadata)
		 VALUES ('11111111-1111-1111-1111-111111111111', 2, 'admin', '2024-01-01T00:00:00Z',
		         '2024-01-01T00:00:00Z', '2024-12-31T00:00:00Z', '2024-12-31T00:00:00Z', 'meta')`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := stores.DB.ExecContext(ctx,
		`INSERT INTO poll_questions (pollId, sortOrder, content, type, maxOptions)
		 VALUES ('11111111-1111-1111-1111-111111111111', 0, 'Best civ?', 'multiple-choice', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	questionID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stores.DB.ExecContext(ctx,
		`INSERT INTO poll_options (questionId, value, voteCount) VALUES (?, 'Britons', 3), (?, 'Franks', 1)`,
		questionID, questionID); err != nil {
		t.Fatal(err)
	}

	list, err := stores.Polls.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d polls", len(list))
	}

	poll, err := stores.Polls.Get(ctx, "11111111-1111-1111-1111-111111111111", Viewer{})
	if err != nil {
		t.Fatal(err)
	}
	if poll.ID != poll.PollID {
		t.Fatalf("id %q != pollId %q", poll.ID, poll.PollID)
	}
	if len(poll.Questions) != 1 || len(poll.Questions[0].Options) != 2 {
		t.Fatalf("unexpected poll shape: %+v", poll)
	}

	if _, err := stores.Polls.Get(ctx, "missing", Viewer{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing poll returned %v", err)
	}
}

func TestSteamTokenIsStable(t *testing.T) {
	stores := newStores(t)
	ctx := context.Background()

	first, err := stores.Users.UserBySteamToken(ctx, "not-base64-token")
	if err != nil {
		t.Fatal(err)
	}
	second, err := stores.Users.UserBySteamToken(ctx, "not-base64-token")
	if err != nil {
		t.Fatal(err)
	}
	if first.UserID != second.UserID {
		t.Fatalf("same token resolved to %d and %d", first.UserID, second.UserID)
	}
	if first.SteamID == "" {
		t.Fatal("empty steam id")
	}
}

func TestSessionRoundTrip(t *testing.T) {
	stores := newStores(t)
	ctx := context.Background()
	user, err := stores.Users.EnsureUser(ctx, "76561198000000009", "Session", "")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := stores.Users.CreateSession(ctx, user.UserID, 0)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := stores.Users.UserBySessionToken(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.UserID != user.UserID {
		t.Fatalf("got user %d, want %d", resolved.UserID, user.UserID)
	}
	if err := stores.Users.DeleteSession(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := stores.Users.UserBySessionToken(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted session still resolves: %v", err)
	}
}

// TestSeedRejectsAStaleCatalog covers the guard for a database created before
// the catalog became per game. The old table keys mod_types on modTypeId alone,
// so the per-game inserts are silently dropped and the catalog comes out
// partial; Seed must fail instead of serving it.
func TestSeedRejectsAStaleCatalog(t *testing.T) {
	stores := newStores(t)

	if _, err := stores.System.db.Exec(`DROP TABLE mod_types`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := stores.System.db.Exec(`CREATE TABLE mod_types (
		modTypeId   INTEGER PRIMARY KEY,
		modTypeName TEXT NOT NULL,
		gameTitleId INTEGER NOT NULL,
		sortOrder   INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		t.Fatalf("recreate: %v", err)
	}

	err := stores.System.Seed()
	if err == nil {
		t.Fatal("Seed accepted a catalog with missing rows")
	}
	if !strings.Contains(err.Error(), "predates the per-game catalog") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestSeedIsIdempotent checks that a second Seed leaves the catalog untouched.
func TestSeedIsIdempotent(t *testing.T) {
	stores := newStores(t)
	if err := stores.System.Seed(); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	tags, err := stores.Mods.Tags(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != len(seedModTypes) {
		t.Fatalf("got %d tags, want %d", len(tags), len(seedModTypes))
	}
}
