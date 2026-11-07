package repository

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Roles menulis tabel roles dan role_permissions (ADR-0022 butir 53-55). Repositori ini hanya
// berisi SQL: aturan (boundary, ceiling, role sistem tak berubah), transaksi, audit, dan
// invalidasi cache ada di application.RoleWriter. Setiap metode memakai transaksi pada context
// bila ada (kernel.DBFrom), jadi semuanya berjalan di dalam transaksi RoleWriter.
type Roles struct {
	db kernel.DBTX
}

// NewRoles membuat Roles di atas db (pool runtime app_user).
func NewRoles(db kernel.DBTX) *Roles { return &Roles{db: db} }

// LockOrganization mengambil kunci advisory organisasi pada transaksi context (lihat
// LockOrganization). Ada di sini supaya Roles memenuhi application.RoleStore.
func (r *Roles) LockOrganization(ctx context.Context, organizationID uuid.UUID) error {
	return LockOrganization(ctx, organizationID)
}

const insertRoleSQL = `
INSERT INTO roles (id, organization_id, group_id, name, slug, description, role_type, status)
VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, 'active')`

// Insert menyimpan role baru berstatus active. Slug yang sudah dipakai di batas yang sama memberi
// domain.ErrRoleSlugTaken.
func (r *Roles) Insert(ctx context.Context, row domain.RoleRecord) error {
	_, err := kernel.DBFrom(ctx, r.db).Exec(ctx, insertRoleSQL,
		row.ID, row.OrganizationID, row.GroupID, row.Name, row.Slug, row.Description, row.Type)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && strings.HasPrefix(pgErr.ConstraintName, "uq_roles_") {
			return domain.ErrRoleSlugTaken
		}
		return fmt.Errorf("authorization: simpan role: %w", err)
	}
	return nil
}

// roleForUpdateSQL memfilter tenant: baris sah hanya yang bukan milik organisasi lain. Role sistem
// (organization_id IS NULL) ikut terbaca supaya pemanggil dapat menolaknya dengan jelas
// (CheckRoleMutable); dengan $2 NULL (scope platform) hanya role sistem yang cocok.
const roleForUpdateSQL = `
SELECT id, organization_id, group_id, name, slug, COALESCE(description, ''), role_type, status
  FROM roles
 WHERE id = $1
   AND (organization_id IS NULL OR organization_id = $2)
   FOR UPDATE`

// ForUpdate membaca role dan mengunci barisnya sampai transaksi selesai. Role yang tidak ada atau
// milik organisasi lain memberi domain.ErrRoleNotFound (keduanya tidak dibedakan).
func (r *Roles) ForUpdate(ctx context.Context, roleID uuid.UUID, organizationID *uuid.UUID) (domain.RoleRecord, error) {
	var row domain.RoleRecord
	err := kernel.DBFrom(ctx, r.db).QueryRow(ctx, roleForUpdateSQL, roleID, organizationID).Scan(
		&row.ID, &row.OrganizationID, &row.GroupID, &row.Name, &row.Slug, &row.Description, &row.Type, &row.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RoleRecord{}, domain.ErrRoleNotFound
	}
	if err != nil {
		return domain.RoleRecord{}, fmt.Errorf("authorization: baca role: %w", err)
	}
	return row, nil
}

const groupActiveSQL = `
SELECT EXISTS (
    SELECT 1
      FROM groups
     WHERE id = $1
       AND organization_id = $2
       AND status = 'active'
       AND deleted_at IS NULL)`

// GroupActive melaporkan apakah grup ada, milik organisasi itu, dan aktif.
func (r *Roles) GroupActive(ctx context.Context, organizationID, groupID uuid.UUID) (bool, error) {
	var ok bool
	if err := kernel.DBFrom(ctx, r.db).QueryRow(ctx, groupActiveSQL, groupID, organizationID).Scan(&ok); err != nil {
		return false, fmt.Errorf("authorization: periksa grup: %w", err)
	}
	return ok, nil
}

// UpdateText mengganti nama dan deskripsi role (deskripsi kosong disimpan NULL).
func (r *Roles) UpdateText(ctx context.Context, roleID uuid.UUID, name, description string) error {
	_, err := kernel.DBFrom(ctx, r.db).Exec(ctx,
		`UPDATE roles SET name = $2, description = NULLIF($3, '') WHERE id = $1`, roleID, name, description)
	if err != nil {
		return fmt.Errorf("authorization: ubah role: %w", err)
	}
	return nil
}

