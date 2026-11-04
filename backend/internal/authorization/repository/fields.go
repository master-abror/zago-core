package repository

import (
	"context"
	"fmt"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel"
)

// Fields memuat definisi field permission dari katalog permission (ADR-0022 butir 41).
type Fields struct {
	db kernel.DBTX
}

// NewFields membuat Fields di atas db (pool runtime app_user).
func NewFields(db kernel.DBTX) *Fields { return &Fields{db: db} }

// fieldCodesSQL mengambil kode katalog berawalan "<tipe>.field." (parameter $1 sudah memuat
// awalan itu). starts_with, BUKAN LIKE: garis bawah pada nama tipe adalah wildcard LIKE sehingga
// tipe a_b akan ikut memuat axb. Tidak ada filter modul atau status: kode field milik modul
// nonaktif tetap membatasi (gagal-tertutup). permissions bukan tabel tenant. Hasil boleh superset
// (mis. kode milik tipe bersarang a.field.b untuk tipe a); domain.NewFieldRules menyaring ulang
// dan tidak bergantung pada urutan baris.
const fieldCodesSQL = `
SELECT code
  FROM permissions
 WHERE starts_with(code, $1)`

// FieldCodes memuat kode katalog yang mungkin mendefinisikan field terbatas milik resourceType.
// Galat berarti definisi tidak dapat dimuat; pemanggil tidak boleh menafsirkannya sebagai "tidak
// ada field terbatas" (domain.ErrInvalidFieldRules bila tipe tidak sah, tanpa menyentuh database).
func (f *Fields) FieldCodes(ctx context.Context, resourceType string) ([]string, error) {
	if _, err := domain.NewFieldRules(resourceType, nil); err != nil {
		return nil, err
	}
	rows, err := f.db.Query(ctx, fieldCodesSQL, resourceType+".field.")
	if err != nil {
		return nil, fmt.Errorf("authorization: kueri definisi field: %w", err)
	}
	defer rows.Close()

	var codes []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("authorization: baca kode field: %w", err)
		}
		codes = append(codes, code)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("authorization: iterasi kode field: %w", err)
	}
	return codes, nil
}
