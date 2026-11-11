package shared

import (
	"encoding/json"
	"testing"
)

func TestFlexIntAcceptsBothShapes(t *testing.T) {
	cases := []struct {
		raw      string
		want     int64
		isString bool
	}{
		{`123`, 123, false},
		{`"123"`, 123, true},
		{`""`, 0, true},
		{`"-14"`, -14, true},
		{`null`, 0, false},
		{`"*"`, 0, true},
	}
	for _, tc := range cases {
		var value FlexInt
		if err := json.Unmarshal([]byte(tc.raw), &value); err != nil {
			t.Fatalf("%s: %v", tc.raw, err)
		}
		if value.Int() != tc.want {
			t.Fatalf("%s: got %d, want %d", tc.raw, value.Int(), tc.want)
		}
		if value.IsString != tc.isString {
			t.Fatalf("%s: got isString=%v, want %v", tc.raw, value.IsString, tc.isString)
		}
	}
}

func TestFlexIntEchoesTheShape(t *testing.T) {
	var value FlexInt
	if err := json.Unmarshal([]byte(`"409484"`), &value); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `"409484"` {
		t.Fatalf("got %s", encoded)
	}
}

func TestOptIntTracksPresence(t *testing.T) {
	type body struct {
		Count OptInt `json:"count"`
	}

	var absent body
	if err := json.Unmarshal([]byte(`{}`), &absent); err != nil {
		t.Fatal(err)
	}
	if absent.Count.Set {
		t.Fatal("absent field reported as set")
	}
	if absent.Count.OrDefault(30) != 30 {
		t.Fatal("default not applied")
	}

	var zero body
	if err := json.Unmarshal([]byte(`{"count":0}`), &zero); err != nil {
		t.Fatal(err)
	}
	if !zero.Count.Set || zero.Count.Value != 0 {
		t.Fatalf("explicit zero lost: %+v", zero.Count)
	}
}
