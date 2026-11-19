package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel"
)

// Assignments menulis tabel role_assignments dan memverifikasi subjek, grup, dan role yang
// dirujuknya (ADR-0022 butir 49 dan 54). Seperti Roles, repositori ini hanya berisi SQL: aturan
// (scope, subjek, masa berlaku, ceiling, super-admin terakhir), transaksi, audit, dan invalidasi
// cache ada di application.AssignmentWriter. Setiap metode memakai transaksi pada context bila
// ada (kernel.DBFrom).
type Assignments struct {
	db kernel.DBTX
}

// NewAssignments membuat Assignments di atas db (pool runtime app_user).
func NewAssignments(db kernel.DBTX) *Assignments { return &Assignments{db: db} }

// LockOrganization mengambil kunci advisory organisasi pada transaksi context (lihat
// LockOrganization). Ada di sini supaya Assignments memenuhi application.AssignmentStore.
func (a *Assignments) LockOrganization(ctx context.Context, organizationID uuid.UUID) error {
	return LockOrganization(ctx, organizationID)
}

// LockPlatform mengambil kunci advisory platform pada transaksi context (lihat LockPlatform).
func (a *Assignments) LockPlatform(ctx context.Context) error {
	return LockPlatform(ctx)
}

// roleReadSQL TIDAK memakai FOR UPDATE: role sistem bersifat global dan mengunci barisnya akan
// menyerialkan semua organisasi. Perubahan role ditulis di bawah kunci organisasi yang sama
// dengan penulis assignment (butir 54), jadi role non-sistem tidak berubah selama transaksi.
const roleReadSQL = `
SELECT id, organization_id, group_id, name, slug, COALESCE(description, ''), role_type, status
  FROM roles
 WHERE id = $1
   AND (organization_id IS NULL OR organization_id = $2::uuid)`

// Role membaca role yang boleh dirujuk assignment di organisasi itu: role sistem (global) atau
// role milik organisasi itu. Dengan organizationID nil (scope platform) hanya role sistem yang
// cocok. Role yang tidak ada atau milik organisasi lain memberi domain.ErrRoleNotFound.
func (a *Assignments) Role(ctx context.Context, roleID uuid.UUID, organizationID *uuid.UUID) (domain.RoleRecord, error) {
	var row domain.RoleRecord
	err := kernel.DBFrom(ctx, a.db).QueryRow(ctx, roleReadSQL, roleID, organizationID).Scan(
		&row.ID, &row.OrganizationID, &row.GroupID, &row.Name, &row.Slug, &row.Description, &row.Type, &row.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RoleRecord{}, domain.ErrRoleNotFound
	}
	if err != nil {
		return domain.RoleRecord{}, fmt.Errorf("authorization: baca role assignment: %w", err)
	}
	return row, nil
}

// PermissionCodes mengembalikan kode permission yang dipegang role, terurut (lihat
// Roles.PermissionCodes).
func (a *Assignments) PermissionCodes(ctx context.Context, roleID uuid.UUID) ([]string, error) {
	return NewRoles(a.db).PermissionCodes(ctx, roleID)
}

// GroupActive melaporkan apakah grup ada, milik organisasi itu, dan aktif (lihat
// Roles.GroupActive).
func (a *Assignments) GroupActive(ctx context.Context, organizationID, groupID uuid.UUID) (bool, error) {
	return NewRoles(a.db).GroupActive(ctx, organizationID, groupID)
}

// GrantorEffective menghitung permission efektif pemberi langsung dari PostgreSQL pada transaksi
// context (lihat Roles.GrantorEffective); di luar transaksi hasilnya ErrNoTransaction.
func (a *Assignments) GrantorEffective(ctx context.Context, userID uuid.UUID, scope domain.ScopeContext) (domain.EffectivePermissions, error) {
	return NewRoles(a.db).GrantorEffective(ctx, userID, scope)
}

const userActiveSQL = `
SELECT EXISTS (
    SELECT 1
      FROM users
     WHERE id = $1
       AND status = 'active'
       AND deleted_at IS NULL)`

// UserActive melaporkan apakah user ada dan aktif (status active, belum dihapus). Dipakai untuk
// subjek user pada scope platform.
func (a *Assignments) UserActive(ctx context.Context, userID uuid.UUID) (bool, error) {
	var ok bool
	if err := kernel.DBFrom(ctx, a.db).QueryRow(ctx, userActiveSQL, userID).Scan(&ok); err != nil {
		return false, fmt.Errorf("authorization: periksa user: %w", err)
	}
	return ok, nil
}

