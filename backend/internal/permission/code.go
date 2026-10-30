package permission

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Batas panjang mengikuti kolom permissions (migrasi 011).
const (
	MaxCodeLength     = 255
	MaxResourceLength = 150
	MaxActionLength   = 100
)

// ErrInvalidCode: kode permission tidak berbentuk <resource>.<action> yang sah.
var ErrInvalidCode = errors.New("permission: kode tidak valid")

// Segmen berupa huruf kecil, angka, dan garis bawah, diawali huruf; minimal dua segmen dipisah titik
// (docs/06 §5.1). Field permission memakai namespace yang sama (`invoice.field.internal_cost.read`).
var codePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

// Split memvalidasi kode dan memisahkannya: resource = semua segmen sebelum titik terakhir,
// action = segmen terakhir (ADR-0022 butir 5).
func Split(code string) (resource, action string, err error) {
	if len(code) > MaxCodeLength || !codePattern.MatchString(code) {
		return "", "", fmt.Errorf("%w: %q", ErrInvalidCode, truncate(code))
	}
	i := strings.LastIndexByte(code, '.')
	resource, action = code[:i], code[i+1:]
	if len(resource) > MaxResourceLength || len(action) > MaxActionLength {
		return "", "", fmt.Errorf("%w: %q melebihi batas panjang resource/action", ErrInvalidCode, truncate(code))
	}
	return resource, action, nil
}

// Namespace mengembalikan segmen pertama kode ("" bila tak ada titik).
func Namespace(code string) string {
	head, _, found := strings.Cut(code, ".")
	if !found {
		return ""
	}
	return head
}

// IsCoreNamespace melaporkan apakah kode berada di namespace milik core (segmen pertama kode
// katalog). Registrasi modul (M07) menolak kode di namespace ini (docs/07 §7).
func IsCoreNamespace(code string) bool {
	ns := Namespace(code)
	if ns == "" {
		return false
	}
	for _, d := range catalog {
		if Namespace(d.Code) == ns {
			return true
		}
	}
	return false
}

// truncate membatasi kode yang ikut masuk pesan galat (kode bisa berasal dari manifest modul).
func truncate(s string) string {
	const limit = 80
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "…"
}
