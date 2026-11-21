package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/organization/domain"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// orgFKConstraint adalah nama foreign key organization_id pada migrasi 005 (penamaan bawaan
// PostgreSQL: <tabel>_<kolom>_fkey).
const orgFKConstraint = "organization_memberships_organization_id_fkey"

// OrganizationMembers membaca dan menulis tabel organization_memberships (docs/02 §9). Berbeda
// dari Organizations, tabel ini memiliki kolom organization_id: SETIAP kueri memfilternya dari
// argumen, dan domain.Access tenant yang menunjuk organisasi lain ditolak sebelum kueri
// dijalankan serta tetap dipagari lagi di SQL. Aturan ada di ADR-0023 butir 11-13.
type OrganizationMembers struct {
	db kernel.DBTX
}

// NewOrganizationMembers membuat OrganizationMembers di atas db (pool runtime app_user atau
// transaksi).
func NewOrganizationMembers(db kernel.DBTX) *OrganizationMembers {
	return &OrganizationMembers{db: db}
}

const memberColumns = `m.id, m.organization_id, m.user_id, m.status, m.joined_at, m.invited_by, m.created_at, m.updated_at, u.display_name, u.status`

const memberFromSQL = `
  FROM organization_memberships m
  JOIN users u ON u.id = m.user_id`

// Pembatas: $1 organisasi, $2 pembatas tenant (NULL hanya berasal dari domain.PlatformAccess).
const memberScopeSQL = `m.organization_id = $1 AND ($2::uuid IS NULL OR m.organization_id = $2::uuid)`

const memberGetSQL = `
SELECT ` + memberColumns + memberFromSQL + `
 WHERE ` + memberScopeSQL + `
   AND m.user_id = $3`

const memberListSQL = `
SELECT ` + memberColumns + memberFromSQL + `
 WHERE ` + memberScopeSQL + `
   AND m.status = ANY($3::text[])
   AND ($4::timestamptz IS NULL OR (m.created_at, m.id) > ($4::timestamptz, $5::uuid))
 ORDER BY m.created_at, m.id
 LIMIT $6`

// Penambahan: hanya user aktif yang belum dihapus; baris yang sudah ada diaktifkan kembali
// HANYA bila statusnya removed (baris yang sama dipakai ulang karena uq_org_memberships).
// Tanpa baris hasil: user tidak layak, atau keanggotaan sudah ada (lihat diagnoseAdd).
const memberAddSQL = `
WITH w AS (
    INSERT INTO organization_memberships AS m (id, organization_id, user_id, status, joined_at, invited_by)
    SELECT $1::uuid, $2::uuid, u.id, 'active', now(), $4::uuid
      FROM users u
     WHERE u.id = $3::uuid AND u.status = 'active' AND u.deleted_at IS NULL
    ON CONFLICT ON CONSTRAINT uq_org_memberships DO UPDATE
       SET status = 'active', joined_at = now(), invited_by = EXCLUDED.invited_by
     WHERE m.status = 'removed'
    RETURNING m.id, m.organization_id, m.user_id, m.status, m.joined_at, m.invited_by, m.created_at, m.updated_at)
SELECT w.id, w.organization_id, w.user_id, w.status, w.joined_at, w.invited_by, w.created_at, w.updated_at, u.display_name, u.status
  FROM w
  JOIN users u ON u.id = w.user_id`

const memberStatusSQL = `
SELECT m.status
  FROM organization_memberships m
 WHERE m.organization_id = $1
   AND m.user_id = $2`

const memberRemoveSQL = `
WITH w AS (
    UPDATE organization_memberships AS m
       SET status = 'removed'
     WHERE ` + memberScopeSQL + `
       AND m.user_id = $3
       AND m.status <> 'removed'
    RETURNING m.id, m.organization_id, m.user_id, m.status, m.joined_at, m.invited_by, m.created_at, m.updated_at)
SELECT w.id, w.organization_id, w.user_id, w.status, w.joined_at, w.invited_by, w.created_at, w.updated_at, u.display_name, u.status
  FROM w
  JOIN users u ON u.id = w.user_id`

const memberRemoveGroupsSQL = `
UPDATE group_memberships AS gm
   SET status = 'removed'
 WHERE gm.organization_id = $1
   AND ($2::uuid IS NULL OR gm.organization_id = $2::uuid)
   AND gm.user_id = $3
   AND gm.status <> 'removed'`

