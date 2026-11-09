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
	"github.com/master-abror/zago-core/backend/internal/permission"
	"github.com/master-abror/zago-core/backend/internal/policy"
)

// Jalur tulis policy (ADR-0022 butir 50-52 dan 54, 59-61). Repositori ini hanya berisi SQL:
// validasi, aturan cakupan, transaksi, audit, dan event ada di application.PolicyWriter. Setiap
// metode memakai transaksi pada context bila ada (kernel.DBFrom), jadi semuanya berjalan di dalam
// transaksi PolicyWriter. Cakupan selalu persis: organizationID nil berarti policy global, dan
// policy organisasi lain tidak pernah terbaca (tidak ada pembacaan "global ATAU organisasi" di
// jalur tulis).

// LockOrganization mengambil kunci advisory organisasi pada transaksi context (lihat
// LockOrganization). Ada di sini supaya Policies memenuhi application.PolicyStore.
func (p *Policies) LockOrganization(ctx context.Context, organizationID uuid.UUID) error {
	return LockOrganization(ctx, organizationID)
}

// LockPlatform mengambil kunci advisory platform pada transaksi context (lihat LockPlatform):
// menjaga hitungan policy global.
func (p *Policies) LockPlatform(ctx context.Context) error {
	return LockPlatform(ctx)
}

// ActionCatalog adalah katalog permission untuk memvalidasi actions policy (policy.ActionCatalog):
// kode core dari kode (permission.Catalog), kode modul dari tabel permissions. Berupa potret yang
// dimuat sekali per penulisan; tidak pernah dipakai bersama antar permintaan.
type ActionCatalog struct {
	codes     map[string]struct{}
	resources map[string]struct{}
}

// NewActionCatalog membangun katalog dari kode core ditambah moduleCodes. Kode modul yang tidak
// berbentuk kode permission diabaikan (artinya tidak ada di katalog; gagal-tertutup).
func NewActionCatalog(moduleCodes []string) *ActionCatalog {
	c := &ActionCatalog{codes: map[string]struct{}{}, resources: map[string]struct{}{}}
	for _, d := range permission.Catalog() {
		c.add(d.Code)
	}
	for _, code := range moduleCodes {
		c.add(code)
	}
	return c
}

func (c *ActionCatalog) add(code string) {
	resource, _, err := permission.Split(code)
	if err != nil {
		return
	}
	c.codes[code] = struct{}{}
	c.resources[resource] = struct{}{}
}

// HasCode melaporkan apakah kode permission persis ada di katalog.
func (c *ActionCatalog) HasCode(code string) bool {
	_, ok := c.codes[code]
	return ok
}

// HasResource melaporkan apakah ada kode di katalog yang resource-nya persis resource itu.
func (c *ActionCatalog) HasResource(resource string) bool {
	_, ok := c.resources[resource]
	return ok
}

// moduleCodesSQL membaca kode permission milik modul. Permission core (module_id NULL) sengaja
// tidak dibaca: kode core bersumber dari kode, bukan dari tabel.
const moduleCodesSQL = `SELECT code FROM permissions WHERE module_id IS NOT NULL`

// Catalog memuat katalog permission pada transaksi context. Galat berarti katalog tidak dapat
// dimuat; pemanggil tidak boleh menggantinya dengan katalog kosong atau hanya-core.
func (p *Policies) Catalog(ctx context.Context) (policy.ActionCatalog, error) {
	rows, err := kernel.DBFrom(ctx, p.db).Query(ctx, moduleCodesSQL)
	if err != nil {
		return nil, fmt.Errorf("authorization: baca katalog modul: %w", err)
	}
	defer rows.Close()

	var codes []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("authorization: baca kode modul: %w", err)
		}
		codes = append(codes, code)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("authorization: iterasi katalog modul: %w", err)
	}
	return NewActionCatalog(codes), nil
}

// activePolicyCountSQL menghitung policy aktif satu cakupan: organisasi $1, atau global bila $1
// NULL (IS NOT DISTINCT FROM).
const activePolicyCountSQL = `
SELECT count(*)
  FROM policies
 WHERE status = 'active'
   AND organization_id IS NOT DISTINCT FROM $1::uuid`

// ActiveCount menghitung policy aktif pada cakupan organizationID (nil = global). Hitungan hanya
// berarti bila dibaca di bawah kunci advisory cakupan itu (policy.CheckPolicyCount).
func (p *Policies) ActiveCount(ctx context.Context, organizationID *uuid.UUID) (int, error) {
	var n int
	if err := kernel.DBFrom(ctx, p.db).QueryRow(ctx, activePolicyCountSQL, organizationID).Scan(&n); err != nil {
		return 0, fmt.Errorf("authorization: hitung policy aktif: %w", err)
	}
	return n, nil
}

