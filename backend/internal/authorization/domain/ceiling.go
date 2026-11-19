package domain

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// ErrInvalidGrantTarget menandai GrantTarget yang bentuknya tidak sah: level selain platform,
// organization, atau group; organisasi/grup yang kosong atau UUID nol; atau kolom yang tidak
// boleh terisi pada levelnya. Level resource dan own tidak dapat menjadi target grant lewat API
// (engine tidak mengevaluasinya, docs/06 §5.2).
var ErrInvalidGrantTarget = errors.New("authorization: target grant tidak sah")

// ErrTargetOutsideScope menandai target yang berada di luar batas scope pemberi (docs/06 §5.3,
// "target_scope ⊆ admin's own scope boundary"): organisasi lain, atau level platform dari scope
// tenant.
var ErrTargetOutsideScope = errors.New("authorization: scope target di luar batas scope pemberi")

// ErrAuthorityCeiling menandai grant yang melebihi otoritas pemberi: ada permission yang tidak
// dimiliki pemberi pada level yang mencakup target (docs/06 §5.3).
var ErrAuthorityCeiling = errors.New("authorization: melebihi otoritas pemberi (authority ceiling)")

// MaxCeilingMissing membatasi jumlah kode yang dimuat CeilingError.Missing.
const MaxCeilingMissing = 50

// CeilingError adalah galat rinci untuk ErrAuthorityCeiling (errors.Is cocok). Missing memuat
// kode yang tidak dimiliki pemberi, terurut dan tanpa duplikat, paling banyak MaxCeilingMissing;
// Total adalah jumlah penuhnya. Kode berasal dari permintaan pemberi sendiri, tetapi pesan galat
// hanya memuat hitungan (tidak masuk log mentah); transport memutuskan apa yang diekspos.
type CeilingError struct {
	Missing []string
	Total   int
}

func (e *CeilingError) Error() string {
	return fmt.Sprintf("%s: %d permission tidak dimiliki pemberi", ErrAuthorityCeiling.Error(), e.Total)
}

// Is membuat errors.Is(err, ErrAuthorityCeiling) benar untuk *CeilingError.
func (e *CeilingError) Is(target error) bool { return target == ErrAuthorityCeiling }

// GrantTarget adalah scope tempat sebuah grant (permission pada role, atau role pada assignment)
// akan berlaku. Gunakan PlatformTarget, OrganizationTarget, atau GroupTarget; nilai nol tidak
// sah (Validate menolaknya) sehingga target yang tidak diisi gagal-tertutup.
type GrantTarget struct {
	Level          DecisionLevel
	OrganizationID *uuid.UUID
	GroupID        *uuid.UUID
}

// PlatformTarget adalah target tingkat platform (tanpa organisasi).
func PlatformTarget() GrantTarget {
	return GrantTarget{Level: LevelPlatform}
}

// OrganizationTarget adalah target seluruh organisasi org.
func OrganizationTarget(org uuid.UUID) GrantTarget {
	return GrantTarget{Level: LevelOrganization, OrganizationID: cloneID(&org)}
}

// GroupTarget adalah target grup group di dalam organisasi org. Bahwa grup itu benar milik org
// diverifikasi pemanggil terhadap database (domain tidak dapat membuktikannya).
func GroupTarget(org, group uuid.UUID) GrantTarget {
	return GrantTarget{Level: LevelGroup, OrganizationID: cloneID(&org), GroupID: cloneID(&group)}
}

func validID(p *uuid.UUID) bool { return p != nil && *p != uuid.Nil }

// Validate memeriksa bentuk target; galatnya selalu ErrInvalidGrantTarget.
func (t GrantTarget) Validate() error {
	switch t.Level {
	case LevelPlatform:
		if t.OrganizationID != nil || t.GroupID != nil {
			return ErrInvalidGrantTarget
		}
	case LevelOrganization:
		if !validID(t.OrganizationID) || t.GroupID != nil {
			return ErrInvalidGrantTarget
		}
	case LevelGroup:
		if !validID(t.OrganizationID) || !validID(t.GroupID) {
			return ErrInvalidGrantTarget
		}
	default:
		return ErrInvalidGrantTarget
	}
	return nil
}

