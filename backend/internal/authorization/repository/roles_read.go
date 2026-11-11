package repository

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel"
)

// errUnknownRoleFilter menandai filter status yang tidak dikenal. Pemanggil (handler) memvalidasi
// nilai klien lebih dulu, jadi galat ini berarti kesalahan pemrograman dan tidak pernah menjadi
// "semua role".
var errUnknownRoleFilter = errors.New("authorization: filter status role tidak dikenal")

// RoleReader membaca roles dan role_permissions untuk endpoint GET (ADR-0022 butir 63). Setiap
// kueri memfilter tenant: parameter organizationID adalah organisasi dari ScopeContext, dan nil
// (scope platform) hanya melihat role sistem. Role organisasi lain tidak pernah terbaca,
// sehingga tidak dapat dibedakan dari role yang tidak ada (domain.ErrRoleNotFound).
type RoleReader struct {
	db kernel.DBTX
}

// NewRoleReader membuat RoleReader di atas db (pool runtime app_user).
func NewRoleReader(db kernel.DBTX) *RoleReader { return &RoleReader{db: db} }

// Role yang terlihat: role sistem (organization_id NULL; CHECK ck_roles_boundary menjamin hanya
// role_type system yang NULL) dan role milik organisasi pemanggil. Dengan $1 NULL perbandingan
// kedua tidak pernah benar, jadi scope platform hanya melihat role sistem.
const roleListWhereSQL = `
 WHERE (r.organization_id IS NULL OR r.organization_id = $1)
   AND ($2::text = 'all' OR r.status = $2::text)`

const countRolesSQL = `SELECT count(*) FROM roles r` + roleListWhereSQL

const listRolesSQL = `
SELECT r.id, r.organization_id, r.group_id, r.name, r.slug, COALESCE(r.description, ''), r.role_type, r.status
  FROM roles r` + roleListWhereSQL + `
 ORDER BY r.created_at, r.id
 LIMIT $3 OFFSET $4`

// List mengembalikan satu halaman role yang terlihat bagi organizationID. Urutan tetap
// (created_at, id) supaya halaman tidak bergeser antar permintaan. Total dan halaman dibaca dengan
// dua kueri, jadi keduanya dapat berselisih bila role berubah di antaranya.
func (r *RoleReader) List(ctx context.Context, organizationID *uuid.UUID, q domain.RoleListQuery) (domain.RolePage, error) {
	status := q.Status
	if status == "" {
		status = domain.RoleFilterActive
	}
	if !domain.ValidRoleFilter(status) {
		return domain.RolePage{}, errUnknownRoleFilter
	}
	w := domain.ClampWindow(q.Window)
	db := kernel.DBFrom(ctx, r.db)

	var total int
	if err := db.QueryRow(ctx, countRolesSQL, organizationID, status).Scan(&total); err != nil {
		return domain.RolePage{}, fmt.Errorf("authorization: hitung role: %w", err)
	}
	rows, err := db.Query(ctx, listRolesSQL, organizationID, status, w.Limit, w.Offset)
	if err != nil {
		return domain.RolePage{}, fmt.Errorf("authorization: daftar role: %w", err)
	}
	defer rows.Close()
	items := []domain.RoleRecord{}
	for rows.Next() {
		var row domain.RoleRecord
		if err := rows.Scan(&row.ID, &row.OrganizationID, &row.GroupID, &row.Name, &row.Slug, &row.Description, &row.Type, &row.Status); err != nil {
			return domain.RolePage{}, fmt.Errorf("authorization: daftar role: %w", err)
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return domain.RolePage{}, fmt.Errorf("authorization: daftar role: %w", err)
	}
	return domain.RolePage{Items: items, Total: total}, nil
}

// Satu pernyataan membaca role beserta kode permission-nya, jadi keduanya konsisten. Filter tenant
// sama dengan daftar; p.code dicor ke text supaya COALESCE bertipe sama.
const roleGetSQL = `
SELECT r.id, r.organization_id, r.group_id, r.name, r.slug, COALESCE(r.description, ''), r.role_type, r.status,
       COALESCE(array_agg(p.code::text) FILTER (WHERE p.code IS NOT NULL), '{}'::text[])
  FROM roles r
  LEFT JOIN role_permissions rp ON rp.role_id = r.id
  LEFT JOIN permissions p ON p.id = rp.permission_id
 WHERE r.id = $1
   AND (r.organization_id IS NULL OR r.organization_id = $2)
 GROUP BY r.id`

// Get membaca satu role (aktif maupun terarsip) dan kode permission-nya, terurut menurut byte
// (diurutkan di Go, bukan ORDER BY, karena collation database berbeda dari urutan byte). Role yang
// tidak ada ATAU milik organisasi lain memberi domain.ErrRoleNotFound; keduanya tidak dibedakan.
func (r *RoleReader) Get(ctx context.Context, organizationID *uuid.UUID, roleID uuid.UUID) (domain.RoleRecord, []string, error) {
	var row domain.RoleRecord
	var codes []string
	err := kernel.DBFrom(ctx, r.db).QueryRow(ctx, roleGetSQL, roleID, organizationID).Scan(
		&row.ID, &row.OrganizationID, &row.GroupID, &row.Name, &row.Slug, &row.Description, &row.Type, &row.Status, &codes)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RoleRecord{}, nil, domain.ErrRoleNotFound
	}
	if err != nil {
		return domain.RoleRecord{}, nil, fmt.Errorf("authorization: baca role: %w", err)
	}
	if codes == nil {
		codes = []string{}
	}
	sort.Strings(codes)
	return row, codes, nil
}
