// SPDX-License-Identifier: AGPL-3.0-only

package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// FindFilter is the resolved form of a Find/Installed/My/Featured request. The
// route layer applies the schema defaults and the sentinel rules before it gets
// here, so every field is already meaningful.
type FindFilter struct {
	GameTitleID int64
	Q           string
	Filter      int64
	Creator     string
	ModID       int64
	Status      string
	Tags        []int64
	Sort        string
	Order       string
	Offset      int
	Limit       int

	// Scopes. At most one is set.
	FeaturedOnly    bool
	CreatorUserID   int64
	InstalledUserID int64
}

// ModPage is one page of mods plus the total count of the filtered set.
type ModPage struct {
	Mods       []Mod
	TotalCount int64
}

// DownloadTarget is the stored data a download URL is built from.
type DownloadTarget struct {
	ModID         int64
	GameTitleID   int64
	DownloadCount int64
	InstallCount  int64
	FileCRC       string
	FileURL       string
}

// ModStore owns games, tags, mods, images, subscriptions and flags.
type ModStore struct {
	db *sql.DB
}

// sortColumns is the whitelist of ORDER BY expressions. A client value is never
// interpolated: it is looked up here.
var sortColumns = map[string]string{
	"popular":       "m.popular",
	"recentpopular": "m.thirtyDayPopular, m.popular",
	"createdate":    "m.createDate",
	"lastupdate":    "m.lastUpdate",
	"downloads":     "m.downloads",
	"rating":        "m.currentRating",
	"subscribers":   "m.installs",
	"modname":       "m.sortName",
	"creatorname":   "u.displayName",
}

// Games returns the catalog.
func (s *ModStore) Games(ctx context.Context) ([]Game, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT gameTitleId, gameTitleName, gameVersion FROM games ORDER BY gameTitleId`)
	if err != nil {
		return nil, fmt.Errorf("query games: %w", err)
	}
	defer func() { _ = rows.Close() }()

	games := make([]Game, 0, 5)
	for rows.Next() {
		var game Game
		var version sql.NullString
		if err := rows.Scan(&game.GameTitleID, &game.GameTitleName, &version); err != nil {
			return nil, fmt.Errorf("scan game: %w", err)
		}
		game.GameVersion = nullString(version)
		games = append(games, game)
	}
	return games, rows.Err()
}

// Tags returns the mod types, optionally filtered by game.
func (s *ModStore) Tags(ctx context.Context, gameTitleID int64) ([]ModTag, error) {
	query := `SELECT modTypeId, modTypeName, gameTitleId FROM mod_types`
	var args []any
	if gameTitleID > 0 {
		query += ` WHERE gameTitleId = ?`
		args = append(args, gameTitleID)
	}
	query += ` ORDER BY sortOrder, gameTitleId, modTypeId`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query tags: %w", err)
	}
	defer func() { _ = rows.Close() }()

	tags := make([]ModTag, 0, 64)
	for rows.Next() {
		var tag ModTag
		if err := rows.Scan(&tag.ModTypeID, &tag.ModTypeName, &tag.GameTitleID); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

// baseSelect is the projection every mod query shares.
const baseSelect = `
SELECT m.modId, m.gameTitleId, g.gameTitleName, m.modName, m.description, m.modDescription,
       m.changeList, m.modVersion, m.verifiedVersion, m.modTypeId, m.clientId, t.modTypeName,
       m.modStatusId, m.modStatusMessage, m.modVisibility, m.creatorId, u.displayName, u.avatarUrl,
       m.allowDiscussion, m.discourseUrl, m.createDate, m.lastUpdate, m.updateAvailable,
       m.modFileSize, m.modDiskSize, m.popular, m.downloads, m.installs, m.likes,
       m.currentRating, m.positiveRatings, m.totalRatings, m.thumbnail, m.fileUrl,
       m.thirtyDayPopular, m.sortName, m.sendNotifications, m.fileCRC, m.metaData, m.reviewsCount,
       COUNT(*) OVER () AS totalCount
FROM mods m
LEFT JOIN games g ON g.gameTitleId = m.gameTitleId
LEFT JOIN mod_types t ON t.gameTitleId = m.gameTitleId AND t.modTypeId = m.modTypeId
LEFT JOIN users u ON u.userId = m.creatorId`

// Find returns one page of mods for the filter.
func (s *ModStore) Find(ctx context.Context, filter FindFilter, viewer Viewer) (*ModPage, error) {
	where, args := buildWhere(filter)

	order := "DESC"
	if strings.EqualFold(filter.Order, "asc") {
		order = "ASC"
	}
	column, ok := sortColumns[strings.ToLower(filter.Sort)]
	if !ok {
		column = sortColumns["popular"]
	}
	orderBy := " ORDER BY " + orderClause(column, order) + ", m.modId " + order

	query := baseSelect + where + orderBy
	if filter.Limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, filter.Limit, filter.Offset)
	} else if filter.Offset > 0 {
		query += ` LIMIT -1 OFFSET ?`
		args = append(args, filter.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query mods: %w", err)
	}
	defer func() { _ = rows.Close() }()
	// Materialise before any other query: with MaxOpenConns(1) a nested query
	// inside the row loop would wait for the connection this loop holds.
	scanned, total, err := scanModRows(rows)
	if err != nil {
		return nil, err
	}

	mods, err := s.hydrate(ctx, scanned, viewer)
	if err != nil {
		return nil, err
	}
	return &ModPage{Mods: mods, TotalCount: total}, nil
}

// orderClause expands a multi-column sort into "col1 DIR, col2 DIR".
func orderClause(column string, order string) string {
	parts := strings.Split(column, ",")
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part) + " " + order
	}
	return strings.Join(parts, ", ")
}

// buildWhere renders the WHERE clause of a mod query.
func buildWhere(filter FindFilter) (string, []any) {
	clauses := make([]string, 0, 8)
	args := make([]any, 0, 8)

	if filter.GameTitleID > 0 {
		clauses = append(clauses, "m.gameTitleId = ?")
		args = append(args, filter.GameTitleID)
	}
	if q := strings.TrimSpace(filter.Q); q != "" && q != "*" {
		pattern := "%" + escapeLike(q) + "%"
		clauses = append(clauses, `(m.modName LIKE ? ESCAPE '\' OR m.modDescription LIKE ? ESCAPE '\')`)
		args = append(args, pattern, pattern)
	}
	if filter.Filter > 0 {
		clauses = append(clauses, "m.modTypeId = ?")
		args = append(args, filter.Filter)
	}
	if creator := strings.TrimSpace(filter.Creator); creator != "" {
		clauses = append(clauses, `u.displayName LIKE ? ESCAPE '\'`)
		args = append(args, "%"+escapeLike(creator)+"%")
	}
	if filter.ModID > 0 {
		clauses = append(clauses, "m.modId = ?")
		args = append(args, filter.ModID)
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		if id, ok := modStatusID(status); ok {
			clauses = append(clauses, "m.modStatusId = ?")
			args = append(args, id)
		}
	}
	if filter.FeaturedOnly {
		// The only Featured captures with rows (the AOM client and the
		// website, both game 1001) carry modStatusId 5 "Featured" rows.
		clauses = append(clauses, "m.modStatusId = ?")
		args = append(args, 5)
	}
	if filter.CreatorUserID > 0 {
		clauses = append(clauses, "m.creatorId = ?")
		args = append(args, filter.CreatorUserID)
	}
	if filter.InstalledUserID > 0 {
		clauses = append(clauses, `EXISTS (SELECT 1 FROM subscriptions s
			WHERE s.modId = m.modId AND s.userId = ? AND s.installedAt IS NOT NULL)`)
		args = append(args, filter.InstalledUserID)
	}

	included, excluded := splitTags(filter.Tags)
	if len(included) > 0 {
		clauses = append(clauses, `EXISTS (SELECT 1 FROM mod_tag_map tm
			WHERE tm.modId = m.modId AND tm.gameTitleId = m.gameTitleId AND tm.modTypeId IN (`+placeholders(len(included))+`))`)
		args = append(args, argsFor(included)...)
	}
	if len(excluded) > 0 {
		clauses = append(clauses, `NOT EXISTS (SELECT 1 FROM mod_tag_map tm
			WHERE tm.modId = m.modId AND tm.gameTitleId = m.gameTitleId AND tm.modTypeId IN (`+placeholders(len(excluded))+`))`)
		args = append(args, argsFor(excluded)...)
	}

	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// splitTags separates inclusion ids from exclusion ids. A negative id excludes.
func splitTags(tags []int64) ([]int64, []int64) {
	included := make([]int64, 0, len(tags))
	excluded := make([]int64, 0, len(tags))
	for _, tag := range tags {
		switch {
		case tag > 0:
			included = append(included, tag)
		case tag < 0:
			excluded = append(excluded, -tag)
		}
	}
	return included, excluded
}

// escapeLike escapes the LIKE wildcards of a user supplied pattern.
func escapeLike(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}

// modStatusID maps the enum name to its id. Published is 2, the id every live
// detail carries alongside the name.
func modStatusID(name string) (int64, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "published":
		return 2, true
	case "featured":
		// The AOM Featured capture renders modStatusId 5 with modStatus
		// "Featured" for every row, so the name resolves to 5.
		return 5, true
	case "unlisted":
		return 3, true
	case "draft":
		return 4, true
	default:
		return 0, false
	}
}

