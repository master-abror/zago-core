package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/authorization/cache"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/permission"
	"github.com/master-abror/zago-core/backend/internal/policy"
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

// PolicySource memuat policy aktif yang mungkin berlaku untuk satu aksi: milik organisasi
// organizationID ditambah policy global; nil berarti scope platform (hanya policy global). Hasil
// boleh superset: engine menyaring ulang berdasarkan actions. Galat berarti policy tidak dapat
// dimuat, dan Can lalu mengembalikan galat (bukan izin).
type PolicySource interface {
	ForAction(ctx context.Context, organizationID *uuid.UUID, action string) ([]policy.Policy, error)
}

// Authorizer memutuskan Check (docs/06 §9). Handler dan middleware hanya bergantung pada
// Can; tidak ada yang membaca role_assignments langsung.
type Authorizer struct {
	loader   EffectiveLoader
	cache    EffectiveCache
	policies PolicySource
}

// NewAuthorizer merakit Authorizer. cache boleh nil (tanpa cache); NILAI NIL HARUS BERUPA
// interface nil, bukan pointer nil yang dibungkus interface.
func NewAuthorizer(loader EffectiveLoader, c EffectiveCache) (*Authorizer, error) {
	if loader == nil {
		return nil, errors.New("authorization: loader effective permissions wajib")
	}
	return &Authorizer{loader: loader, cache: c}, nil
}

// WithPolicies mengembalikan salinan Authorizer yang menerapkan policy (ADR-0022 butir 31-32)
// SESUDAH permission terbukti. src nil berarti tanpa policy; NILAI NIL HARUS BERUPA interface nil,
// bukan pointer nil yang dibungkus interface. Perakitan produksi WAJIB memanggil ini: tanpa itu
// policy tidak berlaku sama sekali.
func (a *Authorizer) WithPolicies(src PolicySource) *Authorizer {
	cp := *a
	cp.policies = src
	return &cp
}

// PoliciesEnabled melaporkan apakah policy dirakit. Hanya untuk tes perakitan: lupa memanggil
// WithPolicies di produksi mematikan semua policy tanpa suara, jadi perakitan harus bisa diuji.
func (a *Authorizer) PoliciesEnabled() bool {
	return a.policies != nil
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
//  5. Policy (bila dirakit) hanya dapat menolak: deny true atau allow false menjadi DENY
//     policy_denied; galat evaluasi menjadi DENY policy_evaluation_failed. Tanpa Resource,
//     policy yang butuh sumber daya ditunda dan dilaporkan di Decision.DeferredPolicies.
//     Galat memuat policy adalah galat (bukan izin). Policy hanya dimuat untuk check yang
//     permission-nya sudah lolos.
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
	return a.applyPolicies(ctx, check, scope, level)
}

// applyPolicies memuat dan mengevaluasi policy untuk check yang permission-nya sudah lolos.
func (a *Authorizer) applyPolicies(ctx context.Context, check domain.Check, scope domain.ScopeContext, level domain.DecisionLevel) (domain.Decision, error) {
	granted := domain.Decision{Allowed: true, Reason: domain.ReasonGranted, Level: level}
	if a.policies == nil {
		return granted, nil
	}
	pols, err := a.policies.ForAction(ctx, scope.OrganizationID, check.Action)
	if err != nil {
		return domain.Decision{}, fmt.Errorf("authorization: memuat policy: %w", err)
	}
	out := policy.Evaluate(pols, check.Action, policyEnv(check, scope))
	switch out.Verdict {
	case policy.VerdictPermit:
		granted.DeferredPolicies = out.Deferred
		return granted, nil
	case policy.VerdictDenied:
		return domain.Decision{Reason: domain.ReasonPolicyDenied, PolicyCode: out.Code}, nil
	default:
		return domain.Decision{Reason: domain.ReasonPolicyEvaluationFailed, PolicyCode: out.Code}, nil
	}
}

// policyEnv membentuk Env ekspresi policy. Akar resource hanya ada bila Check.Resource ada. Atribut
// inti (type, id, organization_id, owner_id, group_id) berasal dari ResourceRef dan MENIMPA kunci
// bernama sama di Attrs, sehingga modul tidak dapat memalsukan pemilik atau organisasi lewat Attrs.
func policyEnv(check domain.Check, scope domain.ScopeContext) policy.MapEnv {
	env := policy.MapEnv{}
	env[policy.RootCurrentUser] = map[string]any{"id": check.Identity.UserID.String(), "organization_id": optionalID(check.Identity.OrganizationID)}
	env[policy.RootScope] = map[string]any{"organization_id": optionalID(scope.OrganizationID), "active_group_id": optionalID(scope.ActiveGroupID)}
	if r := check.Resource; r != nil {
		res := make(map[string]any, len(r.Attrs)+5)
		for k, v := range r.Attrs {
			res[k] = v
		}
		res["type"] = r.Type
		res["id"] = r.ID.String()
		res["organization_id"] = optionalID(r.OrganizationID)
		res["owner_id"] = optionalID(r.OwnerID)
		res["group_id"] = optionalID(r.GroupID)
		env[policy.RootResource] = res
	}
	return env
}

// optionalID mengubah UUID opsional menjadi string, atau nil (null di ekspresi) bila tidak ada.
func optionalID(p *uuid.UUID) any {
	if p == nil {
		return nil
	}
	return p.String()
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
