package shared

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// FlexInt is the oneOf integer|string the spec uses for `modid` and `tags[]`.
// It accepts 123, "123", "-14", "" (the sentinel for "no filter") and null, and
// remembers the raw form so searchParams can echo it as the client sent it.
type FlexInt struct {
	Value    int64
	IsString bool
	Raw      json.RawMessage
}

// UnmarshalJSON accepts both shapes of the oneOf.
func (f *FlexInt) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	f.Raw = append(f.Raw[:0], trimmed...)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		f.Value = 0
		f.IsString = false
		return nil
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return err
		}
		f.IsString = true
		text = strings.TrimSpace(text)
		if text == "" {
			f.Value = 0
			return nil
		}
		value, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			// A non numeric string is the "no filter" sentinel, not an error:
			// the clients send "" and "*" in the same field.
			f.Value = 0
			return nil
		}
		f.Value = value
		return nil
	}
	var value int64
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return err
	}
	f.Value = value
	f.IsString = false
	return nil
}

// MarshalJSON renders the value in the shape it arrived.
func (f FlexInt) MarshalJSON() ([]byte, error) {
	if f.IsString {
		return json.Marshal(strconv.FormatInt(f.Value, 10))
	}
	return json.Marshal(f.Value)
}

// Int is the numeric value, with the sentinels already collapsed to 0.
func (f FlexInt) Int() int64 {
	return f.Value
}

// OptInt is an integer that knows whether the client sent it, so the schema
// default can be told apart from an explicit 0 (which is a sentinel meaning
// "no limit" for count).
type OptInt struct {
	Value int64
	Set   bool
}

// UnmarshalJSON records the presence of the field.
func (o *OptInt) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var value int64
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return err
	}
	o.Value = value
	o.Set = true
	return nil
}

// MarshalJSON renders the value, or null when the client did not send it.
func (o OptInt) MarshalJSON() ([]byte, error) {
	if !o.Set {
		return []byte("null"), nil
	}
	return json.Marshal(o.Value)
}

// OrDefault returns the value, or fallback when the client did not send it.
func (o OptInt) OrDefault(fallback int64) int64 {
	if !o.Set {
		return fallback
	}
	return o.Value
}
