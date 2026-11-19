// SPDX-License-Identifier: AGPL-3.0-only

package shared

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/luskaner/ageAPIServer/internal/models"
)

// ErrMissingGame is returned when a request omits the required game field.
var ErrMissingGame = errors.New("game is required")

// FindRequest is the body of Find, Featured, Installed and My. The four
// endpoints share the filter shape; only the defaults differ, and those live in
// FilterDefaults.
type FindRequest struct {
	Game    int64     `json:"game"`
	Start   OptInt    `json:"start"`
	Count   OptInt    `json:"count"`
	Order   string    `json:"order"`
	Sort    string    `json:"sort"`
	Q       string    `json:"q"`
	Filter  OptInt    `json:"filter"`
	Creator *string   `json:"creator"`
	ModID   FlexInt   `json:"modid"`
	Status  *string   `json:"status"`
	Tags    []FlexInt `json:"tags"`
}

// FindRequestV1 is the legacy AOE2 body: no tags, no status, no modid.
type FindRequestV1 struct {
	Game   int64  `json:"game"`
	Start  OptInt `json:"start"`
	Count  OptInt `json:"count"`
	Order  string `json:"order"`
	Sort   string `json:"sort"`
	Q      string `json:"q"`
	Filter OptInt `json:"filter"`
	Time   string `json:"time"`
}

// DetailRequestV1 is the legacy AOE2 detail body. `game` is optional: the
// capture sends only `{"modid":93963}`.
type DetailRequestV1 struct {
	ModID FlexInt `json:"modid"`
	Game  OptInt  `json:"game"`
}

// RateRequest is the body of Rate.
type RateRequest struct {
	ModID  int64 `json:"modId"`
	Rating int64 `json:"rating"`
}

// FlagRequest is the body of the legacy Flag endpoint.
type FlagRequest struct {
	ID          int64  `json:"id"`
	StringValue string `json:"stringValue"`
	BoolValue   *bool  `json:"boolValue"`
}

// InstallRequest is the body of Install and Uninstall.
type InstallRequest struct {
	ID int64 `json:"id"`
}

// ReviewsRequest is the body of Reviews. modId is a string in the spec.
type ReviewsRequest struct {
	ModID       string `json:"modId"`
	Page        OptInt `json:"page"`
	Count       OptInt `json:"count"`
	RatingNum   OptInt `json:"ratingNum"`
	RatingLabel string `json:"ratingLabel"`
}

// TextModerationRequest is the body of /textmoderation.
type TextModerationRequest struct {
	ConversationID  string `json:"conversationId"`
	TextContent     string `json:"textContent"`
	Language        string `json:"language"`
	TextType        string `json:"textType"`
	IncludeResponse bool   `json:"includeResponse"`
}

// DeleteRequest is the body of Delete. modId is a number here, unlike Report.
type DeleteRequest struct {
	ModID int64 `json:"modId"`
}

// ReportRequest is the body of Report. Every field is a string in the capture,
// including modId.
type ReportRequest struct {
	ModID               string `json:"modId"`
	ConcernCategoryID   string `json:"concernCategoryId"`
	ConcernCategoryName string `json:"concernCategoryName"`
	ConcernContent      string `json:"concernContent"`
}

// FilterDefaults carries the per-endpoint schema defaults.
type FilterDefaults struct {
	Sort  string
	Order string
	Count int64
}

// ToFilter resolves the request into the store filter.
func (r FindRequest) ToFilter(p Pagination, d FilterDefaults) models.FindFilter {
	count := r.Count
	if !count.Set && d.Count > 0 {
		count = OptInt{Value: d.Count, Set: true}
	}
	offset, limit := p.Resolve(r.Start, count)

	tags := make([]int64, 0, len(r.Tags))
	for _, tag := range r.Tags {
		tags = append(tags, tag.Int())
	}

	sort := r.Sort
	if sort == "" {
		sort = d.Sort
	}
	order := r.Order
	if order == "" {
		order = d.Order
	}

	return models.FindFilter{
		GameTitleID: r.Game,
		Q:           r.Q,
		Filter:      r.Filter.Value,
		Creator:     deref(r.Creator),
		ModID:       r.ModID.Int(),
		Status:      deref(r.Status),
		Tags:        tags,
		Sort:        sort,
		Order:       order,
		Offset:      offset,
		Limit:       limit,
	}
}

// ToFilter resolves the legacy body into the store filter.
func (r FindRequestV1) ToFilter(p Pagination, d FilterDefaults) models.FindFilter {
	count := r.Count
	if !count.Set && d.Count > 0 {
		count = OptInt{Value: d.Count, Set: true}
	}
	offset, limit := p.Resolve(r.Start, count)

	sort := r.Sort
	if sort == "" {
		sort = d.Sort
	}
	order := r.Order
	if order == "" {
		order = d.Order
	}

	return models.FindFilter{
		GameTitleID: r.Game,
		Q:           r.Q,
		Filter:      r.Filter.Value,
		Sort:        sort,
		Order:       order,
		Offset:      offset,
		Limit:       limit,
	}
}

