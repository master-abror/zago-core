package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/permission"
	"github.com/master-abror/zago-core/backend/internal/policy"
)

// Policies memuat policy aktif untuk engine (ADR-0022 butir 30-31).
type Policies struct {
	db kernel.DBTX
}

// NewPolicies membuat Policies di atas db (pool runtime app_user).
func NewPolicies(db kernel.DBTX) *Policies { return &Policies{db: db} }

// policiesForActionSQL mengambil policy aktif yang actions-nya memuat kode persis atau
// "resource.*" (resource = semua segmen sebelum titik terakhir), milik organisasi $1 atau global
// (organization_id IS NULL). Dengan $1 NULL (scope platform) hanya policy global yang cocok.
// Policy nonaktif tidak pernah dimuat. Urutan stabil; engine tetap mengurutkan menurut kode.
const policiesForActionSQL = `
SELECT code, effect, expression, actions
  FROM policies
 WHERE status = 'active'
   AND (organization_id IS NULL OR organization_id = $1)
   AND actions && ARRAY[$2::text, $3::text]
 ORDER BY code, id`

// ForAction memuat policy aktif yang mungkin berlaku untuk action di organisasi organizationID
// (nil = scope platform). Galat berarti policy tidak dapat dimuat; pemanggil tidak boleh
// menafsirkannya sebagai "tidak ada policy".
func (p *Policies) ForAction(ctx context.Context, organizationID *uuid.UUID, action string) ([]policy.Policy, error) {
	resource, _, err := permission.Split(action)
	if err != nil {
		return nil, fmt.Errorf("authorization: aksi tidak sah untuk kueri policy: %w", err)
	}
	rows, err := p.db.Query(ctx, policiesForActionSQL, organizationID, action, resource+".*")
	if err != nil {
		return nil, fmt.Errorf("authorization: kueri policy: %w", err)
	}
	defer rows.Close()

	var out []policy.Policy
	for rows.Next() {
		var (
			pol    policy.Policy
			effect string
		)
		if err := rows.Scan(&pol.Code, &effect, &pol.Expression, &pol.Actions); err != nil {
			return nil, fmt.Errorf("authorization: baca policy: %w", err)
		}
		pol.Effect = policy.Effect(effect)
		out = append(out, pol)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("authorization: iterasi policy: %w", err)
	}
	return out, nil
}
