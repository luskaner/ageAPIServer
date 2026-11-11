package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
	args := []any{}
	if gameTitleID > 0 {
		query += ` WHERE gameTitleId = ?`
		args = append(args, gameTitleID)
	}
	query += ` ORDER BY gameTitleId, modTypeId`

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
LEFT JOIN mod_types t ON t.modTypeId = m.modTypeId
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
		clauses = append(clauses, "m.modStatusId = ?")
		args = append(args, 2)
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
			WHERE tm.modId = m.modId AND tm.modTypeId IN (`+placeholders(len(included))+`))`)
		args = append(args, argsFor(included)...)
	}
	if len(excluded) > 0 {
		clauses = append(clauses, `NOT EXISTS (SELECT 1 FROM mod_tag_map tm
			WHERE tm.modId = m.modId AND tm.modTypeId IN (`+placeholders(len(excluded))+`))`)
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

// modStatusID maps the enum name to its id.
func modStatusID(name string) (int64, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "published":
		return 1, true
	case "featured":
		return 2, true
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

	images, err := s.imagesFor(ctx, ids)
	if err != nil {
		return nil, err
	}
	tags, err := s.tagsFor(ctx, ids)
	if err != nil {
		return nil, err
	}
	ratings, err := s.ratingsFor(ctx, ids)
	if err != nil {
		return nil, err
	}
	state, err := s.viewerStateFor(ctx, ids, viewer)
	if err != nil {
		return nil, err
	}

	mods := make([]Mod, 0, len(scanned))
	for _, row := range scanned {
		mods = append(mods, row.toMod(state, ratings[row.modID], images[row.modID], tags[row.modID]))
	}
	return mods, nil
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
		image.ImageFileSize = nullFloat(size)
		images[modID] = append(images[modID], image)
	}
	return images, rows.Err()
}

func (s *ModStore) tagsFor(ctx context.Context, ids []int64) (map[int64][]ModTag, error) {
	query := `SELECT tm.modId, t.modTypeId, t.modTypeName
		FROM mod_tag_map tm JOIN mod_types t ON t.modTypeId = tm.modTypeId
		WHERE tm.modId IN (` + placeholders(len(ids)) + `) ORDER BY tm.modId, t.modTypeId`
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
	query := `SELECT modId, COUNT(*), AVG(rating),
			SUM(CASE WHEN rating = 1 THEN 1 ELSE 0 END),
			SUM(CASE WHEN rating = 2 THEN 1 ELSE 0 END),
			SUM(CASE WHEN rating = 3 THEN 1 ELSE 0 END),
			SUM(CASE WHEN rating = 4 THEN 1 ELSE 0 END),
			SUM(CASE WHEN rating = 5 THEN 1 ELSE 0 END)
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
		var one, two, three, four, five int64
		if err := rows.Scan(&modID, &total, &average, &one, &two, &three, &four, &five); err != nil {
			return nil, fmt.Errorf("scan ratings: %w", err)
		}
		ratings[modID] = &Ratings{
			Rating: RatingBreakdown{
				Average:    average,
				Count1Star: &one,
				Count2Star: &two,
				Count3Star: &three,
				Count4Star: &four,
				Count5Star: &five,
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
		ModType:           nullString(r.modTypeName),
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
		UpdateAvailable:   NumberToBool(r.updateAvailable.Int64),
		ModFileSize:       r.modFileSize.Float64,
		ModDiskSize:       nullFloat(r.modDiskSize),
		Popular:           nullInt(r.popular),
		Downloads:         nullInt(r.downloads),
		Installs:          nullInt(r.installs),
		Likes:             nullInt(r.likes),
		Ratings:           ratings,
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
		mod.UserLiked = boolPtr(state.liked[r.modID])
		mod.UserDownloaded = boolPtr(state.downloaded[r.modID])
		mod.UserInstalled = state.installed[r.modID]
		mod.UserFlagged = boolPtr(state.flagged[r.modID])
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
		UpdateAvailable:   nullBool(r.updateAvailable),
		ModFileSize:       nullFloat(r.modFileSize),
		ModDiskSize:       nullFloat(r.modDiskSize),
		Popular:           nullInt(r.popular),
		Downloads:         nullInt(r.downloads),
		Installs:          nullInt(r.installs),
		Likes:             nullInt(r.likes),
		Ratings:           ratings,
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
		detail.UserLiked = boolPtr(state.liked[r.modID])
		detail.UserDownloaded = boolPtr(state.downloaded[r.modID])
		detail.UserInstalled = boolPtr(state.installed[r.modID])
		detail.UserFlagged = boolPtr(state.flagged[r.modID])
		if rating, ok := state.rating[r.modID]; ok {
			detail.UserRating = &rating
		}
		if review, ok := state.review[r.modID]; ok {
			detail.UserReview = &review
		}
	}
	applyTags(&detail.ModTags, &detail.ModTagIDs, &detail.ModTagNames, tags)
	return detail
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
		*names = append(*names, tag.ModTypeName)
		*tagNames = append(*tagNames, tag.ModTypeName)
	}
}