const organizationMemberSQL = `
SELECT EXISTS (
    SELECT 1
      FROM organization_memberships om
      JOIN users u ON u.id = om.user_id
     WHERE om.organization_id = $1
       AND om.user_id = $2
       AND om.status = 'active'
       AND u.status = 'active'
       AND u.deleted_at IS NULL)`

// OrganizationMember melaporkan apakah user aktif dan beranggota AKTIF organisasi itu. Dipakai
// untuk subjek user pada scope organisasi dan grup (Effective mensyaratkan keanggotaan aktif).
func (a *Assignments) OrganizationMember(ctx context.Context, organizationID, userID uuid.UUID) (bool, error) {
	var ok bool
	if err := kernel.DBFrom(ctx, a.db).QueryRow(ctx, organizationMemberSQL, organizationID, userID).Scan(&ok); err != nil {
		return false, fmt.Errorf("authorization: periksa anggota organisasi: %w", err)
	}
	return ok, nil
}

const organizationActiveSQL = `
SELECT EXISTS (
    SELECT 1
      FROM organizations
     WHERE id = $1
       AND status = 'active'
       AND deleted_at IS NULL)`

// OrganizationActive melaporkan apakah organisasi ada dan aktif. Dipakai untuk subjek
// organization.
func (a *Assignments) OrganizationActive(ctx context.Context, organizationID uuid.UUID) (bool, error) {
	var ok bool
	if err := kernel.DBFrom(ctx, a.db).QueryRow(ctx, organizationActiveSQL, organizationID).Scan(&ok); err != nil {
		return false, fmt.Errorf("authorization: periksa organisasi: %w", err)
	}
	return ok, nil
}

// activeDuplicateSQL menyamakan dua bentuk scope organisasi (scope_id NULL dan scope_id =
// organization_id) yang tidak disamakan indeks unik uq_role_assignments_active. Baris yang
// belum dicabut tetapi sudah kedaluwarsa tetap dihitung: indeks unik juga menghitungnya, jadi
// assignment kedaluwarsa harus dicabut dulu sebelum diberikan ulang.
const activeDuplicateSQL = `
SELECT EXISTS (
    SELECT 1
      FROM role_assignments
     WHERE role_id = $1
       AND subject_type = $2::text
       AND subject_id = $3
       AND scope_type = $4::text
       AND organization_id IS NOT DISTINCT FROM $5::uuid
       AND revoked_at IS NULL
       AND (
               ($4::text = 'organization' AND (scope_id IS NULL OR scope_id = $5::uuid))
            OR ($4::text <> 'organization' AND scope_id IS NOT DISTINCT FROM $6::uuid)))`

// ActiveDuplicate melaporkan apakah sudah ada assignment yang belum dicabut untuk (role, subjek,
// scope) yang sama, mencakup kedua bentuk scope organisasi. Dipanggil di bawah kunci advisory;
// indeks unik tetap menjadi pagar terakhir (Insert memetakannya ke domain.ErrAssignmentExists).
func (a *Assignments) ActiveDuplicate(ctx context.Context, roleID uuid.UUID, subjectType string, subjectID uuid.UUID, scope domain.GrantTarget) (bool, error) {
	scopeType, org, scopeID, err := domain.ScopeColumns(scope)
	if err != nil {
		return false, err
	}
	var exists bool
	err = kernel.DBFrom(ctx, a.db).QueryRow(ctx, activeDuplicateSQL,
		roleID, subjectType, subjectID, scopeType, org, scopeID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("authorization: periksa assignment ganda: %w", err)
	}
	return exists, nil
}

const insertAssignmentSQL = `
INSERT INTO role_assignments
    (id, organization_id, role_id, subject_type, subject_id, scope_type, scope_id, assigned_by, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING created_at`

// Insert menyimpan assignment baru dalam bentuk kanonik (domain.ScopeColumns) dan mengembalikan
// record dengan CreatedAt dari database. Assignment aktif yang sama memberi
// domain.ErrAssignmentExists (pagar terakhir bila pemeriksaan ActiveDuplicate terlewat).
func (a *Assignments) Insert(ctx context.Context, rec domain.AssignmentRecord) (domain.AssignmentRecord, error) {
	scopeType, org, scopeID, err := domain.ScopeColumns(rec.Scope)
	if err != nil {
		return domain.AssignmentRecord{}, err
	}
	err = kernel.DBFrom(ctx, a.db).QueryRow(ctx, insertAssignmentSQL,
		rec.ID, org, rec.RoleID, rec.SubjectType, rec.SubjectID, scopeType, scopeID, rec.AssignedBy, rec.ExpiresAt,
	).Scan(&rec.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "uq_role_assignments_active" {
			return domain.AssignmentRecord{}, domain.ErrAssignmentExists
		}
		return domain.AssignmentRecord{}, fmt.Errorf("authorization: simpan assignment: %w", err)
	}
	return rec, nil
}

