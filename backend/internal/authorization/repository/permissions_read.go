package repository

import (
	"context"
	"fmt"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel"
)

// PermissionReader membaca katalog permission untuk GET /permissions (ADR-0022 butir 63). Katalog
// bersifat global (tabel permissions tidak punya organization_id), jadi tidak ada filter tenant;
// aksesnya dijaga Authorize(permission.read) di endpoint. Permission milik modul yang dinonaktifkan
// belum disaring (docs/06 §14, menunggu M07).
type PermissionReader struct {
	db kernel.DBTX
}

// NewPermissionReader membuat PermissionReader di atas db (pool runtime app_user).
func NewPermissionReader(db kernel.DBTX) *PermissionReader { return &PermissionReader{db: db} }

// Urutan ORDER BY p.code COLLATE "C" = urutan byte, deterministik di semua collation database.
const listPermissionsSQL = `
SELECT p.code, p.resource, p.action, COALESCE(p.description, ''), COALESCE(m.code, ''), p.is_system
  FROM permissions p
  LEFT JOIN modules m ON m.id = p.module_id
 ORDER BY p.code COLLATE "C"
 LIMIT $1 OFFSET $2`

// List mengembalikan satu halaman katalog permission terurut menurut kode.
func (r *PermissionReader) List(ctx context.Context, window domain.Window) (domain.PermissionPage, error) {
	w := domain.ClampWindow(window)
	db := kernel.DBFrom(ctx, r.db)

	var total int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM permissions`).Scan(&total); err != nil {
		return domain.PermissionPage{}, fmt.Errorf("authorization: hitung permission: %w", err)
	}
	rows, err := db.Query(ctx, listPermissionsSQL, w.Limit, w.Offset)
	if err != nil {
		return domain.PermissionPage{}, fmt.Errorf("authorization: daftar permission: %w", err)
	}
	defer rows.Close()
	items := []domain.PermissionRecord{}
	for rows.Next() {
		var row domain.PermissionRecord
		if err := rows.Scan(&row.Code, &row.Resource, &row.Action, &row.Description, &row.Module, &row.System); err != nil {
			return domain.PermissionPage{}, fmt.Errorf("authorization: daftar permission: %w", err)
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return domain.PermissionPage{}, fmt.Errorf("authorization: daftar permission: %w", err)
	}
	return domain.PermissionPage{Items: items, Total: total}, nil
}
