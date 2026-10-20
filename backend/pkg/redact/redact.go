// Package redact menyamarkan nilai sensitif sebelum masuk metadata activity, system log, atau
// log aplikasi (docs/13 §2.3). Dua lapis: (1) KUNCI yang namanya menunjukkan rahasia
// (password, token, secret, authorization, …) — seluruh subtree-nya diganti Placeholder;
// (2) NILAI teks yang berbentuk rahasia (header Bearer/Basic, JWT, kredensial di URL,
// "password=…") disamarkan di dalam string mana pun.
//
// Bias desain: lebih baik menyamarkan berlebihan daripada membocorkan. Kunci seperti
// "token_count" ikut tersamar; modul yang butuh nilainya sebaiknya memakai nama kunci lain.
package redact

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Placeholder menggantikan nilai yang disamarkan.
const Placeholder = "[REDACTED]"

const (
	truncatedMarker = "[TRUNCATED]"
	maxDepth        = 20
)

// Potongan nama kunci (setelah dinormalisasi: huruf kecil, tanpa pemisah) yang menandakan rahasia.
var sensitiveSubstrings = []string{
	"password", "passwd", "passphrase", "secret", "token", "authorization",
	"apikey", "accesskey", "privatekey", "credential", "cookie", "cardnumber",
	"cvv", "cvc", "sessionid", "recoverycode",
}

// Nama kunci lengkap (ternormalisasi) yang terlalu pendek untuk dicocokkan sebagai potongan.
var sensitiveExact = map[string]struct{}{
	"pwd": {}, "pin": {}, "otp": {}, "totp": {}, "mfacode": {}, "sid": {},
}

var valuePatterns = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9\-._~+/]{8,}=*`), "${1} " + Placeholder},
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]*`), Placeholder},
	{regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.\-]*://)[^\s/@:]+:[^\s/@]+@`), "${1}" + Placeholder + "@"},
	{regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|token|api[_-]?key)(\s*[=:]\s*)[^\s,;&"']+`), "${1}${2}" + Placeholder},
}

// Redactor menyamarkan kunci dan nilai sensitif. Aman dipakai bersamaan (tidak berubah setelah dibuat).
type Redactor struct {
	extra map[string]struct{}
}

// Default adalah Redactor dengan daftar bawaan saja.
func Default() *Redactor { return &Redactor{} }

// New menambahkan nama kunci sensitif deklarasi modul (docs/13 §2.3: "module-declared sensitive
// fields"). Kunci tambahan dicocokkan utuh setelah normalisasi.
func New(extraKeys ...string) *Redactor {
	r := &Redactor{extra: make(map[string]struct{}, len(extraKeys))}
	for _, k := range extraKeys {
		if n := normalizeKey(k); n != "" {
			r.extra[n] = struct{}{}
		}
	}
	return r
}

func normalizeKey(k string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(k) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// IsSensitiveKey melaporkan apakah nilai di bawah kunci ini harus disamarkan.
func (r *Redactor) IsSensitiveKey(key string) bool {
	n := normalizeKey(key)
	if n == "" {
		return false
	}
	if _, ok := sensitiveExact[n]; ok {
		return true
	}
	if _, ok := r.extra[n]; ok {
		return true
	}
	for _, s := range sensitiveSubstrings {
		if strings.Contains(n, s) {
			return true
		}
	}
	return false
}

// String menyamarkan pola rahasia di dalam teks bebas.
func (r *Redactor) String(s string) string {
	for _, p := range valuePatterns {
		s = p.re.ReplaceAllString(s, p.repl)
	}
	return s
}

// Value mengembalikan salinan v (struct, map, slice, atau skalar) yang sudah disamarkan, dalam
// bentuk generik JSON (map[string]any, []any, string, json.Number, bool, nil). Nilai yang tak
// dapat di-encode ke JSON menghasilkan error.
func (r *Redactor) Value(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("redact: nilai tidak dapat di-encode JSON: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return nil, fmt.Errorf("redact: decode ulang gagal: %w", err)
	}
	return r.walk(generic, 0), nil
}

// Map seperti Value untuk metadata berbentuk objek. m nil menghasilkan map kosong (bukan nil).
func (r *Redactor) Map(m map[string]any) (map[string]any, error) {
	if m == nil {
		return map[string]any{}, nil
	}
	v, err := r.Value(m)
	if err != nil {
		return nil, err
	}
	out, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("redact: hasil bukan objek")
	}
	return out, nil
}

func (r *Redactor) walk(v any, depth int) any {
	if depth > maxDepth {
		return truncatedMarker
	}
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if r.IsSensitiveKey(k) {
				out[k] = Placeholder
				continue
			}
			out[k] = r.walk(val, depth+1)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = r.walk(val, depth+1)
		}
		return out
	case string:
		return r.String(t)
	default:
		return v
	}
}