func scanMember(row pgx.Row) (domain.Member, error) {
	var m domain.Member
	err := row.Scan(&m.ID, &m.OrganizationID, &m.UserID, &m.Status, &m.JoinedAt, &m.InvitedBy,
		&m.CreatedAt, &m.UpdatedAt, &m.DisplayName, &m.UserStatus)
	return m, err
}

// scopeFor memvalidasi Access dan id organisasi, lalu memastikan pemanggil tenant hanya
// menunjuk organisasinya sendiri; ketidakcocokan menjadi notFound, sama dengan id yang tidak
// ada. Hasilnya adalah nilai pembatas tenant untuk SQL (nil untuk platform).
func scopeFor(a domain.Access, organizationID uuid.UUID, notFound error) (*uuid.UUID, error) {
	filter, err := a.Filter()
	if err != nil {
		return nil, err
	}
	if organizationID == uuid.Nil {
		return nil, domain.ErrInvalidID
	}
	if filter != nil && *filter != organizationID {
		return nil, notFound
	}
	return filter, nil
}

// Get membaca satu keanggotaan (status apa pun, termasuk removed) berdasarkan user. Organisasi
// lain atau keanggotaan yang tidak ada menjadi domain.ErrMemberNotFound.
func (r *OrganizationMembers) Get(ctx context.Context, a domain.Access, organizationID, userID uuid.UUID) (domain.Member, error) {
	filter, err := scopeFor(a, organizationID, domain.ErrMemberNotFound)
	if err != nil {
		return domain.Member{}, err
	}
	if userID == uuid.Nil {
		return domain.Member{}, domain.ErrInvalidID
	}
	m, err := scanMember(kernel.DBFrom(ctx, r.db).QueryRow(ctx, memberGetSQL, organizationID, filter, userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Member{}, domain.ErrMemberNotFound
		}
		return domain.Member{}, fmt.Errorf("organization: baca anggota: %w", err)
	}
	return m, nil
}

// List membaca satu halaman anggota organisasi dengan cursor (created_at, id). Access tenant
// yang menunjuk organisasi lain mendapat domain.ErrOrganizationNotFound, sama dengan organisasi
// yang tidak ada. Filter tak dikenal menjadi domain.ErrInvalidFilter dan cursor rusak menjadi
// domain.ErrInvalidCursor; keduanya diperiksa sebelum kueri.
func (r *OrganizationMembers) List(ctx context.Context, a domain.Access, organizationID uuid.UUID, q domain.MemberListQuery) (domain.MemberPage, error) {
	filter, err := scopeFor(a, organizationID, domain.ErrOrganizationNotFound)
	if err != nil {
		return domain.MemberPage{}, err
	}
	statuses, err := domain.MemberStatusesForFilter(q.Filter)
	if err != nil {
		return domain.MemberPage{}, err
	}
	cur, err := domain.DecodeMemberCursor(q.Cursor)
	if err != nil {
		return domain.MemberPage{}, err
	}
	var afterAt *time.Time
	var afterID *uuid.UUID
	if cur != nil {
		at := cur.CreatedAt
		cid := cur.ID
		afterAt = &at
		afterID = &cid
	}
	limit := domain.ClampMemberLimit(q.Limit)

	// Satu baris lebih dari limit menandai adanya halaman berikutnya.
	rows, err := kernel.DBFrom(ctx, r.db).Query(ctx, memberListSQL,
		organizationID, filter, statuses, afterAt, afterID, limit+1)
	if err != nil {
		return domain.MemberPage{}, fmt.Errorf("organization: daftar anggota: %w", err)
	}
	defer rows.Close()
	items := []domain.Member{}
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return domain.MemberPage{}, fmt.Errorf("organization: daftar anggota: %w", err)
		}
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		return domain.MemberPage{}, fmt.Errorf("organization: daftar anggota: %w", err)
	}
	page := domain.MemberPage{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		page.NextCursor = items[limit-1].NextCursor().Encode()
	}
	return page, nil
}

