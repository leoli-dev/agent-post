package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"unicode/utf8"
)

// Maximum body setting is one million code points; JSON may use twelve ASCII
// bytes for each escaped supplementary character, plus bounded metadata.
const maxJSONBytes int64 = 13 << 20

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
	if !utf8.Valid(b) || !validEscapedUnicode(b) {
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

// encoding/json replaces lone escaped UTF-16 surrogates. Reject them so the
// accepted code point sequence is exactly the sender's valid Unicode text.
func validEscapedUnicode(b []byte) bool {
	for i := 0; i < len(b); i++ {
		if b[i] != '\\' {
			continue
		}
		i++
		if i >= len(b) {
			return false
		}
		if b[i] != 'u' {
			continue
		}
		if i+4 >= len(b) {
			return false
		}
		v, ok := hexUnit(b[i+1 : i+5])
		if !ok {
			return false
		}
		i += 4
		if v >= 0xdc00 && v <= 0xdfff {
			return false
		}
		if v >= 0xd800 && v <= 0xdbff {
			if i+6 >= len(b) || b[i+1] != '\\' || b[i+2] != 'u' {
				return false
			}
			low, ok := hexUnit(b[i+3 : i+7])
			if !ok || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}
func hexUnit(b []byte) (uint16, bool) {
	var n uint16
	for _, c := range b {
		n <<= 4
		switch {
		case c >= '0' && c <= '9':
			n += uint16(c - '0')
		case c >= 'a' && c <= 'f':
			n += uint16(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			n += uint16(c - 'A' + 10)
		default:
			return 0, false
		}
	}
	return n, true
}
