// Package repository adalah jalur baca dan tulis manajemen user (docs/02 §7, ADR-0023 butir
// 21-25). Berbeda dari tabel tenant biasa, users tidak punya kolom organization_id: seorang
// user dapat beranggota di banyak organisasi, sehingga pembatas tenantnya adalah keberadaan
// baris organization_memberships (status bukan removed) di organisasi pemanggil. Paket ini
// terpisah dari auth/repository, yang melayani login dan tidak mengelola data user.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/master-abror/zago-core/backend/internal/identity/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	orgdomain "github.com/master-abror/zago-core/backend/internal/organization/domain"
)

// Users membaca dan menulis tabel users. Metode ber-Access menerima organization/domain.Access:
// Access tenant hanya melihat user yang punya keanggotaan (bukan removed) di organisasinya,
// user organisasi lain tidak dapat dibedakan dari user yang tidak ada (domain.ErrUserNotFound),
// dan Access nol ditolak (orgdomain.ErrInvalidAccess). Metode berakhiran Self melayani pemilik
// data atas barisnya sendiri (GET/PATCH /profile): id berasal dari identitas terautentikasi,
// bukan dari masukan, dan tidak ada pembatas tenant. Baris soft-deleted tidak pernah terlihat.
type Users struct {
	db kernel.DBTX
}

// NewUsers membuat Users di atas db (pool runtime app_user atau transaksi).
func NewUsers(db kernel.DBTX) *Users { return &Users{db: db} }

const userColumns = `u.id, u.username, u.display_name, u.first_name, u.last_name, u.email, u.phone, u.avatar_url, u.locale, u.timezone, u.status, u.last_login_at, u.created_at, u.updated_at`

// Pembatas tenant: $2 NULL hanya berasal dari PlatformAccess atau jalur Self; Access tenant
// selalu mengisinya dengan id organisasi scope. Keanggotaan removed tidak memberi penglihatan;
// invited dan suspended tetap memberi (ADR-0023 butir 26).
const visibleSQL = `u.deleted_at IS NULL
   AND ($2::uuid IS NULL OR EXISTS (
        SELECT 1
          FROM organization_memberships m
         WHERE m.user_id = u.id
           AND m.organization_id = $2::uuid
           AND m.status <> 'removed'))`

const getSQL = `
SELECT ` + userColumns + `
  FROM users u
 WHERE u.id = $1
   AND ` + visibleSQL

const forUpdateSQL = getSQL + `
   FOR UPDATE OF u`

const listWhereSQL = `
 WHERE u.status = ANY($1::text[])
   AND ` + visibleSQL

const countSQL = `SELECT count(*) FROM users u` + listWhereSQL

const listSQL = `
SELECT ` + userColumns + `
  FROM users u` + listWhereSQL + `
 ORDER BY u.display_name, u.id
 LIMIT $3 OFFSET $4`

// Pembaruan sebagian berdaftar-putih: HANYA tujuh kolom ini yang dapat ditulis; email, username,
// status, dan id tidak pernah muncul di sini (ADR-0023 butir 24b). Bidang nullable memakai
// bendera terpisah supaya string kosong dapat menghapusnya. updated_at diisi trigger.
const updateSQL = `
UPDATE users AS u
   SET display_name = COALESCE($3::text, u.display_name),
       first_name   = CASE WHEN $4::boolean THEN NULLIF($5::text, '') ELSE u.first_name END,
       last_name    = CASE WHEN $6::boolean THEN NULLIF($7::text, '') ELSE u.last_name END,
       phone        = CASE WHEN $8::boolean THEN NULLIF($9::text, '') ELSE u.phone END,
       avatar_url   = CASE WHEN $10::boolean THEN NULLIF($11::text, '') ELSE u.avatar_url END,
       locale       = COALESCE($12::text, u.locale),
       timezone     = COALESCE($13::text, u.timezone)
 WHERE u.id = $1
   AND ` + visibleSQL + `
RETURNING ` + userColumns

func scanRecord(row pgx.Row) (domain.Record, error) {
	var r domain.Record
	err := row.Scan(&r.ID, &r.Username, &r.DisplayName, &r.FirstName, &r.LastName, &r.Email, &r.Phone,
		&r.AvatarURL, &r.Locale, &r.Timezone, &r.Status, &r.LastLoginAt, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}

func readError(err error, what string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrUserNotFound
	}
	return fmt.Errorf("identity: %s: %w", what, err)
}

// nullable memecah bidang nullable menjadi bendera "diubah" dan nilainya.
func nullable(p *string) (bool, string) {
	if p == nil {
		return false, ""
	}
	return true, *p
}

// get adalah pembacaan bersama; filter nil berarti tanpa pembatas tenant (platform atau Self).
func (r *Users) get(ctx context.Context, filter *uuid.UUID, userID uuid.UUID, lock bool) (domain.Record, error) {
	if userID == uuid.Nil {
		return domain.Record{}, domain.ErrInvalidID
	}
	sql := getSQL
	if lock {
		if !kernel.InTx(ctx) {
			return domain.Record{}, domain.ErrNoTransaction
		}
		sql = forUpdateSQL
	}
	rec, err := scanRecord(kernel.DBFrom(ctx, r.db).QueryRow(ctx, sql, userID, filter))
	if err != nil {
		return domain.Record{}, readError(err, "baca user")
	}
	return rec, nil
}

