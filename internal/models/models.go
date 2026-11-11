// Package models owns the SQLite schema and every query. The route packages
// only translate HTTP to these calls, so the SQL lives in one place.
package models

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// Store errors. The route layer maps them to 404, 401 and 400.
var (
	ErrNotFound     = errors.New("not found")
	ErrUnauthorized = errors.New("unauthorized")
	ErrValidation   = errors.New("validation")
)

// Stores is the set of stores sharing one database handle. A single file is
// used because subscriptions, ratings and reviews reference mods
// transactionally.
type Stores struct {
	DB      *sql.DB
	Users   *UserStore
	Mods    *ModStore
	Ratings *RatingStore
	Polls   *PollStore
	System  *SystemStore
}

// Options configures Open.
type Options struct {
	// Path is the SQLite file. Empty means an in-memory database.
	Path          string
	BusyTimeoutMs int
	MaxOpenConns  int
}

// Open opens the database, applies the schema and seeds the catalogs.
func Open(opts Options) (*Stores, error) {
	dsn := dsnFor(opts)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if opts.MaxOpenConns < 1 {
		opts.MaxOpenConns = 1
	}
	db.SetMaxOpenConns(opts.MaxOpenConns)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	stores := &Stores{DB: db}
	stores.System = &SystemStore{db: db}
	stores.Users = &UserStore{db: db}
	stores.Mods = &ModStore{db: db}
	stores.Ratings = &RatingStore{db: db}
	stores.Polls = &PollStore{db: db}

	if err := stores.System.Migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := stores.System.Seed(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return stores, nil
}

func dsnFor(opts Options) string {
	busy := opts.BusyTimeoutMs
	if busy <= 0 {
		busy = 5000
	}
	params := fmt.Sprintf("_pragma=busy_timeout(%d)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)", busy)
	if opts.Path == "" {
		return "file::memory:?" + params
	}
	return "file:" + filepath.ToSlash(opts.Path) + "?" + params
}

// Close releases the database handle.
func (s *Stores) Close() error {
	if s == nil || s.DB == nil {
		return nil
	}
	return s.DB.Close()
}

// Viewer is the identity attached to a request. UserID is 0 when anonymous.
type Viewer struct {
	UserID   int64
	SteamID  string
	LoggedOn bool
}

// Game is one entry of the games catalog.
type Game struct {
	GameTitleID   int64   `json:"gameTitleId"`
	GameTitleName string  `json:"gameTitleName"`
	GameVersion   *string `json:"gameVersion"`
}

// ModTag is one mod type/tag.
type ModTag struct {
	ModTypeID   int64  `json:"modTypeId"`
	ModTypeName string `json:"modTypeName"`
	GameTitleID int64  `json:"gameTitleId"`
}

// Image is one entry of imageUrls.
type Image struct {
	ImageID        string   `json:"imageId"`
	DisplayIndex   int64    `json:"displayIndex"`
	ImageName      string   `json:"imageName"`
	ImageThumbnail string   `json:"imageThumbnail"`
	ImageFileSize  *float64 `json:"imageFileSize"`
}

// RatingBreakdown is the rating object of the Ratings schema.
type RatingBreakdown struct {
	Average    float64 `json:"average"`
	Count1Star *int64  `json:"count1Star"`
	Count2Star *int64  `json:"count2Star"`
	Count3Star *int64  `json:"count3Star"`
	Count4Star *int64  `json:"count4Star"`
	Count5Star *int64  `json:"count5Star"`
	TotalCount int64   `json:"totalCount"`
}

// Ratings is the aggregate block embedded in a mod and returned by Rate.
type Ratings struct {
	LeastFavorableReview json.RawMessage `json:"leastFavorableReview"`
	MostFavorableReview  json.RawMessage `json:"mostFavorableReview"`
	Rating               RatingBreakdown `json:"rating"`
	ReviewsCount         int64           `json:"reviewsCount"`
}

// Mod is the ModMetaData schema: one entry of modList.
type Mod struct {
	ModID             int64           `json:"modId"`
	GameTitleID       *int64          `json:"gameTitleId"`
	GameTitleName     *string         `json:"gameTitleName"`
	ModName           string          `json:"modName"`
	Description       *string         `json:"description"`
	ModDescription    *string         `json:"modDescription"`
	ChangeList        *string         `json:"changeList"`
	ModVersion        float64         `json:"modVersion"`
	VerifiedVersion   *string         `json:"verifiedVersion"`
	ModTypeID         *int64          `json:"modTypeId"`
	ClientID          *string         `json:"clientId"`
	ModType           *string         `json:"modType"`
	ModTags           []string        `json:"modTags"`
	ModTagIDs         []int64         `json:"modTagIds"`
	ModTagNames       []string        `json:"modTagNames"`
	MetaData          json.RawMessage `json:"metaData"`
	ModStatusID       *int64          `json:"modStatusId"`
	ModStatus         *string         `json:"modStatus"`
	ModStatusMessage  *string         `json:"modStatusMessage"`
	ModVisibility     *string         `json:"modVisibility"`
	CreatorID         *int64          `json:"creatorId"`
	CreatorName       *string         `json:"creatorName"`
	CreatorAvatarURL  *string         `json:"creatorAvatarUrl"`
	IsOwner           bool            `json:"isOwner"`
	IsLoggedOn        bool            `json:"isLoggedOn"`
	AllowDiscussion   bool            `json:"allowDiscussion"`
	DiscourseURL      *string         `json:"discourseUrl"`
	CreateDate        string          `json:"createDate"`
	LastUpdate        string          `json:"lastUpdate"`
	UpdateAvailable   bool            `json:"updateAvailable"`
	ModFileSize       float64         `json:"modFileSize"`
	ModDiskSize       *float64        `json:"modDiskSize"`
	Popular           *int64          `json:"popular"`
	Downloads         *int64          `json:"downloads"`
	Installs          *int64          `json:"installs"`
	Likes             *int64          `json:"likes"`
	UserLiked         *bool           `json:"userLiked"`
	Ratings           *Ratings        `json:"ratings"`
	CurrentRating     float64         `json:"currentRating"`
	UserDownloaded    *bool           `json:"userDownloaded"`
	UserInstalled     bool            `json:"userInstalled"`
	UserFlagged       *bool           `json:"userFlagged"`
	UserRating        *float64        `json:"userRating"`
	UserReview        *string         `json:"userReview"`
	PositiveRatings   int64           `json:"positiveRatings"`
	TotalRatings      int64           `json:"totalRatings"`
	Thumbnail         string          `json:"thumbnail"`
	FileURL           *string         `json:"fileUrl"`
	ImageURLs         []Image         `json:"imageUrls"`
	ThirtyDayPopular  *int64          `json:"thirtyDayPopular"`
	SortName          *string         `json:"sortName"`
	SendNotifications bool            `json:"sendNotifications"`
}

// Detail is the DetailResponse schema. It is a distinct type from Mod because
// the spec gives the two schemas different nullability for the same fields.
type Detail struct {
	ModID             int64           `json:"modId"`
	GameTitleID       int64           `json:"gameTitleId"`
	GameTitleName     string          `json:"gameTitleName"`
	ModName           string          `json:"modName"`
	Description       string          `json:"description"`
	ModDescription    string          `json:"modDescription"`
	ChangeList        string          `json:"changeList"`
	ModVersion        float64         `json:"modVersion"`
	VerifiedVersion   *string         `json:"verifiedVersion"`
	ModTypeID         *int64          `json:"modTypeId"`
	ClientID          *string         `json:"clientId"`
	ModType           *string         `json:"modType"`
	ModTags           []string        `json:"modTags"`
	ModTagIDs         []int64         `json:"modTagIds"`
	ModTagNames       []string        `json:"modTagNames"`
	MetaData          json.RawMessage `json:"metaData"`
	ModStatusID       int64           `json:"modStatusId"`
	ModStatus         string          `json:"modStatus"`
	ModStatusMessage  *string         `json:"modStatusMessage"`
	ModVisibility     *string         `json:"modVisibility"`
	CreatorID         *int64          `json:"creatorId"`
	CreatorName       *string         `json:"creatorName"`
	CreatorAvatarURL  *string         `json:"creatorAvatarUrl"`
	IsOwner           *bool           `json:"isOwner"`
	IsLoggedOn        bool            `json:"isLoggedOn"`
	AllowDiscussion   bool            `json:"allowDiscussion"`
	DiscourseURL      *string         `json:"discourseUrl"`
	CreateDate        *string         `json:"createDate"`
	LastUpdate        *string         `json:"lastUpdate"`
	UpdateAvailable   *bool           `json:"updateAvailable"`
	ModFileSize       *float64        `json:"modFileSize"`
	ModDiskSize       *float64        `json:"modDiskSize"`
	Popular           *int64          `json:"popular"`
	Downloads         *int64          `json:"downloads"`
	Installs          *int64          `json:"installs"`
	Likes             *int64          `json:"likes"`
	UserLiked         *bool           `json:"userLiked"`
	Ratings           *Ratings        `json:"ratings"`
	CurrentRating     float64         `json:"currentRating"`
	UserDownloaded    *bool           `json:"userDownloaded"`
	UserInstalled     *bool           `json:"userInstalled"`
	UserFlagged       *bool           `json:"userFlagged"`
	UserRating        *float64        `json:"userRating"`
	UserReview        *string         `json:"userReview"`
	PositiveRatings   int64           `json:"positiveRatings"`
	TotalRatings      int64           `json:"totalRatings"`
	Thumbnail         *string         `json:"thumbnail"`
	FileURL           *string         `json:"fileUrl"`
	ImageURLs         []Image         `json:"imageUrls"`
	ThirtyDayPopular  *int64          `json:"thirtyDayPopular"`
	SortName          *string         `json:"sortName"`
	SendNotifications bool            `json:"sendNotifications"`
}

// Review is one entry of the reviews response.
type Review struct {
	HelpfulNegative  int64   `json:"helpfulNegative"`
	HelpfulnessVotes int64   `json:"helpfulnessVotes"`
	HelpfulPositive  int64   `json:"helpfulPositive"`
	IsInstalled      bool    `json:"isInstalled"`
	ItemID           *string `json:"itemId"`
	ItemVersion      string  `json:"itemVersion"`
	Locale           *string `json:"locale"`
	Rating           int64   `json:"rating"`
	ReviewerID       *string `json:"reviewerId"`
	ReviewID         *string `json:"reviewId"`
	ReviewText       string  `json:"reviewText"`
	Submitted        string  `json:"submitted"`
	Title            string  `json:"title"`
	ReviewerName     string  `json:"reviewerName"`
	AvatarURL        string  `json:"avatarUrl"`
}

// DownloadInfo is the SubscribeResponse and DownloadResponse body.
type DownloadInfo struct {
	DownloadURL   string `json:"downloadUrl"`
	DownloadCount int64  `json:"downloadCount"`
	InstallCount  *int64 `json:"installCount"`
	GameTitleID   int64  `json:"gameTitleId"`
	FileCRC       string `json:"fileCRC"`
}

// PollSummary is one entry of /Poll/list.
type PollSummary struct {
	ID             string  `json:"id"`
	GameID         int64   `json:"gameId"`
	Created        string  `json:"created"`
	StartDateTime  string  `json:"startDateTime"`
	EndDateTime    string  `json:"endDateTime"`
	ExpireDateTime string  `json:"expireDateTime"`
	Metadata       string  `json:"metadata"`
	Icon           *string `json:"icon"`
}

// PollOption is one answer of a poll question.
type PollOption struct {
	ID        int64  `json:"id"`
	Value     string `json:"value"`
	VoteCount int64  `json:"voteCount"`
	UserVote  bool   `json:"userVote"`
}

// PollQuestion is one question of a poll.
type PollQuestion struct {
	ID         int64        `json:"id"`
	Order      int64        `json:"order"`
	Content    string       `json:"content"`
	Type       string       `json:"type"`
	MaxOptions int64        `json:"maxOptions"`
	UserVoted  bool         `json:"userVoted"`
	TotalVotes int64        `json:"totalVotes"`
	Options    []PollOption `json:"options"`
}

// Poll is the /Poll/{pollId} body. id and pollId carry the same value.
type Poll struct {
	ID             string         `json:"id"`
	PollID         string         `json:"pollId"`
	GameID         int64          `json:"gameId"`
	CreatedBy      string         `json:"createdBy"`
	Created        string         `json:"created"`
	StartDateTime  string         `json:"startDateTime"`
	EndDateTime    string         `json:"endDateTime"`
	ExpireDateTime string         `json:"expireDateTime"`
	Metadata       string         `json:"metadata"`
	IsDefinition   bool           `json:"isDefinition"`
	TotalVotes     int64          `json:"totalVotes"`
	Questions      []PollQuestion `json:"questions"`
}

// ModStatusName maps modStatusId to the enum of the spec. The mapping is a
// working assumption: the captures carry no response bodies to confirm it.
func ModStatusName(id int64) string {
	switch id {
	case 1:
		return "Published"
	case 2:
		return "Featured"
	case 3:
		return "Unlisted"
	case 4:
		return "Draft"
	default:
		return "Published"
	}
}

// nullString converts a nullable column to a pointer.
func nullString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	out := value.String
	return &out
}

// nullInt converts a nullable column to a pointer.
func nullInt(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	out := value.Int64
	return &out
}

// nullFloat converts a nullable column to a pointer.
func nullFloat(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	out := value.Float64
	return &out
}

// nullBool converts a nullable 0/1 column to a pointer.
func nullBool(value sql.NullInt64) *bool {
	if !value.Valid {
		return nil
	}
	out := value.Int64 != 0
	return &out
}

// placeholders renders "?, ?, ?" for a slice length.
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// argsFor converts a slice of int64 to query arguments.
func argsFor(values []int64) []any {
	args := make([]any, len(values))
	for i, value := range values {
		args[i] = value
	}
	return args
}
