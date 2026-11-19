// SPDX-License-Identifier: AGPL-3.0-only

package internal

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
)

// Json wraps a value that the game protocol sometimes sends as a JSON string
// containing JSON. UnmarshalJSON accepts both shapes.
type Json[T any] struct {
	Data T
}

func (j *Json[T]) UnmarshalText(text []byte) error {
	return json.Unmarshal(text, &j.Data)
}

func (j *Json[T]) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		return j.UnmarshalText([]byte(text))
	}
	return json.Unmarshal(data, &j.Data)
}

// A and H are the shorthand the route packages use for ad-hoc JSON payloads.
type A = []any
type H map[string]any

// Byte parity with the official service matters: the game's upload task
// compares the raw response, so bodies carry no trailing newline and the
// Content-Type matches the captures ("application/json; charset=utf-8").
func writeJSONHeader(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
}

// JSON encodes data before writing so the response never streams
// request-derived values straight to the client.
func JSON(w http.ResponseWriter, data any) {
	encoded, err := json.Marshal(data)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeJSONHeader(w)
	_, _ = w.Write(encoded)
}

// Bind decodes the request into data: the JSON body for POST, the query string
// for GET, and the form for anything else.
func Bind[D any](r *http.Request, data *D) error {
	switch {
	case r.Method == http.MethodGet:
		return bindValues(data, r.URL.Query())
	case strings.Contains(r.Header.Get("Content-Type"), "application/json"):
		defer func(body io.ReadCloser) {
			_ = body.Close()
		}(r.Body)
		return json.NewDecoder(r.Body).Decode(data)
	default:
		if err := r.ParseForm(); err != nil {
			return err
		}
		return bindValues(data, r.PostForm)
	}
}

// bindValues fills a struct from string values, matching the json tag of each
// field. Unknown keys are ignored, as the game clients send extra fields.
func bindValues[D any](data *D, values map[string][]string) error {
	target := reflect.ValueOf(data)
	if target.Kind() != reflect.Pointer || target.IsNil() {
		return errors.New("bind: destination must be a non-nil pointer")
	}
	target = target.Elem()
	if target.Kind() != reflect.Struct {
		return errors.New("bind: destination must point to a struct")
	}
	typ := target.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		name := field.Tag.Get("json")
		if comma := strings.IndexByte(name, ','); comma >= 0 {
			name = name[:comma]
		}
		if name == "" || name == "-" {
			name = field.Name
		}
		raw, ok := values[name]
		if !ok || len(raw) == 0 {
			continue
		}
		if err := setField(target.Field(i), raw[0]); err != nil {
			return err
		}
	}
	return nil
}

func setField(field reflect.Value, raw string) error {
	if !field.CanSet() {
		return nil
	}
	switch field.Kind() {
	case reflect.String:
		field.SetString(raw)
	case reflect.Bool:
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		field.SetBool(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return err
		}
		field.SetInt(value)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return err
		}
		field.SetUint(value)
	case reflect.Float32, reflect.Float64:
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return err
		}
		field.SetFloat(value)
	default:
		// Unsupported kinds keep their zero value.
	}
	return nil
}
