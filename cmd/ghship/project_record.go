package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// Project records are ownership evidence; ambiguous repeated fields are unsafe.
func decodeProjectRecord(data []byte, target any, required ...string) error {
	invalid := errors.New("invalid or ambiguous project record")
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return invalid
	}
	seen := map[string]bool{}
	nullFields := map[string]bool{}
	for decoder.More() {
		keyToken, e := decoder.Token()
		key, ok := keyToken.(string)
		if e != nil || !ok {
			return invalid
		}
		key = strings.ToLower(key)
		if seen[key] {
			return invalid
		}
		seen[key] = true
		var value json.RawMessage
		if e = decoder.Decode(&value); e != nil {
			return invalid
		}
		nullFields[key] = bytes.Equal(bytes.TrimSpace(value), []byte("null"))
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') {
		return invalid
	}
	if _, err = decoder.Token(); err != io.EOF {
		return invalid
	}
	for _, key := range required {
		if !seen[strings.ToLower(key)] || nullFields[strings.ToLower(key)] {
			return invalid
		}
	}
	if err = json.Unmarshal(data, target); err != nil {
		return invalid
	}
	return nil
}
