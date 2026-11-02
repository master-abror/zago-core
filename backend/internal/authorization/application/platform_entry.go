package application

import (
	"context"
	"errors"

	"github.com/google/uuid"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/permission"
)

// Decider adalah bagian engine yang dibutuhkan PlatformEntry; *Authorizer memenuhinya.
type Decider interface {
	Can(ctx context.Context, check domain.Check) (domain.Decision, error)
}

// PlatformEntry memenuhi auth/service.PlatformEntryChecker (ADR-0022 bagian 2): identitas platform
// boleh memasuki organisasi bila ENGINE memutuskan organization.read pada scope platform. Tidak ada
// jalur samping: bukan peran tertentu, bukan nama, bukan "tidak punya keanggotaan".
type PlatformEntry struct {
	authz Decider
}

// NewPlatformEntry merakit PlatformEntry di atas engine.
func NewPlatformEntry(d Decider) (*PlatformEntry, error) {
	if d == nil {
		return nil, errors.New("authorization: Decider wajib")
	}
	return &PlatformEntry{authz: d}, nil
}

// CanEnterOrganizations memutuskan lewat Authorizer.Can dengan Identity tanpa organisasi dan scope
// platform. Organisasi sesi pengguna (bila ia sudah berada di dalam suatu organisasi) sengaja tidak
// dipakai: izin platform tidak bergantung padanya, dan Can akan menolak scope platform bagi
// Identity ber-organisasi (scope_mismatch). Hanya keputusan ber-level platform yang dihitung:
// izin tingkat organisasi atau grup tidak pernah menjadi tiket masuk ke organisasi lain. Galat
// engine dikembalikan apa adanya (bukan false): pemanggil membedakan "ditolak" dari "tidak diketahui".
func (p *PlatformEntry) CanEnterOrganizations(ctx context.Context, userID uuid.UUID) (bool, error) {
	dec, err := p.authz.Can(ctx, domain.Check{
		Identity: authdomain.Identity{UserID: userID},
		Scope:    domain.ScopeContext{},
		Action:   permission.OrganizationRead,
	})
	if err != nil {
		return false, err
	}
	return dec.Allowed && dec.Level == domain.LevelPlatform, nil
}