// CheckCeiling menegakkan authority ceiling (docs/06 §5.3): pemberi (actor, dihitung untuk
// actorScope) hanya boleh memberi permission yang ia pegang sendiri, dan hanya di dalam batas
// scope-nya. Dipanggil di service, di dalam transaksi pemberi, bukan di UI.
//
// Semuanya gagal-tertutup, dengan urutan pemeriksaan ini. Target tidak sah memberi
// ErrInvalidGrantTarget dan scope pemberi tidak sah memberi ErrInvalidScope. Target di luar batas
// scope pemberi memberi ErrTargetOutsideScope, juga untuk codes kosong: tidak ada jalan pintas
// yang meloloskan target lintas-tenant. Kode kosong, berspasi di ujung, atau terlalu panjang
// memberi ErrInvalidGrant. Kode yang tidak tercakup memberi *CeilingError (errors.Is
// ErrAuthorityCeiling).
//
// Sebuah kode tercakup bila pemberi memegangnya pada level yang mencakup target: target platform
// butuh level platform; target organization butuh platform atau organization; target group
// butuh platform atau organization, atau level group HANYA bila grup itu adalah grup aktif
// terverifikasi pada actorScope (permission level group dihitung per grup aktif). Pemberi yang
// ingin memberi di grup lain harus memegang permission itu di level organisasi. Kode yang tidak
// dikenal, atau himpunan pemberi yang kosong, berarti tidak tercakup. codes tidak diubah.
func CheckCeiling(actor EffectivePermissions, actorScope ScopeContext, target GrantTarget, codes []string) error {
	if err := target.Validate(); err != nil {
		return err
	}
	if err := validateScope(actorScope); err != nil {
		return err
	}
	if !withinScope(actorScope, target) {
		return ErrTargetOutsideScope
	}

	seen := make(map[string]struct{}, len(codes))
	var missing []string
	for _, code := range codes {
		if code == "" || code != strings.TrimSpace(code) || len(code) > maxCodeLen {
			return ErrInvalidGrant
		}
		if _, dup := seen[code]; dup {
			continue
		}
		seen[code] = struct{}{}
		if !covers(actor, actorScope, target, code) {
			missing = append(missing, code)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	total := len(missing)
	if total > MaxCeilingMissing {
		missing = missing[:MaxCeilingMissing]
	}
	return &CeilingError{Missing: missing, Total: total}
}

// validateScope menolak ScopeContext yang dibangun tanpa NewScope dan melanggar invarian.
func validateScope(s ScopeContext) error {
	if s.OrganizationID != nil && *s.OrganizationID == uuid.Nil {
		return ErrInvalidScope
	}
	if s.ActiveGroupID != nil && (*s.ActiveGroupID == uuid.Nil || s.OrganizationID == nil) {
		return ErrInvalidScope
	}
	return nil
}

// withinScope melaporkan apakah target berada di dalam batas scope pemberi.
func withinScope(actor ScopeContext, t GrantTarget) bool {
	if t.Level == LevelPlatform {
		return actor.IsPlatform()
	}
	org, ok := actor.Tenant()
	return ok && t.OrganizationID != nil && org == *t.OrganizationID
}

// covers melaporkan apakah pemberi memegang code pada level yang mencakup target.
func covers(actor EffectivePermissions, scope ScopeContext, t GrantTarget, code string) bool {
	level, ok := actor.Level(code)
	if !ok {
		return false
	}
	switch t.Level {
	case LevelPlatform:
		return level == LevelPlatform
	case LevelOrganization:
		return level == LevelPlatform || level == LevelOrganization
	case LevelGroup:
		if level == LevelPlatform || level == LevelOrganization {
			return true
		}
		if level == LevelGroup {
			active, has := scope.Group()
			return has && t.GroupID != nil && active == *t.GroupID
		}
	}
	return false
}
