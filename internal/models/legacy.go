// SPDX-License-Identifier: AGPL-3.0-only

package models

import "strconv"

// The legacy v1 surface renders the same rows with a different contract than
// the v4 one. The captures of /api/v1/Mods/{FindAsUser,Installed,My,Detail}
// carry, for the very same mods, fewer and differently nulled fields than the
// v4 captures: the rating aggregates are always zero and `ratings` is null,
// `popular` is never sent in a detail, the tags are string ids in `modTags`
// with `modTagIds` always null, and the list rows drop changeList, the file
// and the image list. The legacy Detail additionally wraps a sparse object
// around a full snapshot nested in `metaData`.

// LegacyMod is one legacy list row.
type LegacyMod struct {
	ModID             int64    `json:"modId"`
	GameTitleID       *int64   `json:"gameTitleId"`   // always null
	GameTitleName     *string  `json:"gameTitleName"` // always null
	ModName           string   `json:"modName"`
	Description       *string  `json:"description"`
	ModDescription    *string  `json:"modDescription"`
	ChangeList        *string  `json:"changeList"` // always null
	ModVersion        float64  `json:"modVersion"` // as stored, no default
	VerifiedVersion   *string  `json:"verifiedVersion"`
	ModTypeID         *int64   `json:"modTypeId"` // always null
	ClientID          *string  `json:"clientId"`  // always null
	ModType           *string  `json:"modType"`
	ModTags           []string `json:"modTags"`   // the ids as strings
	ModTagIDs         []int64  `json:"modTagIds"` // always null
	ModTagNames       []string `json:"modTagNames"`
	MetaData          any      `json:"metaData"`    // always null in a list
	ModStatusID       *int64   `json:"modStatusId"` // always null
	ModStatus         *string  `json:"modStatus"`
	ModStatusMessage  *string  `json:"modStatusMessage"` // always null
	ModVisibility     *string  `json:"modVisibility"`
	CreatorID         *int64   `json:"creatorId"` // always null
	CreatorName       *string  `json:"creatorName"`
	CreatorAvatarURL  *string  `json:"creatorAvatarUrl"`
	IsOwner           bool     `json:"isOwner"`
	IsLoggedOn        bool     `json:"isLoggedOn"`
	AllowDiscussion   bool     `json:"allowDiscussion"`
	DiscourseURL      *string  `json:"discourseUrl"`
	CreateDate        string   `json:"createDate"`
	LastUpdate        string   `json:"lastUpdate"`
	UpdateAvailable   bool     `json:"updateAvailable"`
	ModFileSize       *float64 `json:"modFileSize"`
	ModDiskSize       *float64 `json:"modDiskSize"` // always null in a list
	Popular           *int64   `json:"popular"`     // always null in a list
	Downloads         *int64   `json:"downloads"`
	Installs          *int64   `json:"installs"`
	Likes             *int64   `json:"likes"`
	UserLiked         *bool    `json:"userLiked"`
	Ratings           *Ratings `json:"ratings"`       // always null: the legacy client does not carry ratings
	CurrentRating     float64  `json:"currentRating"` // always zero
	UserDownloaded    *bool    `json:"userDownloaded"`
	UserInstalled     bool     `json:"userInstalled"`
	UserFlagged       *bool    `json:"userFlagged"`
	UserRating        *float64 `json:"userRating"`
	UserReview        *string  `json:"userReview"`
	PositiveRatings   int64    `json:"positiveRatings"` // always zero
	TotalRatings      int64    `json:"totalRatings"`    // always zero
	Thumbnail         string   `json:"thumbnail"`
	FileURL           *string  `json:"fileUrl"`          // always null in a list
	ImageURLs         []Image  `json:"imageUrls"`        // always null in a list
	ThirtyDayPopular  *int64   `json:"thirtyDayPopular"` // always null in a list
	SortName          *string  `json:"sortName"`         // always null in a list
	SendNotifications bool     `json:"sendNotifications"`
}

// nilFloatPtr keeps a zero float as null, which is how the capture renders a
// value the legacy DTO does not fill.
func nilFloatPtr(value float64) *float64 {
	if value == 0 {
		return nil
	}
	return &value
}