func boolPtr(value bool) *bool {
	return &value
}

func rawJSON(value sql.NullString) json.RawMessage {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil
	}
	return json.RawMessage(value.String)
}

// Detail returns one mod.
func (s *ModStore) Detail(ctx context.Context, modID int64, viewer Viewer) (*Detail, error) {
	rows, err := s.db.QueryContext(ctx, baseSelect+` WHERE m.modId = ?`, modID)
	if err != nil {
		return nil, fmt.Errorf("query mod detail: %w", err)
	}
	scanned, _, err := scanModRows(rows)
	if err != nil {
		return nil, err
	}
	if len(scanned) == 0 {
		return nil, ErrNotFound
	}
	row := scanned[0]
	ids := []int64{row.modID}
	images, err := s.imagesFor(ctx, ids)
	if err != nil {
		return nil, err
	}
	tags, err := s.tagsFor(ctx, ids)
	if err != nil {
		return nil, err
	}
	ratings, err := s.ratingsFor(ctx, ids)
	if err != nil {
		return nil, err
	}
	state, err := s.viewerStateFor(ctx, ids, viewer)
	if err != nil {
		return nil, err
	}
	detail := row.toDetail(state, ratings[row.modID], images[row.modID], tags[row.modID])
	return &detail, nil
}

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
		              JOIN mod_tag_map theirs ON theirs.modTypeId = mine.modTypeId
		              WHERE mine.modId = ? AND theirs.modId = m.modId)
		ORDER BY m.popular DESC, m.modId DESC LIMIT 30`
	rows, err := s.db.QueryContext(ctx, query, gameTitleID, modID, modID)
	if err != nil {
		return nil, fmt.Errorf("query related mods: %w", err)
	}
	scanned, total, err := scanModRows(rows)
	if err != nil {
		return nil, err
	}
	if len(scanned) == 0 {
		fallback := baseSelect + ` WHERE m.gameTitleId = ? AND m.modId <> ?
			ORDER BY m.popular DESC, m.modId DESC LIMIT 30`
		rows, err = s.db.QueryContext(ctx, fallback, gameTitleID, modID)
		if err != nil {
			return nil, fmt.Errorf("query related fallback: %w", err)
		}
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
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO subscriptions (userId, modId, downloadedAt) VALUES (?, ?, ?)
		 ON CONFLICT (userId, modId) DO UPDATE SET downloadedAt = COALESCE(subscriptions.downloadedAt, excluded.downloadedAt)`,
		userID, modID, now); err != nil {
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
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO subscriptions (userId, modId, installedAt) VALUES (?, ?, ?)
		 ON CONFLICT (userId, modId) DO UPDATE SET installedAt = COALESCE(subscriptions.installedAt, excluded.installedAt)`,
		userID, modID, now); err != nil {
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

// Upsert inserts or replaces a mod and its tags.
func (s *ModStore) Upsert(ctx context.Context, input ModInput) error {
	if input.ModID <= 0 || input.GameTitleID <= 0 || input.ModName == "" {
		return ErrValidation
	}
	if input.ModStatusID < 1 || input.ModStatusID > 4 {
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
			`INSERT OR IGNORE INTO mod_tag_map (modId, modTypeId) VALUES (?, ?)`,
			input.ModID, tagID); err != nil {
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
