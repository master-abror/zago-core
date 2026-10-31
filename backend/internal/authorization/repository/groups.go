// Package repository memuat akses PostgreSQL milik Authorization Engine (pgx langsung, ADR-0022
// butir 8).
package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/kernel"
)

// Groups memverifikasi keanggotaan grup untuk resolusi scope (docs/06 §4.2).
type Groups struct {
	db kernel.DBTX
}

// NewGroups membuat Groups di atas db (pool runtime app_user).
func NewGroups(db kernel.DBTX) *Groups { return &Groups{db: db} }

// isActiveGroupMemberSQL mengikuti docs/06 §4.2 dan MENGETATKANNYA di tiga titik: organisasi
// diperiksa pada group_memberships maupun groups (tenant filter), keanggotaan organisasi user
// harus aktif (keanggotaan grup tidak berlaku bila keanggotaan organisasinya ditangguhkan atau
// dicabut), dan semua kolom status diperiksa pada baris yang sama dengan satu kueri (tanpa celah
// antar-kueri).
const isActiveGroupMemberSQL = `
SELECT EXISTS (
    SELECT 1
      FROM group_memberships gm
      JOIN groups g
        ON g.id = gm.group_id AND g.organization_id = gm.organization_id
      JOIN organization_memberships om
        ON om.organization_id = gm.organization_id AND om.user_id = gm.user_id
     WHERE gm.user_id         = $1
       AND gm.organization_id = $2
       AND gm.group_id        = $3
       AND gm.status          = 'active'
       AND om.status          = 'active'
       AND g.status           = 'active'
       AND g.deleted_at IS NULL
)`

// IsActiveGroupMember melaporkan apakah userID anggota aktif grup aktif groupID di organisasi
// organizationID. (false, nil) berarti terbukti bukan; galat berarti tidak bisa diverifikasi.
func (g *Groups) IsActiveGroupMember(ctx context.Context, userID, organizationID, groupID uuid.UUID) (bool, error) {
	var ok bool
	if err := g.db.QueryRow(ctx, isActiveGroupMemberSQL, userID, organizationID, groupID).Scan(&ok); err != nil {
		return false, fmt.Errorf("authorization: kueri keanggotaan grup: %w", err)
	}
	return ok, nil
}