// modRow is one scanned row of baseSelect.
type modRow struct {
	modID             int64
	gameTitleID       sql.NullInt64
	gameTitleName     sql.NullString
	modName           string
	description       sql.NullString
	modDescription    sql.NullString
	changeList        sql.NullString
	modVersion        sql.NullFloat64
	verifiedVersion   sql.NullString
	modTypeID         sql.NullInt64
	clientID          sql.NullString
	modTypeName       sql.NullString
	modStatusID       sql.NullInt64
	modStatusMessage  sql.NullString
	modVisibility     sql.NullString
	creatorID         sql.NullInt64
	creatorName       sql.NullString
	creatorAvatar     sql.NullString
	allowDiscussion   sql.NullInt64
	discourseURL      sql.NullString
	createDate        sql.NullString
	lastUpdate        sql.NullString
	updateAvailable   sql.NullInt64
	modFileSize       sql.NullFloat64
	modDiskSize       sql.NullFloat64
	popular           sql.NullInt64
	downloads         sql.NullInt64
	installs          sql.NullInt64
	likes             sql.NullInt64
	currentRating     sql.NullFloat64
	positiveRatings   sql.NullInt64
	totalRatings      sql.NullInt64
	thumbnail         sql.NullString
	fileURL           sql.NullString
	thirtyDayPopular  sql.NullInt64
	sortName          sql.NullString
	sendNotifications sql.NullInt64
	fileCRC           sql.NullString
	metaData          sql.NullString
	reviewsCount      sql.NullInt64
	totalCount        sql.NullInt64
}

// fallbackModTypeNames resolves the type ids that the live Tags catalog of a
// game does not list but published mods still carry: the AOE4 client uploads
// its mods as model Type 6 - "Extension" - for game 4, whose Tags capture
// stops at id 5, and its created mod renders modType "Extension" anyway.
var fallbackModTypeNames = map[[2]int64]string{
	{4, 6}: "Extension",
}

