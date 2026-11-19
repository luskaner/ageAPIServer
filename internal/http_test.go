// SPDX-License-Identifier: AGPL-3.0-only

package internal

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type bindTarget struct {
	Name  string  `json:"name"`
	Count int     `json:"count"`
	OK    bool    `json:"ok"`
	Ratio float64 `json:"ratio"`
	Size  uint    `json:"size"`
	Skip  string  `json:"-"`
}

func TestBindReadsTheQueryString(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/x?name=alpha&count=2&ok=true&ratio=1.5&size=3&unknown=1", nil)
	var target bindTarget
	if err := Bind(request, &target); err != nil {
		t.Fatalf("bind: %v", err)
	}
	want := bindTarget{Name: "alpha", Count: 2, OK: true, Ratio: 1.5, Size: 3}
	if target != want {
		t.Fatalf("target = %+v, want %+v", target, want)
	}
}

func TestBindReadsAJSONBody(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"name":"beta","count":4,"ok":true}`))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	var target bindTarget
	if err := Bind(request, &target); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if target.Name != "beta" || target.Count != 4 || !target.OK {
		t.Fatalf("target = %+v", target)
	}
}

func TestBindReadsAForm(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("name=gamma&count=5"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var target bindTarget
	if err := Bind(request, &target); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if target.Name != "gamma" || target.Count != 5 {
		t.Fatalf("target = %+v", target)
	}
}

func TestBindRejectsWrongValues(t *testing.T) {
	cases := map[string]string{
		"bool":  "ok=maybe",
		"int":   "count=x",
		"uint":  "size=-1",
		"float": "ratio=x",
	}
	for name, query := range cases {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/x?"+query, nil)
			var target bindTarget
			if err := Bind(request, &target); err == nil {
				t.Fatal("invalid value accepted")
			}
		})
	}
}

func TestBindValuesRejectsBadDestinations(t *testing.T) {
	var pointer *bindTarget
	if err := bindValues(pointer, map[string][]string{}); err == nil {
		t.Fatal("a nil pointer was accepted")
	}
	var number int
	if err := bindValues(&number, map[string][]string{}); err == nil {
		t.Fatal("a pointer to a non-struct was accepted")
	}
}

func TestBindValuesKeepsUnsupportedFieldsAtZero(t *testing.T) {
	type nested struct {
		Inner struct{ X int } `json:"inner"`
	}
	var target nested
	if err := bindValues(&target, map[string][]string{"inner": {"x"}}); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if target.Inner.X != 0 {
		t.Fatalf("inner = %+v", target.Inner)
	}
}

func TestJsonAcceptsBothShapes(t *testing.T) {
	var data Json[map[string]int]
	if err := json.Unmarshal([]byte(`{"a":1}`), &data); err != nil {
		t.Fatalf("object: %v", err)
	}
	if data.Data["a"] != 1 {
		t.Fatalf("data = %+v", data.Data)
	}
	// The game sometimes double-encodes the object as a JSON string.
	if err := json.Unmarshal([]byte(`"{\"a\":2}"`), &data); err != nil {
		t.Fatalf("string: %v", err)
	}
	if data.Data["a"] != 2 {
		t.Fatalf("data = %+v", data.Data)
	}
}

func TestJsonRejectsANonJSONString(t *testing.T) {
	var data Json[map[string]int]
	if err := json.Unmarshal([]byte(`"not json"`), &data); err == nil {
		t.Fatal("a plain string was accepted")
	}
}

func TestJSONReportsAnUnencodableValue(t *testing.T) {
	recorder := httptest.NewRecorder()
	JSON(recorder, make(chan int))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", recorder.Code)
	}
}