const insertPolicySQL = `
INSERT INTO policies (id, organization_id, name, code, description, effect, expression, status, actions)
VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7, $8, $9)
RETURNING created_at, updated_at`

// Insert menyimpan policy baru dan mengembalikan record dengan waktu dari database. Kode yang
// sudah dipakai pada cakupan yang sama memberi domain.ErrPolicyCodeTaken; kode yang sama pada
// cakupan berbeda (dua organisasi, atau organisasi dan global) sah.
func (p *Policies) Insert(ctx context.Context, rec domain.PolicyRecord) (domain.PolicyRecord, error) {
	err := kernel.DBFrom(ctx, p.db).QueryRow(ctx, insertPolicySQL,
		rec.ID, rec.OrganizationID, rec.Name, rec.Code, rec.Description, string(rec.Effect), rec.Expression, rec.Status, rec.Actions,
	).Scan(&rec.CreatedAt, &rec.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && isPolicyCodeConstraint(pgErr.ConstraintName) {
			return domain.PolicyRecord{}, domain.ErrPolicyCodeTaken
		}
		return domain.PolicyRecord{}, fmt.Errorf("authorization: simpan policy: %w", err)
	}
	return rec, nil
}

// isPolicyCodeConstraint mengenali dua pagar keunikan kode: uq_policies_org_code (organisasi) dan
// uq_policies_global_code (indeks parsial untuk organization_id NULL, yang tidak tercakup
// constraint pertama karena NULL tidak dianggap sama).
func isPolicyCodeConstraint(name string) bool {
	return name == "uq_policies_org_code" || name == "uq_policies_global_code"
}

const policyForUpdateSQL = `
SELECT id, organization_id, code, name, COALESCE(description, ''), effect, expression, actions, status, created_at, updated_at
  FROM policies
 WHERE id = $1
   AND organization_id IS NOT DISTINCT FROM $2::uuid
   FOR UPDATE`

// ForUpdate membaca policy dan mengunci barisnya sampai transaksi selesai. Cakupan persis:
// dengan organizationID nil hanya policy global yang cocok, dan dengan organisasi hanya policy
// organisasi itu. Policy yang tidak ada atau berada di luar cakupan memberi
// domain.ErrPolicyNotFound; keduanya tidak dibedakan.
func (p *Policies) ForUpdate(ctx context.Context, policyID uuid.UUID, organizationID *uuid.UUID) (domain.PolicyRecord, error) {
	var (
		rec    domain.PolicyRecord
		effect string
	)
	err := kernel.DBFrom(ctx, p.db).QueryRow(ctx, policyForUpdateSQL, policyID, organizationID).Scan(
		&rec.ID, &rec.OrganizationID, &rec.Code, &rec.Name, &rec.Description, &effect, &rec.Expression, &rec.Actions, &rec.Status, &rec.CreatedAt, &rec.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PolicyRecord{}, domain.ErrPolicyNotFound
	}
	if err != nil {
		return domain.PolicyRecord{}, fmt.Errorf("authorization: baca policy: %w", err)
	}
	rec.Effect = policy.Effect(effect)
	return rec, nil
}

const updatePolicySQL = `
UPDATE policies
   SET name = $2, description = NULLIF($3, ''), effect = $4, expression = $5, actions = $6, status = $7
 WHERE id = $1
   AND organization_id IS NOT DISTINCT FROM $8::uuid
RETURNING updated_at`

// Update menyimpan isi policy (nama, deskripsi, efek, ekspresi, actions, status) dan mengembalikan
// record dengan UpdatedAt baru. Kode, cakupan, dan CreatedAt tidak pernah berubah. Baris yang
// tidak ada atau di luar cakupan rec.OrganizationID memberi domain.ErrPolicyNotFound.
func (p *Policies) Update(ctx context.Context, rec domain.PolicyRecord) (domain.PolicyRecord, error) {
	err := kernel.DBFrom(ctx, p.db).QueryRow(ctx, updatePolicySQL,
		rec.ID, rec.Name, rec.Description, string(rec.Effect), rec.Expression, rec.Actions, rec.Status, rec.OrganizationID,
	).Scan(&rec.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PolicyRecord{}, domain.ErrPolicyNotFound
	}
	if err != nil {
		return domain.PolicyRecord{}, fmt.Errorf("authorization: ubah policy: %w", err)
	}
	return rec, nil
}