// scanModRows materialises every row and closes the cursor.
func scanModRows(rows *sql.Rows) ([]modRow, int64, error) {
	defer func() { _ = rows.Close() }()

	scanned := make([]modRow, 0, 32)
	var total int64
	for rows.Next() {
		var row modRow
		err := rows.Scan(
			&row.modID, &row.gameTitleID, &row.gameTitleName, &row.modName, &row.description,
			&row.modDescription, &row.changeList, &row.modVersion, &row.verifiedVersion,
			&row.modTypeID, &row.clientID, &row.modTypeName, &row.modStatusID,
			&row.modStatusMessage, &row.modVisibility, &row.creatorID, &row.creatorName,
			&row.creatorAvatar, &row.allowDiscussion, &row.discourseURL, &row.createDate,
			&row.lastUpdate, &row.updateAvailable, &row.modFileSize, &row.modDiskSize,
			&row.popular, &row.downloads, &row.installs, &row.likes, &row.currentRating,
			&row.positiveRatings, &row.totalRatings, &row.thumbnail, &row.fileURL,
			&row.thirtyDayPopular, &row.sortName, &row.sendNotifications, &row.fileCRC,
			&row.metaData, &row.reviewsCount, &row.totalCount,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan mod: %w", err)
		}
		if row.totalCount.Valid {
			total = row.totalCount.Int64
		}
		if row.modTypeID.Valid && row.gameTitleID.Valid {
			if name, ok := fallbackModTypeNames[[2]int64{row.gameTitleID.Int64, row.modTypeID.Int64}]; ok {
				row.modTypeName = sql.NullString{String: name, Valid: true}
			}
		}
		scanned = append(scanned, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate mods: %w", err)
	}
	return scanned, total, nil
}

// viewerState is the per-user data of a page, loaded in batch.
type viewerState struct {
	viewer     Viewer
	liked      map[int64]bool
	downloaded map[int64]bool
	installed  map[int64]bool
	flagged    map[int64]bool
	rating     map[int64]float64
	review     map[int64]string
	// updateable carries the mods whose subscribed version predates the
	// current one: the AOE4 capture renders updateAvailable true on the
	// Detail its creator reloads after publishing 1.2 over the 1.1 it
	// installed.
	updateable map[int64]bool
}

// hydrate fills images, tags, ratings and the viewer flags of a page.
func (s *ModStore) hydrate(ctx context.Context, scanned []modRow, viewer Viewer) ([]Mod, error) {
	if len(scanned) == 0 {
		return []Mod{}, nil
	}
	ids := make([]int64, len(scanned))
	for i, row := range scanned {
		ids[i] = row.modID
	}

	batch, err := s.loadBatch(ctx, ids, viewer)
	if err != nil {
		return nil, err
	}

	mods := make([]Mod, 0, len(scanned))
	for _, row := range scanned {
		mods = append(mods, row.toMod(batch.viewer, batch.ratings[row.modID], batch.images[row.modID], batch.tags[row.modID]))
	}
	return mods, nil
}

// modBatch is the per-page data of a set of mods: the images, tags, ratings
// and viewer flags are resolved in one round of statements instead of four
// queries per row.
type modBatch struct {
	images  map[int64][]Image
	tags    map[int64][]ModTag
	ratings map[int64]*Ratings
	viewer  viewerState
}

// loadBatch resolves every per-page lookup of one set of ids.
func (s *ModStore) loadBatch(ctx context.Context, ids []int64, viewer Viewer) (modBatch, error) {
	images, err := s.imagesFor(ctx, ids)
	if err != nil {
		return modBatch{}, err
	}
	tags, err := s.tagsFor(ctx, ids)
	if err != nil {
		return modBatch{}, err
	}
	ratings, err := s.ratingsFor(ctx, ids)
	if err != nil {
		return modBatch{}, err
	}
	state, err := s.viewerStateFor(ctx, ids, viewer)
	if err != nil {
		return modBatch{}, err
	}
	return modBatch{images: images, tags: tags, ratings: ratings, viewer: state}, nil
}

func (s *ModStore) imagesFor(ctx context.Context, ids []int64) (map[int64][]Image, error) {
	query := `SELECT modId, imageId, displayIndex, imageName, COALESCE(imageThumbnail, ''), imageFileSize
		FROM mod_images WHERE modId IN (` + placeholders(len(ids)) + `) ORDER BY modId, displayIndex`
	rows, err := s.db.QueryContext(ctx, query, argsFor(ids)...)
	if err != nil {
		return nil, fmt.Errorf("query images: %w", err)
	}
	defer func() { _ = rows.Close() }()

	images := make(map[int64][]Image, len(ids))
	for rows.Next() {
		var modID int64
		var image Image
		var size sql.NullFloat64
		if err := rows.Scan(&modID, &image.ImageID, &image.DisplayIndex, &image.ImageName, &image.ImageThumbnail, &size); err != nil {
			return nil, fmt.Errorf("scan image: %w", err)
		}
		// Every Detail and Related capture renders imageFileSize null even for
		// images uploaded with an imageFileSize, so the column is not
		// projected.
		_ = size
		images[modID] = append(images[modID], image)
	}
	return images, rows.Err()
}

func (s *ModStore) tagsFor(ctx context.Context, ids []int64) (map[int64][]ModTag, error) {
	// LEFT JOIN: a tag map row of a tag the game's catalog does not name
	// still renders its id - the AOE4 created mod answers modTagIds [26]
	// while its Tags catalog stops at 5, with modTagNames empty.
	query := `SELECT tm.modId, tm.modTypeId, COALESCE(t.modTypeName, '')
		FROM mod_tag_map tm
		LEFT JOIN mod_types t ON t.gameTitleId = tm.gameTitleId AND t.modTypeId = tm.modTypeId
		WHERE tm.modId IN (` + placeholders(len(ids)) + `) ORDER BY tm.modId, tm.modTypeId`
	rows, err := s.db.QueryContext(ctx, query, argsFor(ids)...)
	if err != nil {
		return nil, fmt.Errorf("query mod tags: %w", err)
	}
	defer func() { _ = rows.Close() }()

	tags := make(map[int64][]ModTag, len(ids))
	for rows.Next() {
		var modID int64
		var tag ModTag
		if err := rows.Scan(&modID, &tag.ModTypeID, &tag.ModTypeName); err != nil {
			return nil, fmt.Errorf("scan mod tag: %w", err)
		}
		tags[modID] = append(tags[modID], tag)
	}
	return tags, rows.Err()
}

func (s *ModStore) ratingsFor(ctx context.Context, ids []int64) (map[int64]*Ratings, error) {
	query := `SELECT modId, COUNT(*), AVG(rating)
		FROM mod_ratings WHERE modId IN (` + placeholders(len(ids)) + `) GROUP BY modId`
	rows, err := s.db.QueryContext(ctx, query, argsFor(ids)...)
	if err != nil {
		return nil, fmt.Errorf("query ratings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	ratings := make(map[int64]*Ratings, len(ids))
	for rows.Next() {
		var modID int64
		var total int64
		var average float64
		if err := rows.Scan(&modID, &total, &average); err != nil {
			return nil, fmt.Errorf("scan ratings: %w", err)
		}
		// The capture renders the average with one decimal (4.1 for a mod of
		// 125 ratings), so the render of every page carries that shape.
		ratings[modID] = &Ratings{
			Rating: RatingBreakdown{
				Average:    math.Round(average*10) / 10,
				TotalCount: total,
			},
		}
	}
	return ratings, rows.Err()
}

func (s *ModStore) viewerStateFor(ctx context.Context, ids []int64, viewer Viewer) (viewerState, error) {
	state := viewerState{
		viewer:     viewer,
		liked:      map[int64]bool{},
		downloaded: map[int64]bool{},
		installed:  map[int64]bool{},
		flagged:    map[int64]bool{},
		rating:     map[int64]float64{},
		review:     map[int64]string{},
		updateable: map[int64]bool{},
	}
	if !viewer.LoggedOn || viewer.UserID <= 0 {
		return state, nil
	}

	marks := []struct {
		query string
		dest  map[int64]bool
	}{
		{`SELECT modId FROM mod_likes WHERE userId = ? AND modId IN (` + placeholders(len(ids)) + `)`, state.liked},
		{`SELECT modId FROM flags WHERE userId = ? AND modId IN (` + placeholders(len(ids)) + `)`, state.flagged},
	}
	for _, mark := range marks {
		args := append([]any{viewer.UserID}, argsFor(ids)...)
		rows, err := s.db.QueryContext(ctx, mark.query, args...)
		if err != nil {
			return state, fmt.Errorf("query viewer marks: %w", err)
		}
		for rows.Next() {
			var modID int64
			if err := rows.Scan(&modID); err != nil {
				_ = rows.Close()
				return state, fmt.Errorf("scan viewer mark: %w", err)
			}
			mark.dest[modID] = true
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return state, fmt.Errorf("iterate viewer marks: %w", err)
		}
		_ = rows.Close()
	}

	subArgs := append([]any{viewer.UserID}, argsFor(ids)...)
	rows, err := s.db.QueryContext(ctx,
		`SELECT modId, downloadedAt, installedAt FROM subscriptions
		 WHERE userId = ? AND modId IN (`+placeholders(len(ids))+`)`, subArgs...)
	if err != nil {
		return state, fmt.Errorf("query subscriptions: %w", err)
	}
	for rows.Next() {
		var modID int64
		var downloadedAt, installedAt sql.NullString
		if err := rows.Scan(&modID, &downloadedAt, &installedAt); err != nil {
			_ = rows.Close()
			return state, fmt.Errorf("scan subscription: %w", err)
		}
		state.downloaded[modID] = downloadedAt.Valid
		state.installed[modID] = installedAt.Valid
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return state, fmt.Errorf("iterate subscriptions: %w", err)
	}
	_ = rows.Close()

	// A subscription that was stamped with an older installed version flags
	// an update: the capture answers updateAvailable true on the Detail the
	// creator reloads after publishing over its installed 1.1.
	updateArgs := append([]any{viewer.UserID}, argsFor(ids)...)
	rows, err = s.db.QueryContext(ctx,
		`SELECT s.modId FROM subscriptions s
		 JOIN mods m ON m.modId = s.modId
		 WHERE s.userId = ? AND s.modId IN (`+placeholders(len(ids))+`)
		   AND s.installedVersion IS NOT NULL
		   AND s.installedVersion < COALESCE(NULLIF(m.modVersion, 0), 1.1)`, updateArgs...)
	if err != nil {
		return state, fmt.Errorf("query installed versions: %w", err)
	}
	for rows.Next() {
		var modID int64
		if err := rows.Scan(&modID); err != nil {
			_ = rows.Close()
			return state, fmt.Errorf("scan installed version: %w", err)
		}
		state.updateable[modID] = true
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return state, fmt.Errorf("iterate installed versions: %w", err)
	}
	_ = rows.Close()

	ratingArgs := append([]any{viewer.UserID}, argsFor(ids)...)
	rows, err = s.db.QueryContext(ctx,
		`SELECT modId, rating FROM mod_ratings WHERE userId = ? AND modId IN (`+placeholders(len(ids))+`)`,
		ratingArgs...)
	if err != nil {
		return state, fmt.Errorf("query user ratings: %w", err)
	}
	for rows.Next() {
		var modID int64
		var rating int64
		if err := rows.Scan(&modID, &rating); err != nil {
			_ = rows.Close()
			return state, fmt.Errorf("scan user rating: %w", err)
		}
		state.rating[modID] = float64(rating)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return state, fmt.Errorf("iterate user ratings: %w", err)
	}
	_ = rows.Close()

	if viewer.SteamID != "" {
		reviewArgs := append([]any{viewer.SteamID}, argsFor(ids)...)
		rows, err = s.db.QueryContext(ctx,
			`SELECT modId, reviewText FROM reviews WHERE reviewerId = ? AND modId IN (`+placeholders(len(ids))+`)`,
			reviewArgs...)
		if err != nil {
			return state, fmt.Errorf("query user reviews: %w", err)
		}
		for rows.Next() {
			var modID int64
			var text string
			if err := rows.Scan(&modID, &text); err != nil {
				_ = rows.Close()
				return state, fmt.Errorf("scan user review: %w", err)
			}
			state.review[modID] = text
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return state, fmt.Errorf("iterate user reviews: %w", err)
		}
		_ = rows.Close()
	}

	return state, nil
}

// toMod renders the ModMetaData view of a row.
func (r modRow) toMod(state viewerState, ratings *Ratings, images []Image, tags []ModTag) Mod {
	mod := Mod{
		ModID:             r.modID,
		GameTitleID:       nullInt(r.gameTitleID),
		GameTitleName:     nullString(r.gameTitleName),
		ModName:           r.modName,
		Description:       nullString(r.description),
		ModDescription:    nullString(r.modDescription),
		ChangeList:        nullString(r.changeList),
		ModVersion:        r.modVersion.Float64,
		VerifiedVersion:   nullString(r.verifiedVersion),
		ModTypeID:         nullInt(r.modTypeID),
		ClientID:          nullString(r.clientID),
		ModType:           listModTypeName(r.modTypeID, r.modTypeName),
		MetaData:          rawJSON(r.metaData),
		ModStatusID:       nullInt(r.modStatusID),
		ModStatusMessage:  nullString(r.modStatusMessage),
		ModVisibility:     nullString(r.modVisibility),
		CreatorID:         nullInt(r.creatorID),
		CreatorName:       nullString(r.creatorName),
		CreatorAvatarURL:  nullString(r.creatorAvatar),
		IsLoggedOn:        state.viewer.LoggedOn,
		AllowDiscussion:   NumberToBool(r.allowDiscussion.Int64),
		DiscourseURL:      nullString(r.discourseURL),
		CreateDate:        r.createDate.String,
		LastUpdate:        r.lastUpdate.String,
		UpdateAvailable:   NumberToBool(r.updateAvailable.Int64) || state.updateable[r.modID],
		ModFileSize:       r.modFileSize.Float64,
		ModDiskSize:       nullFloat(r.modDiskSize),
		Popular:           nullInt(r.popular),
		Downloads:         nullInt(r.downloads),
		Installs:          nullInt(r.installs),
		Likes:             nullInt(r.likes),
		Ratings:           ratingsOrZero(ratings),
		CurrentRating:     r.currentRating.Float64,
		PositiveRatings:   r.positiveRatings.Int64,
		TotalRatings:      r.totalRatings.Int64,
		Thumbnail:         r.thumbnail.String,
		FileURL:           nullString(r.fileURL),
		ImageURLs:         images,
		ThirtyDayPopular:  nullInt(r.thirtyDayPopular),
		SortName:          nullString(r.sortName),
		SendNotifications: NumberToBool(r.sendNotifications.Int64),
	}
	if r.modStatusID.Valid {
		status := ModStatusName(r.modStatusID.Int64)
		mod.ModStatus = &status
	}
	if ratings != nil {
		ratings.ReviewsCount = r.reviewsCount.Int64
	}
	if state.viewer.LoggedOn {
		mod.IsOwner = r.creatorID.Valid && r.creatorID.Int64 == state.viewer.UserID
		mod.UserLiked = new(state.liked[r.modID])
		mod.UserDownloaded = new(state.downloaded[r.modID])
		mod.UserInstalled = state.installed[r.modID]
		mod.UserFlagged = new(state.flagged[r.modID])
		if rating, ok := state.rating[r.modID]; ok {
			mod.UserRating = &rating
		}
		if review, ok := state.review[r.modID]; ok {
			mod.UserReview = &review
		}
	}
	applyTags(&mod.ModTags, &mod.ModTagIDs, &mod.ModTagNames, tags)
	return mod
}

// toDetail renders the DetailResponse view of a row.
func (r modRow) toDetail(state viewerState, ratings *Ratings, images []Image, tags []ModTag) Detail {
	detail := Detail{
		ModID:             r.modID,
		GameTitleID:       r.gameTitleID.Int64,
		GameTitleName:     r.gameTitleName.String,
		ModName:           r.modName,
		Description:       r.description.String,
		ModDescription:    r.modDescription.String,
		ChangeList:        r.changeList.String,
		ModVersion:        r.modVersion.Float64,
		VerifiedVersion:   nullString(r.verifiedVersion),
		ModTypeID:         nullInt(r.modTypeID),
		ClientID:          nullString(r.clientID),
		ModType:           nullString(r.modTypeName),
		MetaData:          rawJSON(r.metaData),
		ModStatusID:       r.modStatusID.Int64,
		ModStatus:         ModStatusName(r.modStatusID.Int64),
		ModStatusMessage:  nullString(r.modStatusMessage),
		ModVisibility:     nullString(r.modVisibility),
		CreatorID:         nullInt(r.creatorID),
		CreatorName:       nullString(r.creatorName),
		CreatorAvatarURL:  nullString(r.creatorAvatar),
		IsLoggedOn:        state.viewer.LoggedOn,
		AllowDiscussion:   NumberToBool(r.allowDiscussion.Int64),
		DiscourseURL:      nullString(r.discourseURL),
		CreateDate:        nullString(r.createDate),
		LastUpdate:        nullString(r.lastUpdate),
		UpdateAvailable:   orBool(nullBool(r.updateAvailable), state.updateable[r.modID]),
		ModFileSize:       nullFloat(r.modFileSize),
		ModDiskSize:       nullFloat(r.modDiskSize),
		Popular:           nullInt(r.popular),
		Downloads:         nullInt(r.downloads),
		Installs:          nullInt(r.installs),
		Likes:             nullInt(r.likes),
		Ratings:           ratingsOrZero(ratings),
		CurrentRating:     r.currentRating.Float64,
		PositiveRatings:   r.positiveRatings.Int64,
		TotalRatings:      r.totalRatings.Int64,
		Thumbnail:         nullString(r.thumbnail),
		FileURL:           nullString(r.fileURL),
		ImageURLs:         images,
		ThirtyDayPopular:  nullInt(r.thirtyDayPopular),
		SortName:          nullString(r.sortName),
		SendNotifications: NumberToBool(r.sendNotifications.Int64),
	}
	if ratings != nil {
		ratings.ReviewsCount = r.reviewsCount.Int64
	}
	if state.viewer.LoggedOn {
		owner := r.creatorID.Valid && r.creatorID.Int64 == state.viewer.UserID
		detail.IsOwner = &owner
		detail.UserLiked = new(state.liked[r.modID])
		detail.UserDownloaded = new(state.downloaded[r.modID])
		detail.UserInstalled = new(state.installed[r.modID])
		detail.UserFlagged = new(state.flagged[r.modID])
		if rating, ok := state.rating[r.modID]; ok {
			detail.UserRating = &rating
		}
		if review, ok := state.review[r.modID]; ok {
			detail.UserReview = &review
		}
	}
	// The Detail captures render modTags null even with tags resolved - the
	// ids (and the names in modTagNames) are the arrays it carries - so the
	// names array the list rows show is collected into a discarded slice.
	var listNames []string
	applyTags(&listNames, &detail.ModTagIDs, &detail.ModTagNames, tags)
	return detail
}

// listModTypeName names the mod type of a list row. The AOM Featured list
// answers "Unknown" for a type id its Tags catalog does not sell (type 1 of
// game 1001), while the Detail of the same mod leaves the name null - the
// Detail keeps nullString and only the lists reach for the placeholder.
func listModTypeName(modTypeID sql.NullInt64, name sql.NullString) *string {
	if name.Valid && name.String != "" {
		return &name.String
	}
	if modTypeID.Valid {
		unknown := "Unknown"
		return &unknown
	}
	return nil
}

func applyTags(names *[]string, ids *[]int64, tagNames *[]string, tags []ModTag) {
	if len(tags) == 0 {
		return
	}
	*ids = make([]int64, 0, len(tags))
	*names = make([]string, 0, len(tags))
	*tagNames = make([]string, 0, len(tags))
	for _, tag := range tags {
		*ids = append(*ids, tag.ModTypeID)
		// A tag the catalog does not name contributes the id only: the AOE4
		// created mod of the capture carries modTagIds [26] with empty
		// modTagNames.
		if tag.ModTypeName == "" {
			continue
		}
		*names = append(*names, tag.ModTypeName)
		*tagNames = append(*tagNames, tag.ModTypeName)
	}
}

func rawJSON(value sql.NullString) json.RawMessage {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil
	}
	return json.RawMessage(value.String)
}

// IsAbsoluteURL reports whether raw already names a scheme, which is how a
// stored media name or file URL that points at a CDN is told apart from one
// this server has to resolve against its own base.
func IsAbsoluteURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.IsAbs()
}

// Detail returns one mod.
func (s *ModStore) Detail(ctx context.Context, modID int64, viewer Viewer) (*Detail, error) {
	rows, err := s.db.QueryContext(ctx, baseSelect+` WHERE m.modId = ?`, modID)
	if err != nil {
		return nil, fmt.Errorf("query mod detail: %w", err)
	}
	defer func() { _ = rows.Close() }()
	scanned, _, err := scanModRows(rows)
	if err != nil {
		return nil, err
	}
	if len(scanned) == 0 {
		return nil, ErrNotFound
	}
	row := scanned[0]
	batch, err := s.loadBatch(ctx, []int64{row.modID}, viewer)
	if err != nil {
		return nil, err
	}
	detail := row.toDetail(batch.viewer, batch.ratings[row.modID], batch.images[row.modID], batch.tags[row.modID])
	return &detail, nil
}

// RelatedPageSize is how many related mods the endpoint returns. The captures
// always carry six, and their totalCount is that same six.
const RelatedPageSize = 6

// Related returns mods of the same game sharing at least one tag, falling back
// to the same game when there is no shared tag.
func (s *ModStore) Related(ctx context.Context, modID int64, viewer Viewer) (*ModPage, error) {
	var gameTitleID int64
	err := s.db.QueryRowContext(ctx, `SELECT gameTitleId FROM mods WHERE modId = ?`, modID).Scan(&gameTitleID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query related game: %w", err)
	}

	query := baseSelect + `
		WHERE m.gameTitleId = ? AND m.modId <> ?
		  AND EXISTS (SELECT 1 FROM mod_tag_map mine
		              JOIN mod_tag_map theirs ON theirs.gameTitleId = mine.gameTitleId AND theirs.modTypeId = mine.modTypeId
		              WHERE mine.modId = ? AND theirs.modId = m.modId)
		ORDER BY m.popular DESC, m.modId DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, query, gameTitleID, modID, modID, RelatedPageSize)
	if err != nil {
		return nil, fmt.Errorf("query related mods: %w", err)
	}
	defer func() { _ = rows.Close() }()
	scanned, total, err := scanModRows(rows)
	if err != nil {
		return nil, err
	}
	if len(scanned) == 0 {
		fallback := baseSelect + ` WHERE m.gameTitleId = ? AND m.modId <> ?
			ORDER BY m.popular DESC, m.modId DESC LIMIT ?`
		rows, err = s.db.QueryContext(ctx, fallback, gameTitleID, modID, RelatedPageSize)
		if err != nil {
			return nil, fmt.Errorf("query related fallback: %w", err)
		}
		defer func() { _ = rows.Close() }()
		scanned, total, err = scanModRows(rows)
		if err != nil {
			return nil, err
		}
	}
	mods, err := s.hydrate(ctx, scanned, viewer)
	if err != nil {
		return nil, err
	}
	return &ModPage{Mods: mods, TotalCount: total}, nil
}

// Owns reports whether the user is the creator of the mod. ErrNotFound marks
// a mod that does not exist, so callers answer it like Publish does.
func (s *ModStore) Owns(ctx context.Context, modID int64, userID int64) (bool, error) {
	if modID <= 0 {
		return false, ErrNotFound
	}
	var creatorID sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT creatorId FROM mods WHERE modId = ?`, modID).Scan(&creatorID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, fmt.Errorf("read mod creator: %w", err)
	}
	return creatorID.Valid && creatorID.Int64 == userID, nil
}

