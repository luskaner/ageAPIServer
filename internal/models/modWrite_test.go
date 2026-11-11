package models

import (
	"context"
	"errors"
	"testing"
)

// seedUser returns the id of the user seedMods attaches to its mods.
func seedUser(t *testing.T, stores *Stores) int64 {
	t.Helper()
	user, err := stores.Users.EnsureUser(context.Background(), "76561198000000001", "Tester", "")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return user.UserID
}

func TestDeleteRemovesTheModAndItsChildren(t *testing.T) {
	stores := newStores(t)
	seedMods(t, stores)
	ctx := context.Background()
	userID := seedUser(t, stores)

	if err := stores.Mods.AddImage(ctx, 100, Image{ImageID: "img-1", ImageName: "a.png"}); err != nil {
		t.Fatalf("add image: %v", err)
	}
	if err := stores.Mods.Delete(ctx, userID, 100); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, err := stores.Mods.Detail(ctx, 100, Viewer{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("detail after delete = %v, want ErrNotFound", err)
	}
	for _, table := range []string{"mod_images", "mod_tag_map"} {
		var count int
		if err := stores.DB.QueryRow(`SELECT COUNT(*) FROM ` + table + ` WHERE modId = 100`).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s kept %d rows", table, count)
		}
	}
}

func TestDeleteRejectsBadInput(t *testing.T) {
	stores := newStores(t)
	seedMods(t, stores)
	ctx := context.Background()
	userID := seedUser(t, stores)

	if err := stores.Mods.Delete(ctx, 0, 100); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("anonymous = %v, want ErrUnauthorized", err)
	}
	if err := stores.Mods.Delete(ctx, userID, 0); !errors.Is(err, ErrValidation) {
		t.Fatalf("no modId = %v, want ErrValidation", err)
	}
	if err := stores.Mods.Delete(ctx, userID, 999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing mod = %v, want ErrNotFound", err)
	}
	// Mod 300 has no creator, so nobody owns it.
	if err := stores.Mods.Delete(ctx, userID, 300); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("creatorless mod = %v, want ErrUnauthorized", err)
	}
	// Mod 200 belongs to the same user, so a different user cannot delete it.
	other, err := stores.Users.EnsureUser(ctx, "76561198000000002", "Other", "")
	if err != nil {
		t.Fatalf("other user: %v", err)
	}
	if err := stores.Mods.Delete(ctx, other.UserID, 200); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("foreign mod = %v, want ErrUnauthorized", err)
	}
}

func TestReportRecordsTheConcern(t *testing.T) {
	stores := newStores(t)
	seedMods(t, stores)
	ctx := context.Background()
	userID := seedUser(t, stores)

	input := ReportInput{
		ModID:               100,
		ConcernCategoryID:   "7",
		ConcernCategoryName: "ReasonsOther",
		ConcernContent:      " ",
	}
	if err := stores.Mods.Report(ctx, userID, input); err != nil {
		t.Fatalf("report: %v", err)
	}

	var category, content string
	if err := stores.DB.QueryRow(
		`SELECT concernCategoryName, concernContent FROM mod_reports WHERE modId = 100`).
		Scan(&category, &content); err != nil {
		t.Fatalf("read report: %v", err)
	}
	if category != "ReasonsOther" || content != " " {
		t.Fatalf("stored %q/%q", category, content)
	}
}

func TestReportRejectsBadInput(t *testing.T) {
	stores := newStores(t)
	seedMods(t, stores)
	ctx := context.Background()
	userID := seedUser(t, stores)

	valid := ReportInput{ModID: 100, ConcernCategoryID: "7", ConcernCategoryName: "ReasonsOther"}

	if err := stores.Mods.Report(ctx, 0, valid); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("anonymous = %v, want ErrUnauthorized", err)
	}

	noMod := valid
	noMod.ModID = 0
	if err := stores.Mods.Report(ctx, userID, noMod); !errors.Is(err, ErrValidation) {
		t.Fatalf("no modId = %v, want ErrValidation", err)
	}

	noCategory := valid
	noCategory.ConcernCategoryID = ""
	if err := stores.Mods.Report(ctx, userID, noCategory); !errors.Is(err, ErrValidation) {
		t.Fatalf("no category id = %v, want ErrValidation", err)
	}

	noName := valid
	noName.ConcernCategoryName = ""
	if err := stores.Mods.Report(ctx, userID, noName); !errors.Is(err, ErrValidation) {
		t.Fatalf("no category name = %v, want ErrValidation", err)
	}

	missing := valid
	missing.ModID = 999999
	if err := stores.Mods.Report(ctx, userID, missing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing mod = %v, want ErrNotFound", err)
	}
}