// LegacyModOf projects one v4 Mod into the legacy list row. The tag names go
// to modTagNames, the tag ids move to modTags as strings - with modTagIds
// null - and the ratings block is dropped, which is what the captures carry.
func LegacyModOf(mod Mod) LegacyMod {
	legacy := LegacyMod{
		ModID:             mod.ModID,
		ModName:           mod.ModName,
		Description:       mod.Description,
		ModDescription:    mod.ModDescription,
		ModVersion:        mod.ModVersion,
		VerifiedVersion:   mod.VerifiedVersion,
		ModType:           mod.ModType,
		ModTagNames:       mod.ModTagNames,
		ModStatus:         mod.ModStatus,
		ModVisibility:     mod.ModVisibility,
		CreatorName:       mod.CreatorName,
		CreatorAvatarURL:  mod.CreatorAvatarURL,
		IsOwner:           mod.IsOwner,
		IsLoggedOn:        mod.IsLoggedOn,
		AllowDiscussion:   mod.AllowDiscussion,
		DiscourseURL:      mod.DiscourseURL,
		CreateDate:        mod.CreateDate,
		LastUpdate:        mod.LastUpdate,
		UpdateAvailable:   mod.UpdateAvailable,
		ModFileSize:       nilFloatPtr(mod.ModFileSize),
		Downloads:         mod.Downloads,
		Installs:          mod.Installs,
		Likes:             mod.Likes,
		UserLiked:         mod.UserLiked,
		UserDownloaded:    mod.UserDownloaded,
		UserInstalled:     mod.UserInstalled,
		UserFlagged:       mod.UserFlagged,
		Thumbnail:         mod.Thumbnail,
		SendNotifications: mod.SendNotifications,
	}
	if len(mod.ModTagIDs) > 0 {
		legacy.ModTags = make([]string, 0, len(mod.ModTagIDs))
		for _, id := range mod.ModTagIDs {
			legacy.ModTags = append(legacy.ModTags, strconv.FormatInt(id, 10))
		}
	}
	return legacy
}

// LegacyMeta is the snapshot the legacy Detail nests in metaData. It keeps the
// viewer state, sizes and counters of the mod as stored, but never carries the
// changeList, the image list, the file, the discussion block, the rating
// aggregates or the popularity fields.
type LegacyMeta struct {
	ModID             int64    `json:"modId"`
	GameTitleID       *int64   `json:"gameTitleId"`   // always null
	GameTitleName     *string  `json:"gameTitleName"` // always null
	ModName           string   `json:"modName"`
	Description       *string  `json:"description"`
	ModDescription    *string  `json:"modDescription"`
	ChangeList        *string  `json:"changeList"` // always null
	ModVersion        float64  `json:"modVersion"` // as stored, no default
	VerifiedVersion   *string  `json:"verifiedVersion"`
	ModTypeID         *int64   `json:"modTypeId"` // always null
	ClientID          *string  `json:"clientId"`  // always null
	ModType           *string  `json:"modType"`
	ModTags           []string `json:"modTags"`   // the ids as strings
	ModTagIDs         []int64  `json:"modTagIds"` // always null
	ModTagNames       []string `json:"modTagNames"`
	MetaData          any      `json:"metaData"`    // always null
	ModStatusID       *int64   `json:"modStatusId"` // always null
	ModStatus         *string  `json:"modStatus"`
	ModStatusMessage  *string  `json:"modStatusMessage"` // always null
	ModVisibility     *string  `json:"modVisibility"`
	CreatorID         *int64   `json:"creatorId"` // always null
	CreatorName       *string  `json:"creatorName"`
	CreatorAvatarURL  *string  `json:"creatorAvatarUrl"`
	IsOwner           *bool    `json:"isOwner"`
	IsLoggedOn        bool     `json:"isLoggedOn"`
	AllowDiscussion   bool     `json:"allowDiscussion"` // always false
	DiscourseURL      *string  `json:"discourseUrl"`    // always null
	CreateDate        *string  `json:"createDate"`
	LastUpdate        *string  `json:"lastUpdate"`
	UpdateAvailable   *bool    `json:"updateAvailable"`
	ModFileSize       *float64 `json:"modFileSize"`
	ModDiskSize       *float64 `json:"modDiskSize"`
	Popular           *int64   `json:"popular"` // always null
	Downloads         *int64   `json:"downloads"`
	Installs          *int64   `json:"installs"`
	Likes             *int64   `json:"likes"`
	UserLiked         *bool    `json:"userLiked"`
	Ratings           *Ratings `json:"ratings"`       // always null
	CurrentRating     float64  `json:"currentRating"` // always zero
	UserDownloaded    *bool    `json:"userDownloaded"`
	UserInstalled     *bool    `json:"userInstalled"`
	UserFlagged       *bool    `json:"userFlagged"`
	UserRating        *float64 `json:"userRating"`
	UserReview        *string  `json:"userReview"`
	PositiveRatings   int64    `json:"positiveRatings"` // always zero
	TotalRatings      int64    `json:"totalRatings"`    // always zero
	Thumbnail         string   `json:"thumbnail"`
	FileURL           *string  `json:"fileUrl"`          // always null
	ImageURLs         []Image  `json:"imageUrls"`        // always null
	ThirtyDayPopular  *int64   `json:"thirtyDayPopular"` // always null
	SortName          *string  `json:"sortName"`         // always null
	SendNotifications bool     `json:"sendNotifications"`
}