// AttachFile commits the download file of a mod: the file fields of the AOE4
// CheckAndPublishFile commit - fileUrl, fileCRC, modFileSize and modDiskSize
// equal (the capture block commits 1509669 in both) - and lastUpdate moves to
// the commit time, which the Detail capture shows sharing the commit second.
func (s *ModStore) AttachFile(ctx context.Context, modID int64, url string, crc string, size float64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin attach: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM mods WHERE modId = ?`, modID).Scan(&exists); err != nil {
		return fmt.Errorf("check mod: %w", err)
	}
	if exists == 0 {
		return ErrNotFound
	}
	stamp := time.Now().UTC().Format("2006-01-02T15:04:05.000")
	if _, err := tx.ExecContext(ctx,
		`UPDATE mods SET fileUrl = ?, fileCRC = ?, modFileSize = ?, modDiskSize = ?, lastUpdate = ?
		 WHERE modId = ?`,
		nullEmpty(url), crc, size, size, stamp, modID); err != nil {
		return fmt.Errorf("attach file: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit attach: %w", err)
	}
	return nil
}

// DownloadTargetFor returns the stored data of a mod download.
func (s *ModStore) DownloadTargetFor(ctx context.Context, modID int64) (*DownloadTarget, error) {
	var target DownloadTarget
	err := s.db.QueryRowContext(ctx,
		`SELECT modId, gameTitleId, downloads, installs, COALESCE(fileCRC, ''), COALESCE(fileUrl, '')
		 FROM mods WHERE modId = ?`, modID).
		Scan(&target.ModID, &target.GameTitleID, &target.DownloadCount, &target.InstallCount,
			&target.FileCRC, &target.FileURL)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query download target: %w", err)
	}
	return &target, nil
}

// Subscribe records the subscription without touching the counters: the AOE4
// client calls Subscribe and Download for the same mod, so counting in both
// would double every download.
func (s *ModStore) Subscribe(ctx context.Context, userID int64, modID int64) (*DownloadTarget, error) {
	return s.subscribe(ctx, userID, modID, false)
}

// Download records the subscription and counts the download once per user.
func (s *ModStore) Download(ctx context.Context, userID int64, modID int64) (*DownloadTarget, error) {
	return s.subscribe(ctx, userID, modID, true)
}

func (s *ModStore) subscribe(ctx context.Context, userID int64, modID int64, count bool) (*DownloadTarget, error) {
	if userID <= 0 {
		return nil, ErrUnauthorized
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin subscribe: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM mods WHERE modId = ?`, modID).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("check mod: %w", err)
	}
	if exists == 0 {
		return nil, ErrNotFound
	}

	var downloadedAt sql.NullString
	var downloadCounted int64
	err = tx.QueryRowContext(ctx,
		`SELECT downloadedAt, downloadCounted FROM subscriptions WHERE userId = ? AND modId = ?`,
		userID, modID).Scan(&downloadedAt, &downloadCounted)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("read subscription: %w", err)
	}
	firstCount := errors.Is(err, sql.ErrNoRows) || downloadCounted == 0

	now := time.Now().UTC().Format(time.RFC3339)
	// The version the subscription carries is stamped so a later publish of a
	// newer version flags the update (updateAvailable of the AOE4 capture).
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO subscriptions (userId, modId, downloadedAt, installedVersion)
		 VALUES (?, ?, ?, COALESCE((SELECT COALESCE(NULLIF(modVersion, 0), 1.1) FROM mods WHERE modId = ?), 1.1))
		 ON CONFLICT (userId, modId) DO UPDATE SET downloadedAt = COALESCE(subscriptions.downloadedAt, excluded.downloadedAt)`,
		userID, modID, now, modID); err != nil {
		return nil, fmt.Errorf("upsert subscription: %w", err)
	}
	if count && firstCount {
		if _, err := tx.ExecContext(ctx,
			`UPDATE mods SET downloads = downloads + 1 WHERE modId = ?`, modID); err != nil {
			return nil, fmt.Errorf("increment downloads: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE subscriptions SET downloadCounted = 1 WHERE userId = ? AND modId = ?`,
			userID, modID); err != nil {
			return nil, fmt.Errorf("mark download counted: %w", err)
		}
	}

	target, err := downloadTargetTx(ctx, tx, modID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit subscribe: %w", err)
	}
	return target, nil
}

