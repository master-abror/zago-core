package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel"
)

// errUnknownAssignmentFilter menandai filter status yang tidak dikenal. Handler memvalidasi nilai
// klien lebih dulu, jadi galat ini berarti kesalahan pemrograman dan tidak pernah menjadi
// "semua assignment".
var errUnknownAssignmentFilter = errors.New("authorization: filter status assignment tidak dikenal")

// AssignmentReader membaca role_assignments untuk GET /role-assignments (ADR-0022 butir 66).
// Berbeda dari role, TIDAK ada jalur lain yang melindungi baca assignment, jadi setiap kueri
// memfilter organisasi sendiri: parameter organizationID adalah organisasi dari ScopeContext;
// tenant hanya melihat assignment organisasinya dan nil (scope platform) hanya assignment platform
// (organization_id IS NULL). Assignment organisasi lain tidak pernah terbaca.
type AssignmentReader struct {
	db kernel.DBTX
}

// NewAssignmentReader membuat AssignmentReader di atas db (pool runtime app_user).
func NewAssignmentReader(db kernel.DBTX) *AssignmentReader { return &AssignmentReader{db: db} }

// Filter tenant: IS NOT DISTINCT FROM membuat $1 NULL hanya cocok dengan assignment platform.
// Hanya scope platform, organisasi, dan grup yang terlihat: scope resource dan own tidak pernah
// dibuat lewat API (TargetFromColumns menolaknya) dan belum punya pembuat; satu baris seperti itu
// tidak boleh membuat seluruh daftar gagal. Ringkasan role di-JOIN hanya bila role itu sistem
// atau milik organisasi assignment itu sendiri, jadi nama role organisasi lain tidak dapat
// bocor lewat baris yang ganjil. Status dihitung terhadap $5 (waktu acuan dari pemanggil),
// sama dengan AssignmentRecord.State: kedaluwarsa bila expires_at <= $5.
const listAssignmentsSQL = `
SELECT ra.id, ra.role_id, ra.subject_type, ra.subject_id, ra.scope_type, ra.organization_id, ra.scope_id,
       ra.assigned_by, ra.expires_at, ra.revoked_at, ra.created_at,
       COALESCE(r.name, ''), COALESCE(r.slug, ''), COALESCE(r.status, '')
  FROM role_assignments ra
  LEFT JOIN roles r
         ON r.id = ra.role_id
        AND (r.organization_id IS NULL OR r.organization_id = ra.organization_id)
 WHERE ra.organization_id IS NOT DISTINCT FROM $1::uuid
   AND ra.scope_type IN ('platform', 'organization', 'group')
   AND ($2::uuid IS NULL OR ra.role_id = $2::uuid)
   AND ($3::uuid IS NULL OR ra.subject_id = $3::uuid)
   AND (
            $4::text = 'all'
        OR ($4::text = 'active'  AND ra.revoked_at IS NULL AND (ra.expires_at IS NULL OR ra.expires_at > $5::timestamptz))
        OR ($4::text = 'revoked' AND ra.revoked_at IS NOT NULL)
        OR ($4::text = 'expired' AND ra.revoked_at IS NULL AND ra.expires_at IS NOT NULL AND ra.expires_at <= $5::timestamptz))
   AND ($6::timestamptz IS NULL OR (ra.created_at, ra.id) < ($6::timestamptz, $7::uuid))
 ORDER BY ra.created_at DESC, ra.id DESC
 LIMIT $8`

// List mengembalikan paling banyak q.Limit assignment (dipangkas ClampAssignmentLimit) milik
// organizationID, terbaru dulu menurut (created_at, id); q.After melanjutkan setelah posisi itu.
// Status kosong berarti active; status yang tidak dikenal adalah galat, bukan "semua". Posisi
// hanya mengurutkan: scope selalu datang dari organizationID. Baris yang kolom scope-nya tidak
// dapat dipahami gagal-tertutup sebagai galat. Hasil tidak pernah nil.
func (r *AssignmentReader) List(ctx context.Context, organizationID *uuid.UUID, q domain.AssignmentListQuery) ([]domain.AssignmentView, error) {
	status := q.Status
	if status == "" {
		status = domain.AssignmentFilterActive
	}
	if !domain.ValidAssignmentFilter(status) {
		return nil, errUnknownAssignmentFilter
	}
	now := q.Now
	if now.IsZero() {
		now = time.Now()
	}
	var afterAt *time.Time
	var afterID *uuid.UUID
	if q.After != nil {
		at, pid := q.After.CreatedAt, q.After.ID
		afterAt, afterID = &at, &pid
	}

	rows, err := kernel.DBFrom(ctx, r.db).Query(ctx, listAssignmentsSQL,
		organizationID, q.RoleID, q.SubjectID, status, now.UTC(), afterAt, afterID, domain.ClampAssignmentLimit(q.Limit))
	if err != nil {
		return nil, fmt.Errorf("authorization: daftar assignment: %w", err)
	}
	defer rows.Close()

	items := []domain.AssignmentView{}
	for rows.Next() {
		var v domain.AssignmentView
		var scopeType string
		var org, scopeID *uuid.UUID
		if err := rows.Scan(&v.ID, &v.RoleID, &v.SubjectType, &v.SubjectID, &scopeType, &org, &scopeID,
			&v.AssignedBy, &v.ExpiresAt, &v.RevokedAt, &v.CreatedAt,
			&v.RoleName, &v.RoleSlug, &v.RoleStatus); err != nil {
			return nil, fmt.Errorf("authorization: daftar assignment: %w", err)
		}
		v.Scope, err = domain.TargetFromColumns(scopeType, org, scopeID)
		if err != nil {
			return nil, fmt.Errorf("authorization: assignment %s memakai scope %q yang tidak dapat dipahami: %w", v.ID, scopeType, err)
		}
		items = append(items, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("authorization: daftar assignment: %w", err)
	}
	return items, nil
}
