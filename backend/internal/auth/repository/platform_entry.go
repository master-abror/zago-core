package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/kernel"
)

// organizationIsActiveSQL: organisasi dapat dimasuki hanya bila `active` dan belum terhapus
// (sama dengan syarat organisasi pada HasActiveMembership). Ditulis tangan, bukan sqlc, karena
// hanya dipakai satu jalur; organizations adalah akar tenant sehingga difilter dengan id-nya.
const organizationIsActiveSQL = `
SELECT EXISTS (
    SELECT 1 FROM organizations
    WHERE id = $1::uuid AND status = 'active' AND deleted_at IS NULL
)`

// OrganizationIsActive melaporkan apakah organisasi ada, berstatus active, dan belum terhapus.
// Dipakai switch-organization identitas platform; ikut transaksi pada ctx bila ada.
func (s *Store) OrganizationIsActive(ctx context.Context, organizationID uuid.UUID) (bool, error) {
	var active bool
	if err := kernel.DBFrom(ctx, s.db).QueryRow(ctx, organizationIsActiveSQL, organizationID).Scan(&active); err != nil {
		return false, fmt.Errorf("auth: periksa organisasi aktif: %w", err)
	}
	return active, nil
}