// Unsubscribe removes the subscription.
func (s *ModStore) Unsubscribe(ctx context.Context, userID int64, modID int64) error {
	if userID <= 0 {
		return ErrUnauthorized
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mods WHERE modId = ?`, modID).Scan(&exists); err != nil {
		return fmt.Errorf("check mod: %w", err)
	}
	if exists == 0 {
		return ErrNotFound
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM subscriptions WHERE userId = ? AND modId = ?`, userID, modID)
	if err != nil {
		return fmt.Errorf("delete subscription: %w", err)
	}
	return nil
}

// Install marks the mod installed and counts the install once per user.
func (s *ModStore) Install(ctx context.Context, userID int64, modID int64) (*DownloadTarget, error) {
	if userID <= 0 {
		return nil, ErrUnauthorized
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin install: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM mods WHERE modId = ?`, modID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check mod: %w", err)
	}
	if exists == 0 {
		return nil, ErrNotFound
	}

	var installedAt sql.NullString
	err = tx.QueryRowContext(ctx,
		`SELECT installedAt FROM subscriptions WHERE userId = ? AND modId = ?`, userID, modID).
		Scan(&installedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("read subscription: %w", err)
	}
	firstInstall := errors.Is(err, sql.ErrNoRows) || !installedAt.Valid

	now := time.Now().UTC().Format(time.RFC3339)
	// Installing stamps the version that was installed: re-installing a mod
	// after one of its updates stops flagging the update, while a
	// subscription left on an older version keeps it.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO subscriptions (userId, modId, installedAt, installedVersion)
		 VALUES (?, ?, ?, COALESCE((SELECT COALESCE(NULLIF(modVersion, 0), 1.1) FROM mods WHERE modId = ?), 1.1))
		 ON CONFLICT (userId, modId) DO UPDATE SET
		   installedAt = COALESCE(subscriptions.installedAt, excluded.installedAt),
		   installedVersion = excluded.installedVersion`,
		userID, modID, now, modID); err != nil {
		return nil, fmt.Errorf("upsert install: %w", err)
	}
	if firstInstall {
		if _, err := tx.ExecContext(ctx,
			`UPDATE mods SET installs = installs + 1 WHERE modId = ?`, modID); err != nil {
			return nil, fmt.Errorf("increment installs: %w", err)
		}
	}

	target, err := downloadTargetTx(ctx, tx, modID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit install: %w", err)
	}
	return target, nil
}