// Archive mengarsipkan role aktif. Role yang sudah diarsipkan tidak diubah lagi.
func (r *Roles) Archive(ctx context.Context, roleID uuid.UUID) error {
	tag, err := kernel.DBFrom(ctx, r.db).Exec(ctx,
		`UPDATE roles SET status = 'archived' WHERE id = $1 AND status = 'active'`, roleID)
	if err != nil {
		return fmt.Errorf("authorization: arsipkan role: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrRoleArchived
	}
	return nil
}

const rolePermissionCodesSQL = `
SELECT p.code
  FROM role_permissions rp
  JOIN permissions p ON p.id = rp.permission_id
 WHERE rp.role_id = $1`

// PermissionCodes mengembalikan kode permission yang dipegang role, terurut menurut byte (diurutkan
// di Go, bukan ORDER BY, karena collation database berbeda dari urutan byte).
func (r *Roles) PermissionCodes(ctx context.Context, roleID uuid.UUID) ([]string, error) {
	rows, err := kernel.DBFrom(ctx, r.db).Query(ctx, rolePermissionCodesSQL, roleID)
	if err != nil {
		return nil, fmt.Errorf("authorization: baca permission role: %w", err)
	}
	defer rows.Close()
	codes := []string{}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("authorization: baca permission role: %w", err)
		}
		codes = append(codes, code)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("authorization: baca permission role: %w", err)
	}
	sort.Strings(codes)
	return codes, nil
}

// MissingPermissions mengembalikan kode (terurut) di antara codes yang TIDAK ada di tabel
// permissions. Daftar kosong berarti semuanya terdaftar.
func (r *Roles) MissingPermissions(ctx context.Context, codes []string) ([]string, error) {
	if len(codes) == 0 {
		return []string{}, nil
	}
	rows, err := kernel.DBFrom(ctx, r.db).Query(ctx,
		`SELECT code FROM permissions WHERE code = ANY($1::text[])`, codes)
	if err != nil {
		return nil, fmt.Errorf("authorization: periksa permission: %w", err)
	}
	defer rows.Close()
	found := make(map[string]struct{}, len(codes))
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("authorization: periksa permission: %w", err)
		}
		found[code] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("authorization: periksa permission: %w", err)
	}
	missing := []string{}
	for _, c := range codes {
		if _, ok := found[c]; !ok {
			missing = append(missing, c)
		}
	}
	sort.Strings(missing)
	return missing, nil
}

const addRolePermissionSQL = `
INSERT INTO role_permissions (id, role_id, permission_id, created_by)
SELECT $1::uuid, $2::uuid, p.id, $3::uuid
  FROM permissions p
 WHERE p.code = $4
ON CONFLICT (role_id, permission_id) DO NOTHING`

// AddPermissions menambahkan permission (berdasarkan kode) ke role atas nama grantedBy dan
// mengembalikan jumlah baris yang benar-benar ditambahkan. Kode yang sudah dipegang role dilewati.
func (r *Roles) AddPermissions(ctx context.Context, roleID uuid.UUID, codes []string, grantedBy uuid.UUID) (int, error) {
	db := kernel.DBFrom(ctx, r.db)
	added := 0
	for _, code := range codes {
		tag, err := db.Exec(ctx, addRolePermissionSQL, id.NewID(), roleID, grantedBy, code)
		if err != nil {
			return 0, fmt.Errorf("authorization: tambah permission role: %w", err)
		}
		added += int(tag.RowsAffected())
	}
	return added, nil
}

const removeRolePermissionsSQL = `
DELETE FROM role_permissions rp
 USING permissions p
 WHERE rp.role_id = $1
   AND p.id = rp.permission_id
   AND p.code = ANY($2::text[])`

// RemovePermissions mencabut permission (berdasarkan kode) dari role dan mengembalikan jumlah
// baris yang terhapus.
func (r *Roles) RemovePermissions(ctx context.Context, roleID uuid.UUID, codes []string) (int, error) {
	if len(codes) == 0 {
		return 0, nil
	}
	tag, err := kernel.DBFrom(ctx, r.db).Exec(ctx, removeRolePermissionsSQL, roleID, codes)
	if err != nil {
		return 0, fmt.Errorf("authorization: cabut permission role: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// GrantorEffective menghitung permission efektif pemberi di scope-nya LANGSUNG dari PostgreSQL pada
// transaksi context (bukan cache; ADR-0022 butir 49a). Dibatasi ke transaksi: di luar transaksi
// kembalinya ErrNoTransaction, supaya himpunan pemberi tidak pernah dihitung di luar kunci tulis.
func (r *Roles) GrantorEffective(ctx context.Context, userID uuid.UUID, scope domain.ScopeContext) (domain.EffectivePermissions, error) {
	tx, ok := kernel.TxFromContext(ctx)
	if !ok {
		return domain.EffectivePermissions{}, ErrNoTransaction
	}
	return NewEffective(tx).Load(ctx, userID, scope)
}
