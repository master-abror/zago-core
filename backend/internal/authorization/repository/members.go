package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/kernel"
)

// Members memverifikasi keanggotaan organisasi untuk simulator permission (ADR-0022 butir 69).
// Tipe baca terpisah dari Assignments supaya tipe tulis tidak dipinjam hanya untuk satu kueri;
// kuerinya sama persis (organizationMemberSQL), jadi definisi "anggota aktif" tidak bercabang.
type Members struct {
	db kernel.DBTX
}

// NewMembers membuat Members di atas db (pool runtime app_user).
func NewMembers(db kernel.DBTX) *Members { return &Members{db: db} }

// IsActiveMember melaporkan apakah userID adalah user aktif (belum dihapus) dan anggota AKTIF
// organisasi organizationID. Filter organisasi selalu ada: keanggotaan di organisasi lain tidak
// pernah dihitung. (false, nil) berarti terbukti bukan; galat berarti tidak dapat diverifikasi
// dan pemanggil tidak boleh menafsirkannya sebagai "bukan anggota".
func (m *Members) IsActiveMember(ctx context.Context, organizationID, userID uuid.UUID) (bool, error) {
	var ok bool
	if err := m.db.QueryRow(ctx, organizationMemberSQL, organizationID, userID).Scan(&ok); err != nil {
		return false, fmt.Errorf("authorization: periksa anggota organisasi: %w", err)
	}
	return ok, nil
}