// Uninstall clears the install mark and decrements the counter, with a floor of
// zero so a repeated uninstall cannot make it negative.
func (s *ModStore) Uninstall(ctx context.Context, userID int64, modID int64) error {
	if userID <= 0 {
		return ErrUnauthorized
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin uninstall: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM mods WHERE modId = ?`, modID).Scan(&exists); err != nil {
		return fmt.Errorf("check mod: %w", err)
	}
	if exists == 0 {
		return ErrNotFound
	}

	var installedAt sql.NullString
	err = tx.QueryRowContext(ctx,
		`SELECT installedAt FROM subscriptions WHERE userId = ? AND modId = ?`, userID, modID).
		Scan(&installedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read subscription: %w", err)
	}
	wasInstalled := err == nil && installedAt.Valid

	if _, err := tx.ExecContext(ctx,
		`UPDATE subscriptions SET installedAt = NULL WHERE userId = ? AND modId = ?`,
		userID, modID); err != nil {
		return fmt.Errorf("clear install: %w", err)
	}
	if wasInstalled {
		if _, err := tx.ExecContext(ctx,
			`UPDATE mods SET installs = MAX(installs - 1, 0) WHERE modId = ?`, modID); err != nil {
			return fmt.Errorf("decrement installs: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit uninstall: %w", err)
	}
	return nil
}

// Flag records a report. A repeated report from the same user replaces the
// previous one instead of stacking.
func (s *ModStore) Flag(ctx context.Context, userID int64, modID int64, reason string, boolValue *bool) error {
	if userID <= 0 {
		return ErrUnauthorized
	}
	if strings.TrimSpace(reason) == "" {
		return ErrValidation
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mods WHERE modId = ?`, modID).Scan(&exists); err != nil {
		return fmt.Errorf("check mod: %w", err)
	}
	if exists == 0 {
		return ErrNotFound
	}
	var flag int64
	if boolValue != nil {
		flag = BoolToNumber(*boolValue)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO flags (modId, userId, reason, boolValue) VALUES (?, ?, ?, ?)`,
		modID, userID, reason, flag)
	if err != nil {
		return fmt.Errorf("insert flag: %w", err)
	}
	return nil
}

// ReportInput is the resolved body of Report.
type ReportInput struct {
	ModID               int64
	ConcernCategoryID   string
	ConcernCategoryName string
	ConcernContent      string
}

// Report records a concern about a mod. The category is free text: the capture
// sends both an id and a name, and neither is an enum the server can check.
func (s *ModStore) Report(ctx context.Context, userID int64, input ReportInput) error {
	if userID <= 0 {
		return ErrUnauthorized
	}
	if input.ModID <= 0 || input.ConcernCategoryID == "" || input.ConcernCategoryName == "" {
		return ErrValidation
	}
	var exists int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM mods WHERE modId = ?`, input.ModID).Scan(&exists); err != nil {
		return fmt.Errorf("check mod: %w", err)
	}
	if exists == 0 {
		return ErrNotFound
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO mod_reports (modId, userId, concernCategoryId, concernCategoryName, concernContent)
		 VALUES (?, ?, ?, ?, ?)`,
		input.ModID, userID, input.ConcernCategoryID, input.ConcernCategoryName, input.ConcernContent)
	if err != nil {
		return fmt.Errorf("insert mod report: %w", err)
	}
	return nil
}

// Delete removes a mod and everything that hangs off it. Every child table
// declares ON DELETE CASCADE, so one statement is enough. Only the creator may
// delete a mod.
func (s *ModStore) Delete(ctx context.Context, userID int64, modID int64) error {
	if userID <= 0 {
		return ErrUnauthorized
	}
	if modID <= 0 {
		return ErrValidation
	}
	var creatorID sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT creatorId FROM mods WHERE modId = ?`, modID).Scan(&creatorID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read mod creator: %w", err)
	}
	if !creatorID.Valid || creatorID.Int64 != userID {
		return ErrUnauthorized
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM mods WHERE modId = ?`, modID); err != nil {
		return fmt.Errorf("delete mod: %w", err)
	}
	return nil
}

// Reviews returns one page of reviews and the total count.
func (s *ModStore) Reviews(ctx context.Context, modID int64, page int, count int, ratingNum int64) ([]Review, int64, error) {
	if page < 1 {
		page = 1
	}
	if count < 1 {
		count = 5
	}
	if count > 100 {
		count = 100
	}

	where := ` WHERE modId = ?`
	args := []any{modID}
	if ratingNum >= 1 && ratingNum <= 5 {
		where += ` AND rating = ?`
		args = append(args, ratingNum)
	}

	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM reviews`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count reviews: %w", err)
	}

	query := `SELECT COALESCE(helpfulNegative, 0), COALESCE(helpfulnessVotes, 0),
			COALESCE(helpfulPositive, 0), isInstalled, reviewerId, itemVersion, locale,
			rating, reviewId, reviewText, submitted, title, COALESCE(reviewerName, ''),
			COALESCE(avatarUrl, '')
		FROM reviews` + where + ` ORDER BY submitted DESC LIMIT ? OFFSET ?`
	args = append(args, count, (page-1)*count)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query reviews: %w", err)
	}
	defer func() { _ = rows.Close() }()

	reviews := make([]Review, 0, count)
	for rows.Next() {
		var review Review
		var installed int64
		var reviewerID, locale, reviewID sql.NullString
		err := rows.Scan(&review.HelpfulNegative, &review.HelpfulnessVotes, &review.HelpfulPositive,
			&installed, &reviewerID, &review.ItemVersion, &locale, &review.Rating, &reviewID,
			&review.ReviewText, &review.Submitted, &review.Title, &review.ReviewerName, &review.AvatarURL)
		if err != nil {
			return nil, 0, fmt.Errorf("scan review: %w", err)
		}
		review.IsInstalled = NumberToBool(installed)
		review.ReviewerID = nullString(reviewerID)
		review.Locale = nullString(locale)
		review.ReviewID = nullString(reviewID)
		reviews = append(reviews, review)
	}
	return reviews, total, rows.Err()
}

func downloadTargetTx(ctx context.Context, tx *sql.Tx, modID int64) (*DownloadTarget, error) {
	var target DownloadTarget
	err := tx.QueryRowContext(ctx,
		`SELECT modId, gameTitleId, downloads, installs, COALESCE(fileCRC, ''), COALESCE(fileUrl, '')
		 FROM mods WHERE modId = ?`, modID).
		Scan(&target.ModID, &target.GameTitleID, &target.DownloadCount, &target.InstallCount,
			&target.FileCRC, &target.FileURL)
	if err != nil {
		return nil, fmt.Errorf("read download target: %w", err)
	}
	return &target, nil
}

// ParseModID parses a path or body mod id.
func ParseModID(value string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || id <= 0 {
		return 0, ErrValidation
	}
	return id, nil
}

// ModInput is the writable form of a mod. The spec has no creation endpoint, so
// this is the seam the importer and the tests use.
type ModInput struct {
	ModID            int64
	GameTitleID      int64
	ModName          string
	Description      string
	ModDescription   string
	ChangeList       string
	ModVersion       float64
	ModTypeID        int64
	ModStatusID      int64
	ModVisibility    string
	CreatorID        int64
	CreateDate       string
	LastUpdate       string
	Popular          int64
	ThirtyDayPopular int64
	Downloads        int64
	Installs         int64
	Likes            int64
	Thumbnail        string
	FileURL          string
	FileCRC          string
	ModFileSize      float64
	MetaData         string
	TagIDs           []int64
}

