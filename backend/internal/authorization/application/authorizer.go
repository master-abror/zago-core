package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/authorization/cache"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/permission"
)

// ErrInvalidCheck menandai Check yang salah bentuk (aksi tidak sah, user kosong). Ini kesalahan
// pemrograman pemanggil, bukan keputusan: pemanggil tidak boleh menafsirkannya sebagai DENY biasa
// maupun ALLOW.
var ErrInvalidCheck = errors.New("authorization: check tidak sah")

// EffectiveLoader menghitung effective permissions dari sumber kebenaran (PostgreSQL).
type EffectiveLoader interface {
	Load(ctx context.Context, userID uuid.UUID, scope domain.ScopeContext) (domain.EffectivePermissions, error)
}

// EffectiveCache adalah cache berversi (paket cache). Lookup tidak pernah gagal: galat Redis
// berarti Lookup{} (Key kosong), dan Store pada Key kosong tidak melakukan apa pun.
type EffectiveCache interface {
	Lookup(ctx context.Context, userID uuid.UUID, scope domain.ScopeContext) cache.Lookup
	Store(ctx context.Context, l cache.Lookup, eff domain.EffectivePermissions)
}

// Authorizer memutuskan Check (docs/06 §9). Handler dan middleware hanya bergantung pada
// Can; tidak ada yang membaca role_assignments langsung.
type Authorizer struct {
	loader EffectiveLoader
	cache  EffectiveCache
}

// NewAuthorizer merakit Authorizer. cache boleh nil (tanpa cache); NILAI NIL HARUS BERUPA
// interface nil, bukan pointer nil yang dibungkus interface.
func NewAuthorizer(loader EffectiveLoader, c EffectiveCache) (*Authorizer, error) {
	if loader == nil {
		return nil, errors.New("authorization: loader effective permissions wajib")
	}
	return &Authorizer{loader: loader, cache: c}, nil
}

// Can memutuskan satu Check. Urutan (setiap langkah hanya dapat menolak, docs/06 §2):
//
//  1. Check salah bentuk atau scope melanggar invarian: galat (bukan keputusan).
//  2. Organisasi Identity harus sama dengan organisasi scope, dan organisasi sumber daya (bila ada)
//     juga: selain itu DENY scope_mismatch, sebelum menyentuh penyimpanan apa pun. Level platform
//     TIDAK mengecualikan ini: identitas platform masuk ke organisasi lewat switch-organization.
//  3. Effective permissions dari cache, atau dari PostgreSQL bila cache tidak ada/tidak tersedia.
//     Kegagalan PostgreSQL adalah galat, tidak pernah ALLOW.
//  4. Aksi harus ada di himpunan; izin level group pada sumber daya hanya berlaku bila sumber daya
//     itu berada di grup aktif.
//
// Scope resource dan own belum dievaluasi (ditolak, gagal tertutup): ADR-0022 bagian 3.
func (a *Authorizer) Can(ctx context.Context, check domain.Check) (domain.Decision, error) {
	if check.Identity.UserID == uuid.Nil {
		return domain.Decision{}, fmt.Errorf("%w: user kosong", ErrInvalidCheck)
	}
	if _, _, err := permission.Split(check.Action); err != nil {
		return domain.Decision{}, fmt.Errorf("%w: aksi %q", ErrInvalidCheck, check.Action)
	}
	scope, err := domain.NewScope(check.Scope.OrganizationID, check.Scope.ActiveGroupID)
	if err != nil {
		return domain.Decision{}, err
	}

	if !sameID(check.Identity.OrganizationID, scope.OrganizationID) {
		return deny(domain.ReasonScopeMismatch), nil
	}
	if r := check.Resource; r != nil && !sameID(r.OrganizationID, scope.OrganizationID) {
		return deny(domain.ReasonScopeMismatch), nil
	}

	eff, err := a.effective(ctx, check.Identity.UserID, scope)
	if err != nil {
		return domain.Decision{}, err
	}
	level, ok := eff.Level(check.Action)
	if !ok {
		return deny(domain.ReasonNoPermission), nil
	}
	if level == domain.LevelGroup && check.Resource != nil &&
		!sameID(check.Resource.GroupID, scope.ActiveGroupID) {
		return deny(domain.ReasonNoPermission), nil
	}
	return domain.Decision{Allowed: true, Reason: domain.ReasonGranted, Level: level}, nil
}

func (a *Authorizer) effective(ctx context.Context, userID uuid.UUID, scope domain.ScopeContext) (domain.EffectivePermissions, error) {
	var lookup cache.Lookup
	if a.cache != nil {
		lookup = a.cache.Lookup(ctx, userID, scope)
		if lookup.Hit {
			return lookup.Effective, nil
		}
	}
	eff, err := a.loader.Load(ctx, userID, scope)
	if err != nil {
		return domain.EffectivePermissions{}, err
	}
	if a.cache != nil {
		a.cache.Store(ctx, lookup, eff)
	}
	return eff, nil
}

func deny(reason string) domain.Decision { return domain.Decision{Reason: reason} }

// sameID membandingkan dua organisasi/grup opsional: keduanya nil, atau keduanya terisi dan sama.
func sameID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
