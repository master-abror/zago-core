package authhttp

import "strings"

// OriginPolicy memeriksa header Origin terhadap ALLOWED_ORIGINS. Dipakai CSRF (docs/05 §8) dan
// handshake WebSocket (docs/09 §2).
type OriginPolicy struct {
	allowed map[string]struct{}
}

// NewOriginPolicy membuat kebijakan dari daftar origin (scheme://host[:port]). Entri
// dinormalisasi ke huruf kecil tanpa garis miring penutup.
func NewOriginPolicy(origins []string) OriginPolicy {
	p := OriginPolicy{allowed: make(map[string]struct{}, len(origins))}
	for _, o := range origins {
		o = strings.TrimRight(strings.ToLower(strings.TrimSpace(o)), "/")
		if o != "" {
			p.allowed[o] = struct{}{}
		}
	}
	return p
}

// Allows melaporkan apakah origin persis salah satu yang diizinkan. Pencocokan ketat terhadap
// nilai yang dikirim peramban (huruf kecil, tanpa path/slash): "null", subdomain, port lain,
// skema lain, dan slash penutup semuanya ditolak.
func (p OriginPolicy) Allows(origin string) bool {
	_, ok := p.allowed[origin]
	return ok
}