// Add menambahkan user sebagai anggota AKTIF organisasi. Hanya Access platform: Access tenant
// mendapat domain.ErrPlatformRequired dan tidak ada yang ditulis (ADR-0023 butir 11). Keanggotaan
// yang dihapus diaktifkan kembali pada baris yang sama; yang masih aktif, diundang, atau
// ditangguhkan menjadi domain.ErrMemberExists. User yang tidak ada, tidak aktif, atau sudah
// dihapus menjadi domain.ErrUserNotEligible, dan organisasi yang tidak ada menjadi
// domain.ErrOrganizationNotFound. addedBy boleh nil.
func (r *OrganizationMembers) Add(ctx context.Context, a domain.Access, organizationID, userID uuid.UUID, addedBy *uuid.UUID) (domain.Member, error) {
	if !a.Valid() {
		return domain.Member{}, domain.ErrInvalidAccess
	}
	if !a.IsPlatform() {
		return domain.Member{}, domain.ErrPlatformRequired
	}
	if organizationID == uuid.Nil || userID == uuid.Nil {
		return domain.Member{}, domain.ErrInvalidID
	}
	db := kernel.DBFrom(ctx, r.db)
	m, err := scanMember(db.QueryRow(ctx, memberAddSQL, id.NewID(), organizationID, userID, addedBy))
	if err == nil {
		return m, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Member{}, diagnoseAdd(ctx, db, organizationID, userID)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" && pgErr.ConstraintName == orgFKConstraint {
		return domain.Member{}, domain.ErrOrganizationNotFound
	}
	return domain.Member{}, fmt.Errorf("organization: tambah anggota: %w", err)
}

// diagnoseAdd menjelaskan INSERT yang tidak menghasilkan baris: keanggotaan yang belum dihapus
// sudah ada (ErrMemberExists), atau user tidak layak (ErrUserNotEligible).
func diagnoseAdd(ctx context.Context, db kernel.DBTX, organizationID, userID uuid.UUID) error {
	var status string
	err := db.QueryRow(ctx, memberStatusSQL, organizationID, userID).Scan(&status)
	switch {
	case err == nil && status != domain.MemberRemoved:
		return domain.ErrMemberExists
	case err == nil || errors.Is(err, pgx.ErrNoRows):
		return domain.ErrUserNotEligible
	default:
		return fmt.Errorf("organization: diagnosis tambah anggota: %w", err)
	}
}

// Remove menghapus keanggotaan secara logis (status removed; baris dan user tetap ada).
// Mengembalikan keadaan SESUDAH perubahan. Keanggotaan yang tidak ada, sudah dihapus, atau
// milik organisasi lain menjadi domain.ErrMemberNotFound.
func (r *OrganizationMembers) Remove(ctx context.Context, a domain.Access, organizationID, userID uuid.UUID) (domain.Member, error) {
	filter, err := scopeFor(a, organizationID, domain.ErrMemberNotFound)
	if err != nil {
		return domain.Member{}, err
	}
	if userID == uuid.Nil {
		return domain.Member{}, domain.ErrInvalidID
	}
	m, err := scanMember(kernel.DBFrom(ctx, r.db).QueryRow(ctx, memberRemoveSQL, organizationID, filter, userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Member{}, domain.ErrMemberNotFound
		}
		return domain.Member{}, fmt.Errorf("organization: hapus anggota: %w", err)
	}
	return m, nil
}

// RemoveFromGroups menandai removed semua keanggotaan GRUP milik user di organisasi itu yang
// belum removed dan mengembalikan jumlah baris yang berubah (ADR-0023 butir 13). Kueri
// memfilter organization_id; grup organisasi lain tidak tersentuh. Access tenant yang menunjuk
// organisasi lain mendapat domain.ErrOrganizationNotFound.
func (r *OrganizationMembers) RemoveFromGroups(ctx context.Context, a domain.Access, organizationID, userID uuid.UUID) (int, error) {
	filter, err := scopeFor(a, organizationID, domain.ErrOrganizationNotFound)
	if err != nil {
		return 0, err
	}
	if userID == uuid.Nil {
		return 0, domain.ErrInvalidID
	}
	tag, err := kernel.DBFrom(ctx, r.db).Exec(ctx, memberRemoveGroupsSQL, organizationID, filter, userID)
	if err != nil {
		return 0, fmt.Errorf("organization: keluarkan anggota dari grup: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