// LegacyDetail is the outer body of the legacy Detail. Only the identity, the
// texts, the type, the status, the image list and the nested metaData carry
// data; every viewer field, the dates, the counters and the rating aggregates
// are null or zero, and isLoggedOn is false even for the owner of the mod.
type LegacyDetail struct {
	ModID             int64       `json:"modId"`
	GameTitleID       int64       `json:"gameTitleId"`
	GameTitleName     *string     `json:"gameTitleName"` // always null
	ModName           string      `json:"modName"`
	Description       string      `json:"description"`
	ModDescription    string      `json:"modDescription"`
	ChangeList        string      `json:"changeList"`
	ModVersion        float64     `json:"modVersion"` // as stored, no default
	VerifiedVersion   *string     `json:"verifiedVersion"`
	ModTypeID         *int64      `json:"modTypeId"`
	ClientID          *string     `json:"clientId"` // always null
	ModType           *string     `json:"modType"`
	ModTags           []string    `json:"modTags"`   // the ids as strings
	ModTagIDs         []int64     `json:"modTagIds"` // always null
	ModTagNames       []string    `json:"modTagNames"`
	MetaData          *LegacyMeta `json:"metaData"`
	ModStatusID       int64       `json:"modStatusId"`
	ModStatus         string      `json:"modStatus"`
	ModStatusMessage  string      `json:"modStatusMessage"`
	ModVisibility     *string     `json:"modVisibility"`    // always null
	CreatorID         *int64      `json:"creatorId"`        // always null
	CreatorName       *string     `json:"creatorName"`      // always null
	CreatorAvatarURL  *string     `json:"creatorAvatarUrl"` // always null
	IsOwner           *bool       `json:"isOwner"`          // always null here; the snapshot carries the truth
	IsLoggedOn        bool        `json:"isLoggedOn"`       // always false here; the snapshot carries the truth
	AllowDiscussion   bool        `json:"allowDiscussion"`  // always false
	DiscourseURL      *string     `json:"discourseUrl"`     // always null
	CreateDate        *string     `json:"createDate"`       // always null
	LastUpdate        *string     `json:"lastUpdate"`       // always null
	UpdateAvailable   *bool       `json:"updateAvailable"`  // always null
	ModFileSize       *float64    `json:"modFileSize"`      // always null
	ModDiskSize       *float64    `json:"modDiskSize"`      // always null
	Popular           *int64      `json:"popular"`          // always null
	Downloads         *int64      `json:"downloads"`        // always null
	Installs          *int64      `json:"installs"`         // always null
	Likes             *int64      `json:"likes"`            // always null
	UserLiked         *bool       `json:"userLiked"`        // always null
	Ratings           *Ratings    `json:"ratings"`          // always null
	CurrentRating     float64     `json:"currentRating"`    // always zero
	UserDownloaded    *bool       `json:"userDownloaded"`   // always null
	UserInstalled     *bool       `json:"userInstalled"`    // always null
	UserFlagged       *bool       `json:"userFlagged"`      // always null
	UserRating        *float64    `json:"userRating"`       // always null
	UserReview        *string     `json:"userReview"`       // always null
	PositiveRatings   int64       `json:"positiveRatings"`  // always zero
	TotalRatings      int64       `json:"totalRatings"`     // always zero
	Thumbnail         *string     `json:"thumbnail"`        // always null
	FileURL           *string     `json:"fileUrl"`          // always null
	ImageURLs         []Image     `json:"imageUrls"`
	ThirtyDayPopular  *int64      `json:"thirtyDayPopular"` // always null
	SortName          *string     `json:"sortName"`         // always null
	SendNotifications bool        `json:"sendNotifications"`
}

