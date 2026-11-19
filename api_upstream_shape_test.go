package ageAPIServer

import (
	"encoding/json"
	"net/http"
	"testing"
)

// decodeMap reads a JSON object body into a generic map.
func decodeMap(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("not a json object: %v (%s)", err, body)
	}
	return out
}

// TestListResponsesCarryTheUpstreamEnvelope pins the envelope every list
// endpoint returns: errorCode -1, an empty errorMessage and the searchParams
// echo, all of them present even when the response carries data.
func TestListResponsesCarryTheUpstreamEnvelope(t *testing.T) {
	server, path := newTestServer(t)
	seed(t, path)

	body := `{"q":"","filter":0,"sort":"popular","order":"desc","start":1,"count":30,"game":2,"modid":""}`
	recorder := do(t, server.Handler(), http.MethodPost, "/api/v4/Mods/Find/", body, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", recorder.Code, recorder.Body.String())
	}
	response := decodeMap(t, recorder.Body.Bytes())

	if code, ok := response["errorCode"]; !ok || code != float64(-1) {
		t.Fatalf("errorCode = %v (present=%v), want -1", code, ok)
	}
	if message, ok := response["errorMessage"]; !ok || message != "" {
		t.Fatalf("errorMessage = %v (present=%v), want an empty string", message, ok)
	}

	params, ok := response["searchParams"].(map[string]any)
	if !ok {
		t.Fatalf("searchParams = %v", response["searchParams"])
	}
	for _, key := range []string{"q", "creator", "filter", "sort", "order", "start", "count", "game", "modid"} {
		if _, present := params[key]; !present {
			t.Fatalf("searchParams is missing %q: %v", key, params)
		}
	}
}

// TestRatingsIsAlwaysAnObject pins the November shape of the ratings block: it
// is an object even for a mod without ratings, and it carries only average and
// totalCount.
func TestRatingsIsAlwaysAnObject(t *testing.T) {
	server, path := newTestServer(t)
	seed(t, path)

	body := `{"game":2,"start":1,"count":30}`
	recorder := do(t, server.Handler(), http.MethodPost, "/api/v4/Mods/Find/", body, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", recorder.Code, recorder.Body.String())
	}
	response := decodeMap(t, recorder.Body.Bytes())
	mods, ok := response["modList"].([]any)
	if !ok || len(mods) == 0 {
		t.Fatalf("modList = %v", response["modList"])
	}

	for _, entry := range mods {
		mod := entry.(map[string]any)
		ratings, ok := mod["ratings"].(map[string]any)
		if !ok {
			t.Fatalf("mod %v has ratings = %v, want an object", mod["modId"], mod["ratings"])
		}
		for _, gone := range []string{"leastFavorableReview", "mostFavorableReview"} {
			if _, present := ratings[gone]; present {
				t.Fatalf("ratings still carries %q: %v", gone, ratings)
			}
		}
		rating, ok := ratings["rating"].(map[string]any)
		if !ok {
			t.Fatalf("ratings.rating = %v", ratings["rating"])
		}
		for _, key := range []string{"average", "totalCount"} {
			if _, present := rating[key]; !present {
				t.Fatalf("ratings.rating is missing %q: %v", key, rating)
			}
		}
		for _, gone := range []string{"count1Star", "count2Star", "count3Star", "count4Star", "count5Star"} {
			if _, present := rating[gone]; present {
				t.Fatalf("ratings.rating still carries %q: %v", gone, rating)
			}
		}
		if _, present := ratings["reviewsCount"]; !present {
			t.Fatalf("ratings is missing reviewsCount: %v", ratings)
		}
	}
}

// TestRateMatchesUpstreamEnvelope pins the body of Rate: statusCode and
// responseMessage are null and the aggregate sits under playFabResponse.
func TestRateMatchesUpstreamEnvelope(t *testing.T) {
	server, path := newTestServer(t)
	seed(t, path)
	auth := map[string]string{"Authorization": "Steam1.0 c21va2UtdG9rZW4="}

	recorder := do(t, server.Handler(), http.MethodPost, "/api/v4/Mods/Rate/", `{"modId":100,"rating":5}`, auth)
	if recorder.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", recorder.Code, recorder.Body.String())
	}
	response := decodeMap(t, recorder.Body.Bytes())

	for _, key := range []string{"statusCode", "responseMessage"} {
		value, present := response[key]
		if !present {
			t.Fatalf("%s is absent: %v", key, response)
		}
		if value != nil {
			t.Fatalf("%s = %v, want null", key, value)
		}
	}
	playFab, ok := response["playFabResponse"].(map[string]any)
	if !ok {
		t.Fatalf("playFabResponse = %v", response["playFabResponse"])
	}
	rating, ok := playFab["rating"].(map[string]any)
	if !ok {
		t.Fatalf("playFabResponse.rating = %v", playFab["rating"])
	}
	if rating["average"] != float64(5) || rating["totalCount"] != float64(1) {
		t.Fatalf("rating = %v", rating)
	}
	if playFab["reviewsCount"] != float64(0) {
		t.Fatalf("reviewsCount = %v", playFab["reviewsCount"])
	}
}