// WithEchoDefaults returns a copy of the request with the endpoint defaults
// filled in. The searchParams echo of every list endpoint shows the resolved
// values, not the raw body: the AOE3 My answers creator=caller and count 10,
// its Installed answers creator="" and count 1000, and the Featured capture
// answers sort recentpopular and count 3, all of them for requests that only
// carried game.
func (r FindRequest) WithEchoDefaults(sort string, order string, count int64, creator *string) FindRequest {
	if r.Sort == "" {
		r.Sort = sort
	}
	if r.Order == "" {
		r.Order = order
	}
	if !r.Count.Set && count > 0 {
		r.Count = OptInt{Value: count, Set: true}
	}
	if r.Creator == nil && creator != nil {
		r.Creator = creator
	}
	return r
}

// modIDEcho is the modid of the echo. The captures always render it as the
// JSON string of the number: the web Find sends the numeric 0 and the answer
// carries "0"; the game clients omit the field and the answer still carries
// "0". The legacy v1 echo keeps null for the empty value, as its captures
// show.
func modIDEcho(raw FlexInt, unset json.RawMessage) json.RawMessage {
	if raw.IsString {
		if len(raw.Raw) == 0 {
			return unset
		}
		return raw.Raw
	}
	return json.RawMessage(strconv.Quote(strconv.FormatInt(raw.Value, 10)))
}

// SearchParams echoes the request. `status` and `tags` are not part of the
// schema, so they cannot be echoed; `modid` keeps the shape the client used.
func (r FindRequest) SearchParams() SearchParams {
	params := SearchParams{
		Q:      stringPtr(r.Q),
		Filter: r.Filter.Value,
		Start:  r.Start.OrDefault(1),
		Count:  r.Count.Value,
		Game:   r.Game,
		ModID:  modIDEcho(r.ModID, json.RawMessage(`"0"`)),
	}
	if r.Creator != nil {
		params.Creator = r.Creator
	}
	if r.Sort != "" {
		sort := r.Sort
		params.Sort = &sort
	}
	if r.Order != "" {
		// The echo upper-cases the order: the AOE3 Find sends "desc" and the
		// answer carries "DESC", matching the web captures that send "DESC".
		order := strings.ToUpper(r.Order)
		params.Order = &order
	}
	return params
}

// SearchParams echoes the legacy request.
func (r FindRequestV1) SearchParams() SearchParams {
	params := SearchParams{
		Q:      stringPtr(r.Q),
		Filter: r.Filter.Value,
		Start:  r.Start.OrDefault(1),
		Count:  r.Count.Value,
		Game:   r.Game,
	}
	if r.Sort != "" {
		sort := r.Sort
		params.Sort = &sort
	}
	if r.Order != "" {
		order := r.Order
		params.Order = &order
	}
	return params
}

// Validate rejects a body without the required game.
func (r FindRequest) Validate() error {
	if r.Game <= 0 {
		return ErrMissingGame
	}
	return nil
}

// Validate rejects a body without the required game.
func (r FindRequestV1) Validate() error {
	if r.Game <= 0 {
		return ErrMissingGame
	}
	return nil
}

// Validate rejects a body without the required modid.
func (r DetailRequestV1) Validate() error {
	if r.ModID.Int() <= 0 {
		return errors.New("modid is required")
	}
	return nil
}

// Validate rejects a rating outside 1..5.
func (r RateRequest) Validate() error {
	if r.ModID <= 0 {
		return errors.New("modId is required")
	}
	if r.Rating < 1 || r.Rating > 5 {
		return errors.New("rating must be between 1 and 5")
	}
	return nil
}

// Validate rejects a flag without id or reason.
func (r FlagRequest) Validate() error {
	if r.ID <= 0 {
		return errors.New("id is required")
	}
	if r.StringValue == "" {
		return errors.New("stringValue is required")
	}
	return nil
}

// Validate rejects an install without id.
func (r InstallRequest) Validate() error {
	if r.ID <= 0 {
		return errors.New("id is required")
	}
	return nil
}

// Validate rejects a review request without a numeric modId.
func (r ReviewsRequest) Validate() error {
	if _, err := models.ParseModID(r.ModID); err != nil {
		return errors.New("modId is required")
	}
	return nil
}

// Validate rejects a moderation request that misses a required field.
func (r TextModerationRequest) Validate() error {
	if r.ConversationID == "" {
		return errors.New("conversationId is required")
	}
	if r.TextContent == "" {
		return errors.New("textContent is required")
	}
	if r.Language == "" {
		return errors.New("language is required")
	}
	switch r.TextType {
	case "SanitisationUsername", "SanitisationInput":
	default:
		return errors.New("textType must be SanitisationUsername or SanitisationInput")
	}
	return nil
}

// Validate rejects a delete without modId.
func (r DeleteRequest) Validate() error {
	if r.ModID <= 0 {
		return errors.New("modId is required")
	}
	return nil
}

// Validate rejects a report without a category. The modId is parsed by the
// handler, which is the only place that needs its numeric value.
func (r ReportRequest) Validate() error {
	if r.ConcernCategoryID == "" {
		return errors.New("concernCategoryId is required")
	}
	if r.ConcernCategoryName == "" {
		return errors.New("concernCategoryName is required")
	}
	return nil
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func stringPtr(value string) *string {
	out := value
	return &out
}

// RawModID is the raw form of a modid, for the searchParams echo.
func (r FindRequest) RawModID() json.RawMessage {
	return r.ModID.Raw
}