// legacyMetaOf renders the nested snapshot of one rendered Detail.
func legacyMetaOf(detail Detail) LegacyMeta {
	return LegacyMeta{
		ModID:             detail.ModID,
		ModName:           detail.ModName,
		Description:       nilOf(detail.Description),
		ModDescription:    nilOf(detail.ModDescription),
		ModVersion:        detail.ModVersion,
		VerifiedVersion:   detail.VerifiedVersion,
		ModType:           detail.ModType,
		ModTags:           legacyTags(detail.ModTagIDs),
		ModTagNames:       detail.ModTagNames,
		ModStatus:         nilOf(detail.ModStatus),
		ModVisibility:     detail.ModVisibility,
		CreatorName:       detail.CreatorName,
		CreatorAvatarURL:  detail.CreatorAvatarURL,
		IsOwner:           detail.IsOwner,
		IsLoggedOn:        detail.IsLoggedOn,
		CreateDate:        detail.CreateDate,
		LastUpdate:        detail.LastUpdate,
		UpdateAvailable:   detail.UpdateAvailable,
		ModFileSize:       detail.ModFileSize,
		ModDiskSize:       detail.ModDiskSize,
		Downloads:         detail.Downloads,
		Installs:          detail.Installs,
		Likes:             detail.Likes,
		UserLiked:         detail.UserLiked,
		UserDownloaded:    detail.UserDownloaded,
		UserInstalled:     detail.UserInstalled,
		UserFlagged:       detail.UserFlagged,
		UserRating:        detail.UserRating,
		UserReview:        detail.UserReview,
		Thumbnail:         nilOfStringPtr(detail.Thumbnail),
		SendNotifications: detail.SendNotifications,
	}
}

// LegacyDetailOf projects the full Detail into the legacy outer body plus the
// nested metaData snapshot. The outer answers what the legacy client uses -
// identity, texts, type, status and the image list - and leaves the rest null
// or zero, exactly as the capture does even for the owner of the mod. The
// snapshot keeps the rest of the rendered data.
func LegacyDetailOf(detail Detail) LegacyDetail {
	meta := legacyMetaOf(detail)
	return LegacyDetail{
		ModID:           detail.ModID,
		GameTitleID:     detail.GameTitleID,
		ModName:         detail.ModName,
		Description:     detail.Description,
		ModDescription:  detail.ModDescription,
		ChangeList:      detail.ChangeList,
		ModVersion:      detail.ModVersion,
		VerifiedVersion: detail.VerifiedVersion,
		ModTypeID:       detail.ModTypeID,
		ModType:         detail.ModType,
		ModTags:         legacyTags(detail.ModTagIDs),
		ModTagNames:     detail.ModTagNames,
		MetaData:        &meta,
		ModStatusID:     detail.ModStatusID,
		ModStatus:       detail.ModStatus,
		ImageURLs:       detail.ImageURLs,
	}
}

func legacyTags(ids []int64) []string {
	if len(ids) == 0 {
		return []string{}
	}
	tags := make([]string, 0, len(ids))
	for _, id := range ids {
		tags = append(tags, strconv.FormatInt(id, 10))
	}
	return tags
}

// nilOf turns an empty string into a null pointer so a missing value renders
// as null rather than as empty.
func nilOf(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// nilOfStringPtr keeps a nil pointer as null and an empty string as empty.
func nilOfStringPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