func TestPublishCreatesAMod(t *testing.T) {
	stores := newStores(t)
	ctx := context.Background()
	userID := seedUser(t, stores)

	modID, err := stores.Mods.Publish(ctx, PublishInput{
		GameTitleID:   2,
		ModName:       "The Roman Dawn",
		Description:   "<p>desc</p>",
		ChangeList:    "<p>change</p>",
		ModVisibility: "Private",
		ModTypeID:     15,
		ModDiscussion: true,
		TagIDs:        []int64{15, 16, 15, 0},
		CreatorID:     userID,
		FileURL:       "abc.zip",
		FileCRC:       "abc",
		FileSize:      12.5,
		Images: []PublishImage{
			{DisplayIndex: 0, ImageName: "01.png", Thumbnail: "01.png", FileSize: 3},
			{DisplayIndex: 1, ImageName: "02.png", Thumbnail: "02.png", FileSize: 4},
		},
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if modID <= 0 {
		t.Fatalf("mod id = %d", modID)
	}

	detail, err := stores.Mods.Detail(ctx, modID, Viewer{})
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if detail.ModName != "The Roman Dawn" || detail.ModVisibility == nil || *detail.ModVisibility != "Private" {
		t.Fatalf("detail = %+v", detail)
	}
	if detail.ModStatusID != 4 {
		t.Fatalf("status = %d, want 4 (Draft) for a Private mod", detail.ModStatusID)
	}
	if detail.FileURL == nil || *detail.FileURL != "abc.zip" {
		t.Fatalf("fileUrl = %v", detail.FileURL)
	}
	if len(detail.ModTagIDs) != 2 {
		t.Fatalf("tags = %+v, want the two unique ids", detail.ModTagIDs)
	}
	if len(detail.ImageURLs) != 2 {
		t.Fatalf("images = %+v", detail.ImageURLs)
	}
}

func TestPublishUpdatesAMod(t *testing.T) {
	stores := newStores(t)
	seedMods(t, stores)
	ctx := context.Background()
	userID := seedUser(t, stores)

	modID, err := stores.Mods.Publish(ctx, PublishInput{
		ModID:       100,
		GameTitleID: 2,
		ModName:     "Chieftains v2",
		ChangeList:  "<p>fixed</p>",
		TagIDs:      []int64{16},
		CreatorID:   userID,
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if modID != 100 {
		t.Fatalf("mod id = %d, want 100", modID)
	}

	detail, err := stores.Mods.Detail(ctx, 100, Viewer{})
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if detail.ModName != "Chieftains v2" {
		t.Fatalf("name = %q", detail.ModName)
	}
	// An empty visibility falls back to Public, which is Published.
	if detail.ModStatusID != 1 {
		t.Fatalf("status = %d, want 1", detail.ModStatusID)
	}
	if len(detail.ModTagIDs) != 1 || detail.ModTagIDs[0] != 16 {
		t.Fatalf("tags = %+v, want only 16", detail.ModTagIDs)
	}
}

func TestPublishRejectsBadInput(t *testing.T) {
	stores := newStores(t)
	seedMods(t, stores)
	ctx := context.Background()
	userID := seedUser(t, stores)

	valid := PublishInput{GameTitleID: 2, ModName: "ok", CreatorID: userID}

	anonymous := valid
	anonymous.CreatorID = 0
	if _, err := stores.Mods.Publish(ctx, anonymous); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("anonymous = %v, want ErrUnauthorized", err)
	}

	noGame := valid
	noGame.GameTitleID = 0
	if _, err := stores.Mods.Publish(ctx, noGame); !errors.Is(err, ErrValidation) {
		t.Fatalf("no game = %v, want ErrValidation", err)
	}

	noName := valid
	noName.ModName = "   "
	if _, err := stores.Mods.Publish(ctx, noName); !errors.Is(err, ErrValidation) {
		t.Fatalf("no name = %v, want ErrValidation", err)
	}

	badVisibility := valid
	badVisibility.ModVisibility = "Secret"
	if _, err := stores.Mods.Publish(ctx, badVisibility); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad visibility = %v, want ErrValidation", err)
	}

	badTag := valid
	badTag.TagIDs = []int64{999}
	if _, err := stores.Mods.Publish(ctx, badTag); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad tag = %v, want ErrValidation", err)
	}

	missing := valid
	missing.ModID = 999999
	if _, err := stores.Mods.Publish(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing mod = %v, want ErrNotFound", err)
	}

	other, err := stores.Users.EnsureUser(ctx, "76561198000000002", "Other", "")
	if err != nil {
		t.Fatalf("other user: %v", err)
	}
	foreign := valid
	foreign.ModID = 200
	foreign.CreatorID = other.UserID
	if _, err := stores.Mods.Publish(ctx, foreign); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("foreign mod = %v, want ErrUnauthorized", err)
	}
}

func TestStatusForVisibility(t *testing.T) {
	cases := map[string]int64{"Public": 1, "Unlisted": 3, "Private": 4}
	for visibility, want := range cases {
		got, ok := statusForVisibility(visibility)
		if !ok || got != want {
			t.Fatalf("%s = %d/%v, want %d", visibility, got, ok, want)
		}
	}
	if _, ok := statusForVisibility("Secret"); ok {
		t.Fatal("Secret must not map to a status")
	}
}

func TestNullableIDAndUniqueIDs(t *testing.T) {
	if nullableID(0) != nil {
		t.Fatal("zero must become NULL")
	}
	if nullableID(7) != int64(7) {
		t.Fatal("a positive id must be kept")
	}
	got := uniqueIDs([]int64{3, 0, -1, 3, 4})
	if len(got) != 2 || got[0] != 3 || got[1] != 4 {
		t.Fatalf("uniqueIDs = %v", got)
	}
}