// update adalah penulisan bersama; filter nil berarti tanpa pembatas tenant (platform atau Self).
func (r *Users) update(ctx context.Context, filter *uuid.UUID, userID uuid.UUID, in domain.UpdateInput) (domain.Record, error) {
	if userID == uuid.Nil {
		return domain.Record{}, domain.ErrInvalidID
	}
	n, err := in.Normalize()
	if err != nil {
		return domain.Record{}, err
	}
	firstSet, first := nullable(n.FirstName)
	lastSet, last := nullable(n.LastName)
	phoneSet, phone := nullable(n.Phone)
	avatarSet, avatar := nullable(n.AvatarURL)
	rec, err := scanRecord(kernel.DBFrom(ctx, r.db).QueryRow(ctx, updateSQL,
		userID, filter, n.DisplayName, firstSet, first, lastSet, last, phoneSet, phone, avatarSet, avatar, n.Locale, n.Timezone))
	if err != nil {
		return domain.Record{}, readError(err, "ubah user")
	}
	return rec, nil
}

// Get membaca satu user yang terlihat bagi a. User organisasi lain, user yang tidak ada, dan user
// soft-deleted semuanya menjadi domain.ErrUserNotFound dengan pesan identik.
func (r *Users) Get(ctx context.Context, a orgdomain.Access, userID uuid.UUID) (domain.Record, error) {
	filter, err := a.Filter()
	if err != nil {
		return domain.Record{}, err
	}
	return r.get(ctx, filter, userID, false)
}

// GetSelf membaca baris milik pemanggil sendiri (GET /profile). userID wajib berasal dari
// identitas terautentikasi, bukan dari masukan.
func (r *Users) GetSelf(ctx context.Context, userID uuid.UUID) (domain.Record, error) {
	return r.get(ctx, nil, userID, false)
}

// ForUpdate membaca satu user yang terlihat bagi a dengan kunci baris. Wajib di dalam transaksi:
// di luar transaksi kunci langsung lepas, jadi metode menolaknya (domain.ErrNoTransaction).
func (r *Users) ForUpdate(ctx context.Context, a orgdomain.Access, userID uuid.UUID) (domain.Record, error) {
	filter, err := a.Filter()
	if err != nil {
		return domain.Record{}, err
	}
	return r.get(ctx, filter, userID, true)
}

// ForUpdateSelf adalah ForUpdate untuk baris milik pemanggil sendiri.
func (r *Users) ForUpdateSelf(ctx context.Context, userID uuid.UUID) (domain.Record, error) {
	return r.get(ctx, nil, userID, true)
}

// List mengembalikan satu halaman user yang terlihat bagi a, diurutkan (display_name, id) supaya
// halaman tidak bergeser. Tidak ada pencarian: pencarian email akan menjadi oracle keberadaan
// akun. Total dan halaman dibaca dengan dua kueri, jadi keduanya dapat berselisih bila data
// berubah di antaranya.
func (r *Users) List(ctx context.Context, a orgdomain.Access, q domain.ListQuery) (domain.Page, error) {
	filter, err := a.Filter()
	if err != nil {
		return domain.Page{}, err
	}
	statuses, err := domain.StatusesForFilter(q.Filter)
	if err != nil {
		return domain.Page{}, err
	}
	w := domain.ClampWindow(q.Window)
	db := kernel.DBFrom(ctx, r.db)

	var total int
	if err := db.QueryRow(ctx, countSQL, statuses, filter).Scan(&total); err != nil {
		return domain.Page{}, fmt.Errorf("identity: hitung user: %w", err)
	}
	rows, err := db.Query(ctx, listSQL, statuses, filter, w.Limit, w.Offset)
	if err != nil {
		return domain.Page{}, fmt.Errorf("identity: daftar user: %w", err)
	}
	defer rows.Close()
	items := []domain.Record{}
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return domain.Page{}, fmt.Errorf("identity: daftar user: %w", err)
		}
		items = append(items, rec)
	}
	if err := rows.Err(); err != nil {
		return domain.Page{}, fmt.Errorf("identity: daftar user: %w", err)
	}
	return domain.Page{Items: items, Total: total}, nil
}

// Update mengubah bidang yang diisi pada in (display_name, first_name, last_name, phone,
// avatar_url, locale, timezone). Hanya Access platform: Access tenant mendapat
// orgdomain.ErrPlatformRequired dan tidak ada yang ditulis, karena satu user dapat beranggota di
// banyak organisasi (ADR-0023 butir 23). User yang tidak ada atau soft-deleted menjadi
// domain.ErrUserNotFound.
func (r *Users) Update(ctx context.Context, a orgdomain.Access, userID uuid.UUID, in domain.UpdateInput) (domain.Record, error) {
	if _, err := a.Filter(); err != nil {
		return domain.Record{}, err
	}
	if !a.IsPlatform() {
		return domain.Record{}, orgdomain.ErrPlatformRequired
	}
	return r.update(ctx, nil, userID, in)
}

// UpdateSelf mengubah bidang yang diisi pada in untuk baris milik pemanggil sendiri
// (PATCH /profile); bidang yang dapat diubah sama dengan Update.
func (r *Users) UpdateSelf(ctx context.Context, userID uuid.UUID, in domain.UpdateInput) (domain.Record, error) {
	return r.update(ctx, nil, userID, in)
}