// TestReviewsTotalCountIsNullWhenEmpty pins the null totalCount of a mod
// without reviews.
func TestReviewsTotalCountIsNullWhenEmpty(t *testing.T) {
	server, path := newTestServer(t)
	seed(t, path)

	recorder := do(t, server.Handler(), http.MethodPost, "/api/v4/mods/Reviews",
		`{"modId":"100","page":1,"count":5}`, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", recorder.Code, recorder.Body.String())
	}
	response := decodeMap(t, recorder.Body.Bytes())
	if value, present := response["totalCount"]; !present || value != nil {
		t.Fatalf("totalCount = %v (present=%v), want null", value, present)
	}
	if reviews, ok := response["reviews"].([]any); !ok || len(reviews) != 0 {
		t.Fatalf("reviews = %v, want an empty array", response["reviews"])
	}
}

// TestRelatedEchoesAFixedPage pins the echo of Related: start 0, a page of six
// and the mod id as a string, with totalCount equal to the page size.
func TestRelatedEchoesAFixedPage(t *testing.T) {
	server, path := newTestServer(t)
	seed(t, path)

	recorder := do(t, server.Handler(), http.MethodGet, "/api/v4/mods/Related/100", "", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", recorder.Code, recorder.Body.String())
	}
	response := decodeMap(t, recorder.Body.Bytes())

	params, ok := response["searchParams"].(map[string]any)
	if !ok {
		t.Fatalf("searchParams = %v", response["searchParams"])
	}
	if params["start"] != float64(0) {
		t.Fatalf("start = %v, want 0", params["start"])
	}
	if params["count"] != float64(6) {
		t.Fatalf("count = %v, want 6", params["count"])
	}
	if params["modid"] != "100" {
		t.Fatalf("modid = %v, want the string \"100\"", params["modid"])
	}
	if params["game"] != float64(0) {
		t.Fatalf("game = %v, want 0", params["game"])
	}

	mods, ok := response["modList"].([]any)
	if !ok {
		t.Fatalf("modList = %v", response["modList"])
	}
	if response["totalCount"] != float64(len(mods)) {
		t.Fatalf("totalCount = %v, want %d", response["totalCount"], len(mods))
	}
	if len(mods) > 6 {
		t.Fatalf("got %d related mods, want at most 6", len(mods))
	}
}

// TestRateRejectsBadInput covers the three ways Rate can refuse a request.
func TestRateRejectsBadInput(t *testing.T) {
	server, path := newTestServer(t)
	seed(t, path)
	handler := server.Handler()
	auth := map[string]string{"Authorization": "Steam1.0 c21va2UtdG9rZW4="}

	for name, body := range map[string]string{
		"not json":    `not json`,
		"no modId":    `{"rating":5}`,
		"bad rating":  `{"modId":100,"rating":9}`,
		"no rating":   `{"modId":100}`,
		"empty body":  ``,
		"wrong types": `{"modId":"x","rating":"y"}`,
	} {
		t.Run(name, func(t *testing.T) {
			recorder := do(t, handler, http.MethodPost, "/api/v4/Mods/Rate/", body, auth)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("code = %d, want 400", recorder.Code)
			}
		})
	}

	recorder := do(t, handler, http.MethodPost, "/api/v4/Mods/Rate/", `{"modId":999,"rating":5}`, auth)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown mod: code = %d, want 404", recorder.Code)
	}
}

// TestReviewsRejectsBadInput covers the ways Reviews can refuse a request.
func TestReviewsRejectsBadInput(t *testing.T) {
	server, _ := newTestServer(t)
	handler := server.Handler()

	for name, body := range map[string]string{
		"not json":       `not json`,
		"no modId":       `{"page":1}`,
		"non numeric id": `{"modId":"abc"}`,
		"empty body":     ``,
	} {
		t.Run(name, func(t *testing.T) {
			recorder := do(t, handler, http.MethodPost, "/api/v4/mods/Reviews", body, nil)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("code = %d, want 400", recorder.Code)
			}
		})
	}
}

// TestRelatedRejectsBadInput covers the path and lookup failures of Related.
func TestRelatedRejectsBadInput(t *testing.T) {
	server, _ := newTestServer(t)
	handler := server.Handler()

	recorder := do(t, handler, http.MethodGet, "/api/v4/mods/Related/abc", "", nil)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("non numeric id: code = %d, want 400", recorder.Code)
	}

	recorder = do(t, handler, http.MethodGet, "/api/v4/mods/Related/999", "", nil)
	if recorder.Code != http.StatusNoContent {
		// The capture of a deleted mod answers 204 instead of an error.
		t.Fatalf("unknown mod: code = %d, want 204", recorder.Code)
	}
}
