// Package repository adalah jalur baca dan tulis tabel organisasi (docs/02 §6-§11). Berbeda
// dari tabel tenant biasa, organizations adalah akar tenant dan tidak punya kolom
// organization_id: pembatas tenantnya adalah id baris itu sendiri, dan setiap metode menerimanya
// lewat domain.Access.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/organization/domain"
)

// slugConstraint adalah nama indeks unik slug pada migrasi 003.
const slugConstraint = "uq_organizations_slug"

// errNotApplied menandai UPDATE yang tidak mengenai baris padahal organisasinya terlihat dan
// belum terarsip; hanya mungkin bila baris berubah di antara dua kueri.
var errNotApplied = errors.New("organization: pembaruan tidak diterapkan")

// Organizations membaca dan menulis tabel organizations. Setiap metode menerima domain.Access:
// Access tenant hanya melihat dan mengubah organisasinya sendiri, organisasi lain tidak dapat
// dibedakan dari yang tidak ada (domain.ErrOrganizationNotFound), dan Access nol ditolak
// (domain.ErrInvalidAccess). Hanya Access platform yang dapat membuat organisasi atau melihat
// lebih dari satu.
type Organizations struct {
	db kernel.DBTX
}

// NewOrganizations membuat Organizations di atas db (pool runtime app_user atau transaksi).
func NewOrganizations(db kernel.DBTX) *Organizations { return &Organizations{db: db} }

const orgColumns = `o.id, o.name, o.slug, o.legal_name, o.status, o.default_locale, o.default_timezone, o.created_at, o.updated_at, o.deleted_at`

// Pembatas tenant: $2 NULL hanya berasal dari domain.PlatformAccess; Access tenant selalu
// mengisinya dengan id organisasi scope.
const accessSQL = `o.id = $1 AND ($2::uuid IS NULL OR o.id = $2::uuid)`

const getSQL = `
SELECT ` + orgColumns + `
  FROM organizations o
 WHERE ` + accessSQL

const forUpdateSQL = getSQL + `
   FOR UPDATE OF o`

const listWhereSQL = `
 WHERE o.status = ANY($1::text[])
   AND ($2::uuid IS NULL OR o.id = $2::uuid)`

const countSQL = `SELECT count(*) FROM organizations o` + listWhereSQL

const listSQL = `
SELECT ` + orgColumns + `
  FROM organizations o` + listWhereSQL + `
 ORDER BY o.created_at, o.id
 LIMIT $3 OFFSET $4`

const insertSQL = `
INSERT INTO organizations AS o (id, name, slug, legal_name, status, default_locale, default_timezone)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING ` + orgColumns

// Pembaruan sebagian: bidang NULL tidak diubah; legal_name memakai bendera terpisah supaya string
// kosong dapat menghapusnya. Organisasi terarsip (deleted_at terisi) tidak diubah.
const updateSQL = `
UPDATE organizations AS o
   SET name             = COALESCE($3::text, o.name),
       legal_name       = CASE WHEN $4::boolean THEN NULLIF($5::text, '') ELSE o.legal_name END,
       default_locale   = COALESCE($6::text, o.default_locale),
       default_timezone = COALESCE($7::text, o.default_timezone)
 WHERE ` + accessSQL + `
   AND o.deleted_at IS NULL
RETURNING ` + orgColumns

// Perpindahan status hanya dari himpunan status asal yang sah ($4). Mengarsipkan mengisi
// deleted_at; kode otorisasi memeriksa status dan deleted_at sekaligus.
const setStatusSQL = `
UPDATE organizations AS o
   SET status     = $3::text,
       deleted_at = CASE WHEN $3::text = 'archived' THEN now() ELSE o.deleted_at END
 WHERE ` + accessSQL + `
   AND o.status = ANY($4::text[])
RETURNING ` + orgColumns

// probeSQL membedakan "tidak ada" dari "ada tetapi tidak dapat diubah" setelah UPDATE tidak
// mengenai baris, dengan pembatas Access yang sama.
const probeSQL = `SELECT o.deleted_at IS NOT NULL FROM organizations o WHERE ` + accessSQL

func scanRecord(row pgx.Row) (domain.Record, error) {
	var r domain.Record
	err := row.Scan(&r.ID, &r.Name, &r.Slug, &r.LegalName, &r.Status, &r.DefaultLocale, &r.DefaultTimezone, &r.CreatedAt, &r.UpdatedAt, &r.ArchivedAt)
	return r, err
}

func readError(err error, what string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrOrganizationNotFound
	}
	return fmt.Errorf("organization: %s: %w", what, err)
}

// Get membaca satu organisasi. Access tenant yang menunjuk organisasi lain mendapat
// domain.ErrOrganizationNotFound, sama seperti id yang tidak ada.
func (r *Organizations) Get(ctx context.Context, a domain.Access, organizationID uuid.UUID) (domain.Record, error) {
	filter, err := a.Filter()
	if err != nil {
		return domain.Record{}, err
	}
	rec, err := scanRecord(kernel.DBFrom(ctx, r.db).QueryRow(ctx, getSQL, organizationID, filter))
	if err != nil {
		return domain.Record{}, readError(err, "baca organisasi")
	}
	return rec, nil
}

// ForUpdate membaca satu organisasi dengan kunci baris. Wajib di dalam transaksi: di luar
// transaksi kunci langsung lepas, jadi metode menolaknya (domain.ErrNoTransaction).
func (r *Organizations) ForUpdate(ctx context.Context, a domain.Access, organizationID uuid.UUID) (domain.Record, error) {
	filter, err := a.Filter()
	if err != nil {
		return domain.Record{}, err
	}
	if !kernel.InTx(ctx) {
		return domain.Record{}, domain.ErrNoTransaction
	}
	rec, err := scanRecord(kernel.DBFrom(ctx, r.db).QueryRow(ctx, forUpdateSQL, organizationID, filter))
	if err != nil {
		return domain.Record{}, readError(err, "kunci organisasi")
	}
	return rec, nil
}