// PublishImage is one image of a Publish request.
type PublishImage struct {
	DisplayIndex int64
	ImageName    string
	Thumbnail    string
	FileSize     float64
}

// PublishInput is the resolved multipart body of Publish.
type PublishInput struct {
	// ModID is 0 to create a mod and the existing id to update it.
	ModID         int64
	GameTitleID   int64
	ModName       string
	Description   string
	ChangeList    string
	ModVisibility string
	ModTypeID     int64
	ModDiscussion bool
	TagIDs        []int64
	CreatorID     int64
	Images        []PublishImage
	FileURL       string
	FileCRC       string
	FileSize      float64
	// ModVersion is stored as sent: zero for a create without one. The v4
	// Detail renders the stored zero as 1.1, which is what the capture shows.
	ModVersion float64
	// DiskSize is the uncompressed size of the archive, which the capture
	// carries as modDiskSize of a created mod. Zero keeps it unset.
	DiskSize float64
	// Thumbnail overrides the image the mod starts with.
	Thumbnail string
}

// defaultTagOther is the tag the upstream puts on a freshly created mod that
// does not carry one: the created mod of the capture answers later with
// modTagIds [26] ("Other") although the request carried no tags.
const defaultTagOther = 26

// LocalModIDFloor opens the numeric range reserved for mods published by this
// server. The official server hands out ids from its own namespace - the
// upstream scrape peaks at 593,695 and grows by a few hundred thousand ids
// per decade - so nothing below LocalModIDFloor can ever move, and nothing
// from LocalModIDFloor up can collide with an official mod. The ceiling keeps
// every id readable where a 32-bit client (the legacy v1 wire) parses modId.
const (
	LocalModIDFloor = int64(1_000_000_000)
	LocalModIDLimit = int64(2_147_483_647)
)

// allocateLocalModID hands out the next id of the locally published band
// [LocalModIDFloor, LocalModIDLimit) inside tx. The allocator row is seeded
// lazily and only ever advances, so deleting the highest mod never recycles
// its id. It also jumps past any id that already landed in the band sideways
// (a hand-imported capture or a restored backup), so banded ids stay unique
// whatever wrote them.
func (s *ModStore) allocateLocalModID(ctx context.Context, tx *sql.Tx) (int64, error) {
	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO mod_id_allocator (id, nextModId) VALUES (1, ?)`,
		LocalModIDFloor); err != nil {
		return 0, fmt.Errorf("seed mod id allocator: %w", err)
	}
	var next int64
	if err := tx.QueryRowContext(ctx,
		`SELECT nextModId FROM mod_id_allocator WHERE id = 1`).Scan(&next); err != nil {
		return 0, fmt.Errorf("read mod id allocator: %w", err)
	}
	var highest sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		`SELECT MAX(modId) FROM mods WHERE modId >= ?`, LocalModIDFloor).Scan(&highest); err != nil {
		return 0, fmt.Errorf("read banded mods: %w", err)
	}
	if highest.Valid && highest.Int64 >= next {
		next = highest.Int64 + 1
	}
	if next >= LocalModIDLimit {
		return 0, fmt.Errorf("%w: the local mod id band [%d, %d) is spent",
			ErrValidation, LocalModIDFloor, LocalModIDLimit)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE mod_id_allocator SET nextModId = ? WHERE id = 1`, next+1); err != nil {
		return 0, fmt.Errorf("advance mod id allocator: %w", err)
	}
	return next, nil
}

