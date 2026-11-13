package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/policy"
)

// errUnknownPolicyFilter menandai filter status yang tidak dikenal. Handler memvalidasi nilai
// klien lebih dulu, jadi galat ini berarti kesalahan pemrograman dan tidak pernah menjadi "semua".
var errUnknownPolicyFilter = errors.New("authorization: filter status policy tidak dikenal")

// PolicyReader membaca policies untuk GET /policies (ADR-0022 butir 68). Setiap kueri memfilter
// tenant: organizationID adalah organisasi dari ScopeContext. Pemanggil tenant melihat policy
// organisasinya DAN policy global (penanda: PolicyRecord.Global()); nil (scope platform) hanya
// melihat policy global. Policy organisasi lain tidak pernah terbaca.
type PolicyReader struct {
	db kernel.DBTX
}

// NewPolicyReader membuat PolicyReader di atas db (pool runtime app_user).
func NewPolicyReader(db kernel.DBTX) *PolicyReader { return &PolicyReader{db: db} }

// Dengan $1 NULL perbandingan kedua tidak pernah benar, jadi scope platform hanya melihat policy
// global (organization_id NULL).
const policyListWhereSQL = `
 WHERE (organization_id IS NULL OR organization_id = $1)
   AND ($2::text = 'all' OR status = $2::text)`

const countPoliciesSQL = `SELECT count(*) FROM policies` + policyListWhereSQL

const listPoliciesSQL = `
SELECT id, organization_id, code, name, COALESCE(description, ''), effect, expression, actions, status, created_at, updated_at
  FROM policies` + policyListWhereSQL + `
 ORDER BY created_at, id
 LIMIT $3 OFFSET $4`

// List mengembalikan satu halaman policy yang terlihat bagi organizationID. Urutan tetap
// (created_at, id) supaya halaman tidak bergeser antar permintaan. Total dan halaman dibaca dengan
// dua kueri, jadi keduanya dapat berselisih bila policy berubah di antaranya. Actions dikembalikan
// terurut menurut byte (diurutkan di Go, bukan di SQL).
func (r *PolicyReader) List(ctx context.Context, organizationID *uuid.UUID, q domain.PolicyListQuery) (domain.PolicyPage, error) {
	status := q.Status
	if status == "" {
		status = domain.PolicyFilterAll
	}
	if !domain.ValidPolicyFilter(status) {
		return domain.PolicyPage{}, errUnknownPolicyFilter
	}
	w := domain.ClampWindow(q.Window)
	db := kernel.DBFrom(ctx, r.db)

	var total int
	if err := db.QueryRow(ctx, countPoliciesSQL, organizationID, status).Scan(&total); err != nil {
		return domain.PolicyPage{}, fmt.Errorf("authorization: hitung policy: %w", err)
	}
	rows, err := db.Query(ctx, listPoliciesSQL, organizationID, status, w.Limit, w.Offset)
	if err != nil {
		return domain.PolicyPage{}, fmt.Errorf("authorization: daftar policy: %w", err)
	}
	defer rows.Close()
	items := []domain.PolicyRecord{}
	for rows.Next() {
		var (
			rec    domain.PolicyRecord
			effect string
		)
		if err := rows.Scan(&rec.ID, &rec.OrganizationID, &rec.Code, &rec.Name, &rec.Description, &effect, &rec.Expression, &rec.Actions, &rec.Status, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
			return domain.PolicyPage{}, fmt.Errorf("authorization: daftar policy: %w", err)
		}
		rec.Effect = policy.Effect(effect)
		rec.Actions = domain.NormalizePolicyActions(rec.Actions)
		items = append(items, rec)
	}
	if err := rows.Err(); err != nil {
		return domain.PolicyPage{}, fmt.Errorf("authorization: daftar policy: %w", err)
	}
	return domain.PolicyPage{Items: items, Total: total}, nil
}
