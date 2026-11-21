package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/kernel"
)

// revokeUserInOrganizationSQL mencabut setiap assignment aktif yang subjeknya user itu dan yang
// terikat pada organisasi itu. Filter organization_id adalah pagar tenant: assignment organisasi
// lain dan assignment platform (organization_id NULL) tidak pernah cocok. Hanya subject_type
// 'user' yang dicabut; assignment ber-subjek grup atau organisasi bukan milik keanggotaan user.
// Baris yang sudah dicabut tidak disentuh sehingga revoked_at aslinya tetap.
const revokeUserInOrganizationSQL = `
UPDATE role_assignments
   SET revoked_at = now()
 WHERE organization_id = $1
   AND subject_type = 'user'
   AND subject_id = $2
   AND revoked_at IS NULL`

// RevokeUserInOrganization mencabut semua assignment aktif milik user di organisasi itu dan
// mengembalikan jumlah baris yang dicabut (ADR-0023 butir 15). Cakupannya mengikuti kolom
// organization_id, jadi meliputi scope organisasi (kedua bentuk kanonik), grup, serta resource
// dan own yang terikat organisasi; scope resource dan own hari ini tidak dievaluasi mesin tetapi
// tetap dicabut supaya tidak hidup lagi ketika keanggotaan diaktifkan kembali. Assignment
// platform dan assignment organisasi lain tidak tersentuh.
//
// Mencabut hanya mengurangi hak sehingga tidak butuh pemeriksaan ceiling. Pemanggil WAJIB
// memegang kunci advisory organisasi (LockOrganization) sebagai langkah pertama transaksi yang
// sama (ADR-0022 butir 54); di luar transaksi hasilnya ErrNoTransaction, karena pencabutan di
// luar kunci bisa berpacu dengan penulis assignment lain.
func (a *Assignments) RevokeUserInOrganization(ctx context.Context, organizationID, userID uuid.UUID) (int, error) {
	if organizationID == uuid.Nil || userID == uuid.Nil {
		return 0, errors.New("authorization: cabut assignment user membutuhkan organisasi dan user")
	}
	tx, ok := kernel.TxFromContext(ctx)
	if !ok {
		return 0, ErrNoTransaction
	}
	tag, err := tx.Exec(ctx, revokeUserInOrganizationSQL, organizationID, userID)
	if err != nil {
		return 0, fmt.Errorf("authorization: cabut assignment user di organisasi: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