// Publish creates or updates a mod and returns its id. The live capture of a
// created mod shows that the status is always Published (2) whatever the
// visibility - which is a field of its own -, that a new mod starts at
// modVersion that the v4 render shows as 1.1, an empty verifiedVersion, that the description is
// mirrored into modDescription, that the changeList gains the
// `mod-update-date` header of the day, that the creator counts as its first
// download and install (popular and both counters start at 1) and that the
// tag "Other" is attached when the request carries no tags.
func (s *ModStore) Publish(ctx context.Context, input PublishInput) (int64, error) {
	if input.CreatorID <= 0 {
		return 0, ErrUnauthorized
	}
	if input.GameTitleID <= 0 || strings.TrimSpace(input.ModName) == "" {
		return 0, ErrValidation
	}
	visibility := input.ModVisibility
	if visibility == "" {
		visibility = "Public"
	}
	status, ok := statusForVisibility(visibility)
	if !ok {
		return 0, ErrValidation
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin publish: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// The capture stamps the dates as 2026-11-18T22:07:21.043: milliseconds and
	// no zone marker, unlike the RFC 3339 form stored before the capture.
	now := time.Now().UTC()
	stamp := now.Format("2006-01-02T15:04:05.000")
	changeList := WrapChangeList(input.ChangeList, now)
	modID := input.ModID
	if modID > 0 {
		var creatorID sql.NullInt64
		var storedVersion float64
		err := tx.QueryRowContext(ctx,
			`SELECT creatorId, COALESCE(modVersion, 0) FROM mods WHERE modId = ?`, modID).
			Scan(&creatorID, &storedVersion)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		if err != nil {
			return 0, fmt.Errorf("read mod creator: %w", err)
		}
		if !creatorID.Valid || creatorID.Int64 != input.CreatorID {
			return 0, ErrUnauthorized
		}
		// The AOE4 capture bumps the version on every update publish: the mod
		// it created shows 1.1 right after the create (the stored zero of the
		// legacy Detail is 1.1 on the v4 render) and 1.2 after the second
		// publish, although that request carried no version field. A request
		// that carries a version still stores it as sent.
		version := input.ModVersion
		if version == 0 {
			version = storedVersion
			if version == 0 {
				version = 1.1
			}
			// One decimal at a time, without the binary drift of a raw
			// 1.1+0.1 (1.2000000000000002, which JSON spells out).
			version = math.Round(version*10) / 10
			version += 0.1
			version = math.Round(version*10) / 10
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE mods SET gameTitleId = ?, modName = ?, description = ?, modDescription = ?, changeList = ?,
			                modVersion = ?, modVisibility = ?, modStatusId = ?, modTypeId = ?,
			                allowDiscussion = ?, lastUpdate = ?
			WHERE modId = ?`,
			input.GameTitleID, input.ModName, input.Description, nullEmpty(input.Description), changeList,
			version, visibility, status, nullableID(input.ModTypeID), BoolToNumber(input.ModDiscussion),
			stamp, modID); err != nil {
			return 0, fmt.Errorf("update mod: %w", err)
		}
	} else {
		// modVersion stays as sent - zero when the request carries none. The
		// capture shows the raw zero on the legacy Detail and 1.1 on the v4
		// Detail, which the v4 render fills in.
		thumbnail := input.Thumbnail
		if thumbnail == "" && len(input.Images) > 0 {
			thumbnail = input.Images[0].Thumbnail
		}
		diskSize := input.DiskSize
		modID, err = s.allocateLocalModID(ctx, tx)
		if err != nil {
			return 0, fmt.Errorf("allocate mod id: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO mods (modId, gameTitleId, modName, description, modDescription, changeList, modVersion,
			                  verifiedVersion, modTypeId, allowDiscussion, modStatusId, modVisibility,
			                  creatorId, createDate, lastUpdate, popular, downloads, installs,
			                  thumbnail, fileUrl, fileCRC, modFileSize, modDiskSize)
			VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, ?, ?, 1, 1, 1, ?, ?, ?, ?, ?)`,
			modID, input.GameTitleID, input.ModName, input.Description,
			nullEmpty(input.Description), changeList, input.ModVersion, nullableID(input.ModTypeID),
			BoolToNumber(input.ModDiscussion), status, visibility, input.CreatorID, stamp, stamp,
			nullEmpty(thumbnail), nullEmpty(input.FileURL), input.FileCRC, input.FileSize,
			nullFloat64(diskSize)); err != nil {
			return 0, fmt.Errorf("insert mod: %w", err)
		}
		// The creator counts as its first download and install, and carries the
		// viewer state userDownloaded/userInstalled that the capture shows on a
		// freshly created mod. The installed version is stamped so a later
		// publish flips updateAvailable: the AOE4 capture renders it true for
		// the creator after it publishes 1.2 over its installed 1.1.
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO subscriptions (userId, modId, downloadedAt, installedAt, downloadCounted, installedVersion)
			 VALUES (?, ?, ?, ?, 1, COALESCE((SELECT COALESCE(NULLIF(modVersion, 0), 1.1) FROM mods WHERE modId = ?), 1.1))`,
			input.CreatorID, modID, now.Format(time.RFC3339), now.Format(time.RFC3339), modID); err != nil {
			return 0, fmt.Errorf("subscribe creator: %w", err)
		}
	}

	if input.FileURL != "" {
		if _, err := tx.ExecContext(ctx,
			`UPDATE mods SET fileUrl = ?, fileCRC = ?, modFileSize = ? WHERE modId = ?`,
			input.FileURL, input.FileCRC, input.FileSize, modID); err != nil {
			return 0, fmt.Errorf("attach mod file: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM mod_tag_map WHERE modId = ?`, modID); err != nil {
		return 0, fmt.Errorf("clear mod tags: %w", err)
	}
	// A mod created without tags gets "Other": the captures of both games
	// answer with modTagIds [26] later on, also game 4 whose Tags catalog has
	// no 26 (the id renders without a name then).
	tags := uniqueIDs(input.TagIDs)
	if len(tags) == 0 {
		tags = []int64{defaultTagOther}
	}
	for _, tagID := range tags {
		if tagID != defaultTagOther {
			var exists int
			if err := tx.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM mod_types WHERE gameTitleId = ? AND modTypeId = ?`,
				input.GameTitleID, tagID).Scan(&exists); err != nil {
				return 0, fmt.Errorf("check mod tag: %w", err)
			}
			if exists == 0 {
				return 0, ErrValidation
			}
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO mod_tag_map (modId, gameTitleId, modTypeId) VALUES (?, ?, ?)`,
			modID, input.GameTitleID, tagID); err != nil {
			return 0, fmt.Errorf("insert mod tag: %w", err)
		}
	}

	if len(input.Images) > 0 {
		// An update that re-published an existing image identity (the AOE2
		// client rides Create with the imageId and the absolute URL it got,
		// and uploads nothing new) keeps the stored rows: replacing them
		// would renumber the identities the subscriber's install saw. A row
		// with a fresh upload carries the stored CRC name and renews the set.
		fresh := false
		for _, image := range input.Images {
			if image.ImageName != "" && !IsAbsoluteURL(image.ImageName) {
				fresh = true
				break
			}
		}
		if fresh {
			if _, err := tx.ExecContext(ctx, `DELETE FROM mod_images WHERE modId = ?`, modID); err != nil {
				return 0, fmt.Errorf("clear mod images: %w", err)
			}
			for _, image := range input.Images {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO mod_images (imageId, modId, displayIndex, imageName, imageThumbnail, imageFileSize)
					VALUES (?, ?, ?, ?, ?, ?)`,
					fmt.Sprintf("%d-%d", modID, image.DisplayIndex), modID, image.DisplayIndex,
					image.ImageName, image.Thumbnail, image.FileSize); err != nil {
					return 0, fmt.Errorf("insert mod image: %w", err)
				}
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit publish: %w", err)
	}
	return modID, nil
}

// statusForVisibility validates the visibility of a Publish request. The
// capture of a created mod with Unlisted visibility answers modStatusId 2
// ("Published") and the November Detail captures show the same id for public
// mods, so every visibility maps to the published status.
func statusForVisibility(visibility string) (int64, bool) {
	switch visibility {
	case "Public", "Unlisted", "Private":
		return 2, true
	default:
		return 0, false
	}
}

// WrapChangeList prefixes the client changeList with the update-date header
// the captures carry: a plain `<p>changelog</p>` comes back as
// `<h4 class="mod-update-date">11/18/2026</h4><p>changelog</p>`. An empty
// changeList stays empty.
func WrapChangeList(changeList string, when time.Time) string {
	if strings.TrimSpace(changeList) == "" {
		return changeList
	}
	return `<h4 class="mod-update-date">` + when.Format("01/02/2006") + `</h4>` + changeList
}

// nullEmpty stores an unset string as NULL, which is how the capture renders
// the fields the request did not fill.
func nullEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// nullFloat64 stores a zero as NULL so an optional number stays unset.
func nullFloat64(value float64) any {
	if value == 0 {
		return nil
	}
	return value
}

// nullableID turns a zero id into NULL, so an optional reference stays unset.
func nullableID(id int64) any {
	if id <= 0 {
		return nil
	}
	return id
}

// uniqueIDs drops the non-positive and repeated ids of a request.
func uniqueIDs(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// Upsert inserts or replaces a mod and its tags.
func (s *ModStore) Upsert(ctx context.Context, input ModInput) error {
	// Imports mirror the official namespace, which lives below
	// LocalModIDFloor; ids beyond LocalModIDLimit are on purpose never
	// accepted because 32-bit clients (the legacy v1 wire) cannot read them.
	// Ids inside the band are allowed - re-importing a locally published mod
	// round-trips through here - and allocateLocalModID skips past them.
	if input.ModID <= 0 || input.ModID > LocalModIDLimit ||
		input.GameTitleID <= 0 || input.ModName == "" {
		return ErrValidation
	}
	if input.ModStatusID < 1 || input.ModStatusID > 5 {
		// 5 is the live "Featured" of the AOM captures; the unknown ids
		// default to 1, which every render reads as the plain Published.
		input.ModStatusID = 1
	}
	if input.ModVisibility == "" {
		input.ModVisibility = "Public"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if input.CreateDate == "" {
		input.CreateDate = now
	}
	if input.LastUpdate == "" {
		input.LastUpdate = now
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin upsert mod: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx,
		`INSERT INTO mods (modId, gameTitleId, modName, description, modDescription, changeList,
			modVersion, modTypeId, modStatusId, modVisibility, creatorId, createDate, lastUpdate,
			popular, thirtyDayPopular, downloads, installs, likes, sortName, thumbnail, fileUrl,
			fileCRC, modFileSize, metaData)
		 VALUES (?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), ?, NULLIF(?, 0), ?, ?, NULLIF(?, 0),
			?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), ?, NULLIF(?, ''))
		 ON CONFLICT (modId) DO UPDATE SET
			gameTitleId = excluded.gameTitleId, modName = excluded.modName,
			description = excluded.description, modDescription = excluded.modDescription,
			changeList = excluded.changeList, modVersion = excluded.modVersion,
			modTypeId = excluded.modTypeId, modStatusId = excluded.modStatusId,
			modVisibility = excluded.modVisibility, creatorId = excluded.creatorId,
			createDate = excluded.createDate, lastUpdate = excluded.lastUpdate,
			popular = excluded.popular, thirtyDayPopular = excluded.thirtyDayPopular,
			downloads = excluded.downloads, installs = excluded.installs, likes = excluded.likes,
			sortName = excluded.sortName, thumbnail = excluded.thumbnail, fileUrl = excluded.fileUrl,
			fileCRC = excluded.fileCRC, modFileSize = excluded.modFileSize, metaData = excluded.metaData`,
		input.ModID, input.GameTitleID, input.ModName, input.Description, input.ModDescription,
		input.ChangeList, input.ModVersion, input.ModTypeID, input.ModStatusID, input.ModVisibility,
		input.CreatorID, input.CreateDate, input.LastUpdate, input.Popular, input.ThirtyDayPopular,
		input.Downloads, input.Installs, input.Likes, strings.ToLower(input.ModName),
		input.Thumbnail, input.FileURL, input.FileCRC, input.ModFileSize, input.MetaData)
	if err != nil {
		return fmt.Errorf("upsert mod: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM mod_tag_map WHERE modId = ?`, input.ModID); err != nil {
		return fmt.Errorf("clear mod tags: %w", err)
	}
	for _, tagID := range input.TagIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO mod_tag_map (modId, gameTitleId, modTypeId) VALUES (?, ?, ?)`,
			input.ModID, input.GameTitleID, tagID); err != nil {
			return fmt.Errorf("insert mod tag: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit upsert mod: %w", err)
	}
	return nil
}

// AddImage attaches an image to a mod.
func (s *ModStore) AddImage(ctx context.Context, modID int64, image Image) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO mod_images (imageId, modId, displayIndex, imageName, imageThumbnail, imageFileSize)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		image.ImageID, modID, image.DisplayIndex, image.ImageName, image.ImageThumbnail, image.ImageFileSize)
	if err != nil {
		return fmt.Errorf("insert mod image: %w", err)
	}
	return nil
}
