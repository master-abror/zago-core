package domain

import (
	"errors"
	"sort"
	"strings"
)

// ErrInvalidGrant menandai grant yang tidak boleh masuk himpunan effective permissions: kode
// kosong atau berspasi di ujung, kode terlalu panjang, atau level selain platform, organization,
// dan group. Level resource dan own sengaja ditolak: assignment ber-scope itu tidak termasuk
// himpunan yang di-cache dan dievaluasi saat pengecekan (docs/06 §5.2).
var ErrInvalidGrant = errors.New("authorization: grant effective permission tidak sah")

// maxCodeLen mengikuti kolom permissions.code (varchar(255)).
const maxCodeLen = 255

// EffectivePermissions adalah himpunan kode permission yang berlaku untuk satu pasangan
// (Identity, ScopeContext), dengan level terluas yang memberi tiap kode (docs/06 §5.2). Nilai nol
// adalah himpunan kosong (tidak memberi apa pun). Tipe ini tidak dapat diubah dari luar paket:
// akses lewat metode, dan Grants/Codes mengembalikan salinan.
type EffectivePermissions struct {
	grants map[string]DecisionLevel
}

// Level mengembalikan level terluas yang memberi code; ok=false bila tidak diberi.
func (e EffectivePermissions) Level(code string) (DecisionLevel, bool) {
	l, ok := e.grants[code]
	return l, ok
}

// Has melaporkan apakah code diberi.
func (e EffectivePermissions) Has(code string) bool {
	_, ok := e.grants[code]
	return ok
}

// Len mengembalikan jumlah kode yang diberi.
func (e EffectivePermissions) Len() int { return len(e.grants) }

// Codes mengembalikan salinan kode yang diberi, terurut.
func (e EffectivePermissions) Codes() []string {
	out := make([]string, 0, len(e.grants))
	for c := range e.grants {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// Grants mengembalikan salinan peta kode → level (untuk serialisasi cache).
func (e EffectivePermissions) Grants() map[string]DecisionLevel {
	out := make(map[string]DecisionLevel, len(e.grants))
	for c, l := range e.grants {
		out[c] = l
	}
	return out
}

// EffectiveBuilder menyusun EffectivePermissions; untuk kode yang sama level terluas menang.
type EffectiveBuilder struct {
	grants map[string]DecisionLevel
}

// NewEffectiveBuilder membuat builder kosong.
func NewEffectiveBuilder() *EffectiveBuilder {
	return &EffectiveBuilder{grants: map[string]DecisionLevel{}}
}

// Add mencatat bahwa code diberi pada level. Grant tidak sah ditolak dengan ErrInvalidGrant dan
// tidak tersimpan.
func (b *EffectiveBuilder) Add(code string, level DecisionLevel) error {
	if code == "" || code != strings.TrimSpace(code) || len(code) > maxCodeLen {
		return ErrInvalidGrant
	}
	switch level {
	case LevelPlatform, LevelOrganization, LevelGroup:
	default:
		return ErrInvalidGrant
	}
	if cur, ok := b.grants[code]; !ok || level.BroaderThan(cur) {
		b.grants[code] = level
	}
	return nil
}

// Build mengembalikan salinan; Add berikutnya tidak memengaruhi hasil yang sudah dibangun.
func (b *EffectiveBuilder) Build() EffectivePermissions {
	g := make(map[string]DecisionLevel, len(b.grants))
	for c, l := range b.grants {
		g[c] = l
	}
	return EffectivePermissions{grants: g}
}

// EffectiveFromGrants membangun himpunan dari peta kode → level (mis. hasil decode cache) dengan
// validasi yang sama seperti Add. Galat pada satu entri menolak seluruhnya.
func EffectiveFromGrants(grants map[string]DecisionLevel) (EffectivePermissions, error) {
	b := NewEffectiveBuilder()
	for c, l := range grants {
		if err := b.Add(c, l); err != nil {
			return EffectivePermissions{}, err
		}
	}
	return b.Build(), nil
}
