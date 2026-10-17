package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"unicode/utf8"
)

func contextBackground() context.Context { return context.Background() }
func ioReadBounded(r io.Reader, n int64) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(r, n+1))
	if e != nil {
		return nil, e
	}
	if int64(len(b)) > n {
		return nil, bad("request_too_large")
	}
	return b, nil
}
func strictJSON(b []byte, v any) error {
	if !utf8.Valid(b) {
		return bad("invalid_unicode")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return bad("invalid_json")
	}
	if d.Decode(new(any)) != io.EOF {
		return bad("invalid_json")
	}
	return nil
}
