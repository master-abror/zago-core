package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
)

// FieldDefinitions memuat kode katalog yang mungkin mendefinisikan field terbatas milik satu tipe
// sumber daya (superset boleh; domain.NewFieldRules menyaring ulang). Galat berarti definisi tidak
// dapat dimuat, dan FieldGuard lalu mengembalikan galat (bukan "tanpa pembatasan").
type FieldDefinitions interface {
	FieldCodes(ctx context.Context, resourceType string) ([]string, error)
}

// FieldGuard menapis dan memeriksa field per Identity dan scope (docs/06 §7, ADR-0022 butir 41-43).
// Ia BUKAN gerbang izin resource: izin resource tetap lewat Authorize/Can. Himpunan izin diambil
// lewat Authorizer yang sama dengan Can (cache berversi, lalu PostgreSQL).
type FieldGuard struct {
	// authorizer menyediakan himpunan izin yang sama dengan yang dipakai Can.
	authorizer *Authorizer

	// defs memuat definisi field terbatas dari katalog.
	defs FieldDefinitions
}

// NewFieldGuard merakit FieldGuard. NILAI NIL HARUS BERUPA interface nil, bukan pointer nil yang
// dibungkus interface.
func NewFieldGuard(authorizer *Authorizer, defs FieldDefinitions) (*FieldGuard, error) {
	if authorizer == nil {
		return nil, errors.New("authorization: authorizer wajib untuk FieldGuard")
	}
	if defs == nil {
		return nil, errors.New("authorization: sumber definisi field wajib untuk FieldGuard")
	}
	return &FieldGuard{authorizer: authorizer, defs: defs}, nil
}

// For membuat FieldFilter untuk (identity, scope, resourceType); gunakan ulang untuk banyak objek
// satu tipe dalam satu request. Urutan (setiap kegagalan adalah galat, tidak pernah filter tanpa
// pembatasan):
//
//  1. Identity.UserID tidak kosong (ErrInvalidCheck) dan scope sah (domain.ErrInvalidScope).
//  2. Organisasi Identity sama dengan organisasi scope (domain.ErrFieldScopeMismatch), seperti
//     langkah 2 Can.
//  3. Tipe sah (domain.ErrInvalidFieldRules), sebelum menyentuh penyimpanan apa pun.
//  4. Definisi dimuat dan aturan dibangun (galat pemuatan atau domain.ErrInvalidFieldCode).
//  5. Tanpa field terbatas, himpunan izin TIDAK dimuat: tidak ada yang ditapis. Selain itu
//     himpunan izin dimuat lewat jalur Can (galat penyimpanan adalah galat).
func (g *FieldGuard) For(ctx context.Context, identity authdomain.Identity, scope domain.ScopeContext, resourceType string) (domain.FieldFilter, error) {
	if identity.UserID == uuid.Nil {
		return domain.FieldFilter{}, fmt.Errorf("%w: user kosong", ErrInvalidCheck)
	}
	valid, err := domain.NewScope(scope.OrganizationID, scope.ActiveGroupID)
	if err != nil {
		return domain.FieldFilter{}, err
	}
	if !sameID(identity.OrganizationID, valid.OrganizationID) {
		return domain.FieldFilter{}, domain.ErrFieldScopeMismatch
	}
	if _, err := domain.NewFieldRules(resourceType, nil); err != nil {
		return domain.FieldFilter{}, err
	}

	codes, err := g.defs.FieldCodes(ctx, resourceType)
	if err != nil {
		return domain.FieldFilter{}, fmt.Errorf("authorization: muat definisi field %q: %w", resourceType, err)
	}
	rules, err := domain.NewFieldRules(resourceType, codes)
	if err != nil {
		return domain.FieldFilter{}, err
	}
	if !rules.HasRestrictions() {
		return domain.NewFieldFilter(rules, domain.EffectivePermissions{}, valid)
	}

	eff, err := g.authorizer.effective(ctx, identity.UserID, valid)
	if err != nil {
		return domain.FieldFilter{}, err
	}
	return domain.NewFieldFilter(rules, eff, valid)
}

// Filter menapis satu objek (lihat FieldFilter.Apply). Untuk banyak objek satu tipe, panggil For
// sekali lalu Apply per objek.
func (g *FieldGuard) Filter(ctx context.Context, identity authdomain.Identity, scope domain.ScopeContext, ref domain.ResourceRef, obj map[string]any) (map[string]any, error) {
	f, err := g.For(ctx, identity, scope, ref.Type)
	if err != nil {
		return nil, err
	}
	return f.Apply(ref, obj)
}

// CheckWrite memeriksa field yang hendak ditulis (lihat FieldFilter.CheckWrite).
func (g *FieldGuard) CheckWrite(ctx context.Context, identity authdomain.Identity, scope domain.ScopeContext, ref domain.ResourceRef, obj map[string]any) error {
	f, err := g.For(ctx, identity, scope, ref.Type)
	if err != nil {
		return err
	}
	return f.CheckWrite(ref, obj)
}
