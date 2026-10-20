package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
)

// DefaultMaxBodyBytes adalah batas body JSON bawaan (docs/08 §2.3: 1 MiB).
const DefaultMaxBodyBytes int64 = 1 << 20

// DecodeJSON membaca tepat satu nilai JSON dari body ke dst. maxBytes <= 0 memakai
// DefaultMaxBodyBytes. Aturan (docs/08 §2.3): Content-Type harus application/json (415), body
// melebihi batas → 413, field tak dikenal ditolak, body kosong/rusak/berisi lebih dari satu nilai →
// validation_failed. Error yang dikembalikan selalu *Error.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/json" {
		return UnsupportedMediaType.New()
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBodyBytes
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return decodeError(err)
	}
	// Tidak boleh ada nilai kedua setelah yang pertama.
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Validation(FieldError{Field: "", Code: "multiple_values", Message: "body must contain a single JSON value"})
		}
		return decodeError(err)
	}
	return nil
}

const unknownFieldPrefix = "json: unknown field "

func decodeError(err error) *Error {
	var mbe *http.MaxBytesError
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	switch {
	case errors.As(err, &mbe):
		return PayloadTooLarge.Wrap(err)
	case errors.Is(err, io.EOF):
		return Validation(FieldError{Code: "required", Message: "request body is required"})
	case errors.Is(err, io.ErrUnexpectedEOF), errors.As(err, &syn):
		return Validation(FieldError{Code: "malformed_json", Message: "request body is not valid JSON"})
	case errors.As(err, &typ):
		return Validation(FieldError{Field: typ.Field, Code: "invalid_type", Message: "value has the wrong type"})
	case strings.HasPrefix(err.Error(), unknownFieldPrefix):
		name := strings.Trim(strings.TrimPrefix(err.Error(), unknownFieldPrefix), `"`)
		return Validation(FieldError{Field: name, Code: "unknown_field", Message: "unknown field"})
	default:
		return Validation(FieldError{Code: "malformed_json", Message: "request body could not be read"})
	}
}