// List mengembalikan satu halaman organisasi yang terlihat bagi a, diurutkan (created_at, id)
// supaya halaman tidak bergeser. Access tenant paling banyak melihat satu organisasi. Total dan
// halaman dibaca dengan dua kueri, jadi keduanya dapat berselisih bila data berubah di antaranya.
func (r *Organizations) List(ctx context.Context, a domain.Access, q domain.ListQuery) (domain.Page, error) {
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
		return domain.Page{}, fmt.Errorf("organization: hitung organisasi: %w", err)
	}
	rows, err := db.Query(ctx, listSQL, statuses, filter, w.Limit, w.Offset)
	if err != nil {
		return domain.Page{}, fmt.Errorf("organization: daftar organisasi: %w", err)
	}
	defer rows.Close()
	items := []domain.Record{}
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return domain.Page{}, fmt.Errorf("organization: daftar organisasi: %w", err)
		}
		items = append(items, rec)
	}
	if err := rows.Err(); err != nil {
		return domain.Page{}, fmt.Errorf("organization: daftar organisasi: %w", err)
	}
	return domain.Page{Items: items, Total: total}, nil
}

// Insert membuat organisasi. Hanya Access platform: Access tenant mendapat
// domain.ErrPlatformRequired dan tidak ada yang ditulis. Record divalidasi lebih dulu
// (Record.Validate); slug yang sudah dipakai menjadi domain.ErrSlugTaken.
func (r *Organizations) Insert(ctx context.Context, a domain.Access, rec domain.Record) (domain.Record, error) {
	if !a.Valid() {
		return domain.Record{}, domain.ErrInvalidAccess
	}
	if !a.IsPlatform() {
		return domain.Record{}, domain.ErrPlatformRequired
	}
	if err := rec.Validate(); err != nil {
		return domain.Record{}, err
	}
	out, err := scanRecord(kernel.DBFrom(ctx, r.db).QueryRow(ctx, insertSQL,
		rec.ID, rec.Name, rec.Slug, rec.LegalName, rec.Status, rec.DefaultLocale, rec.DefaultTimezone))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == slugConstraint {
			return domain.Record{}, domain.ErrSlugTaken
		}
		return domain.Record{}, fmt.Errorf("organization: sisipkan organisasi: %w", err)
	}
	return out, nil
}

// Update mengubah bidang yang diisi pada in (name, legal_name, default_locale,
// default_timezone). Slug dan status tidak dapat diubah di sini. Organisasi terarsip menjadi
// domain.ErrOrganizationArchived; organisasi lain atau yang tidak ada menjadi
// domain.ErrOrganizationNotFound.
func (r *Organizations) Update(ctx context.Context, a domain.Access, organizationID uuid.UUID, in domain.UpdateInput) (domain.Record, error) {
	filter, err := a.Filter()
	if err != nil {
		return domain.Record{}, err
	}
	n, err := in.Normalize()
	if err != nil {
		return domain.Record{}, err
	}
	legalSet := n.LegalName != nil
	legal := ""
	if legalSet {
		legal = *n.LegalName
	}
	db := kernel.DBFrom(ctx, r.db)
	rec, err := scanRecord(db.QueryRow(ctx, updateSQL, organizationID, filter, n.Name, legalSet, legal, n.DefaultLocale, n.DefaultTimezone))
	if err == nil {
		return rec, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.Record{}, fmt.Errorf("organization: ubah organisasi: %w", err)
	}
	return domain.Record{}, r.diagnose(ctx, db, organizationID, filter, domain.ErrOrganizationArchived, errNotApplied)
}

// SetStatus memindahkan status organisasi ke to bila perpindahannya sah menurut
// domain.PredecessorStatuses. Perpindahan yang tidak sah, termasuk ke status yang sama dan keluar dari
// archived, menjadi domain.ErrInvalidTransition. Mengarsipkan juga mengisi deleted_at.
func (r *Organizations) SetStatus(ctx context.Context, a domain.Access, organizationID uuid.UUID, to string) (domain.Record, error) {
	filter, err := a.Filter()
	if err != nil {
		return domain.Record{}, err
	}
	from, err := domain.PredecessorStatuses(to)
	if err != nil {
		return domain.Record{}, err
	}
	db := kernel.DBFrom(ctx, r.db)
	rec, err := scanRecord(db.QueryRow(ctx, setStatusSQL, organizationID, filter, to, from))
	if err == nil {
		return rec, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.Record{}, fmt.Errorf("organization: ubah status organisasi: %w", err)
	}
	return domain.Record{}, r.diagnose(ctx, db, organizationID, filter, domain.ErrInvalidTransition, domain.ErrInvalidTransition)
}

// diagnose menerjemahkan UPDATE tanpa baris: tidak terlihat bagi Access (atau tidak ada) menjadi
// domain.ErrOrganizationNotFound; terlihat tetapi tidak dapat diubah menjadi archivedErr bila
// organisasinya terarsip, selain itu liveErr.
func (r *Organizations) diagnose(ctx context.Context, db kernel.DBTX, organizationID uuid.UUID, filter *uuid.UUID, archivedErr, liveErr error) error {
	var archived bool
	if err := db.QueryRow(ctx, probeSQL, organizationID, filter).Scan(&archived); err != nil {
		return readError(err, "periksa organisasi")
	}
	if archived {
		return archivedErr
	}
	return liveErr
}