// assignmentForUpdateSQL memfilter tenant: organization_id harus sama dengan organisasi pemanggil,
// dan dengan $2 NULL (scope platform) hanya assignment platform yang cocok (IS NOT DISTINCT
// FROM). Assignment organisasi lain tidak pernah terbaca.
const assignmentForUpdateSQL = `
SELECT id, role_id, subject_type, subject_id, scope_type, organization_id, scope_id,
       assigned_by, expires_at, revoked_at, created_at
  FROM role_assignments
 WHERE id = $1
   AND organization_id IS NOT DISTINCT FROM $2::uuid
   FOR UPDATE`

// ForUpdate membaca assignment dan mengunci barisnya sampai transaksi selesai. Assignment yang
// tidak ada atau milik organisasi lain (atau, dengan organizationID nil, bukan assignment
// platform) memberi domain.ErrAssignmentNotFound; keadaan-keadaan itu tidak dibedakan. Baris
// dengan scope yang tidak dipahami jalur tulis (resource, own) gagal-tertutup sebagai galat.
func (a *Assignments) ForUpdate(ctx context.Context, assignmentID uuid.UUID, organizationID *uuid.UUID) (domain.AssignmentRecord, error) {
	var (
		rec       domain.AssignmentRecord
		scopeType string
		org       *uuid.UUID
		scopeID   *uuid.UUID
	)
	err := kernel.DBFrom(ctx, a.db).QueryRow(ctx, assignmentForUpdateSQL, assignmentID, organizationID).Scan(
		&rec.ID, &rec.RoleID, &rec.SubjectType, &rec.SubjectID, &scopeType, &org, &scopeID,
		&rec.AssignedBy, &rec.ExpiresAt, &rec.RevokedAt, &rec.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AssignmentRecord{}, domain.ErrAssignmentNotFound
	}
	if err != nil {
		return domain.AssignmentRecord{}, fmt.Errorf("authorization: baca assignment: %w", err)
	}
	rec.Scope, err = domain.TargetFromColumns(scopeType, org, scopeID)
	if err != nil {
		return domain.AssignmentRecord{}, fmt.Errorf("authorization: assignment %s memakai scope %q yang tidak dapat diubah lewat API: %w", rec.ID, scopeType, err)
	}
	return rec, nil
}

// Revoke mencabut assignment yang belum dicabut (revoked_at = now()). Assignment yang sudah
// dicabut memberi domain.ErrAssignmentRevoked dan tidak diubah lagi.
func (a *Assignments) Revoke(ctx context.Context, assignmentID uuid.UUID) error {
	tag, err := kernel.DBFrom(ctx, a.db).Exec(ctx,
		`UPDATE role_assignments SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, assignmentID)
	if err != nil {
		return fmt.Errorf("authorization: cabut assignment: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrAssignmentRevoked
	}
	return nil
}

const activeSuperAdminsSQL = `
SELECT count(*)
  FROM role_assignments ra
  JOIN roles r ON r.id = ra.role_id
  JOIN users u ON u.id = ra.subject_id
 WHERE ra.scope_type = 'platform'
   AND ra.organization_id IS NULL
   AND ra.subject_type = 'user'
   AND ra.revoked_at IS NULL
   AND (ra.expires_at IS NULL OR ra.expires_at > now())
   AND r.role_type = 'system'
   AND r.slug = $1
   AND r.status = 'active'
   AND u.status = 'active'
   AND u.deleted_at IS NULL`

// ActiveSuperAdmins menghitung assignment super-admin platform yang aktif DAN dapat dipakai:
// belum dicabut, belum kedaluwarsa, role sistem aktif, user aktif (ADR-0022 butir 49c). Wajib
// dipanggil di bawah LockPlatform pada transaksi context: di luar transaksi hasilnya
// ErrNoTransaction, karena hitungan di luar kunci tidak melindungi apa pun.
func (a *Assignments) ActiveSuperAdmins(ctx context.Context) (int, error) {
	tx, ok := kernel.TxFromContext(ctx)
	if !ok {
		return 0, ErrNoTransaction
	}
	var n int
	if err := tx.QueryRow(ctx, activeSuperAdminsSQL, domain.RoleSlugSuperAdmin).Scan(&n); err != nil {
		return 0, fmt.Errorf("authorization: hitung super-admin aktif: %w", err)
	}
	return n, nil
}
