package migrations_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Tes migrasi 035 (ADR-0022 butir 65): uq_role_assignments_active memuat organization_id, sehingga
// assignment yang sama di organisasi berbeda tidak bertabrakan. Semua penolakan harus datang dari
// PostgreSQL (SQLSTATE + nama constraint), bukan dari Go.

const uoUp = "035_role_assignments_unique_org.up.sql"

const uoDown = "035_role_assignments_unique_org.down.sql"

// uoInsert menyisipkan assignment user untuk subject pada scope yang diberikan (kolom lain bawaan).
func uoInsert(w *world, role, subject string, org any, scopeType string, scopeID any) error {
	return tryExec(w.p, `INSERT INTO role_assignments (organization_id, role_id, subject_type, subject_id, scope_type, scope_id)
		VALUES ($1, $2, 'user', $3, $4, $5)`, org, role, subject, scopeType, scopeID)
}

func uoIndexDef(w *world) string {
	return scalar[string](w.t, w.p, `SELECT indexdef FROM pg_indexes WHERE indexname = 'uq_role_assignments_active'`)
}

func TestRoleAssignmentsActiveKeyIsPerOrganization(t *testing.T) {
	w := newWorld(t)
	orgA, orgB := w.org(), w.org()
	role := w.role("system", nil)
	subject := w.user()

	require.NoError(t, uoInsert(w, role, subject, orgA, "organization", nil))
	require.NoError(t, uoInsert(w, role, subject, orgB, "organization", nil),
		"role sistem yang sama untuk user yang sama di organisasi lain bukan duplikat")

	requireDB(t, uoInsert(w, role, subject, orgA, "organization", nil), uniqueViolation, "uq_role_assignments_active")
	requireDB(t, uoInsert(w, role, subject, orgB, "organization", nil), uniqueViolation, "uq_role_assignments_active")

	require.NoError(t, uoInsert(w, role, subject, nil, "platform", nil), "platform tidak bertabrakan dengan organisasi")
	requireDB(t, uoInsert(w, role, subject, nil, "platform", nil), uniqueViolation, "uq_role_assignments_active")

	exec(t, w.p, `UPDATE role_assignments SET revoked_at = now() WHERE organization_id = $1`, orgB)
	require.NoError(t, uoInsert(w, role, subject, orgB, "organization", nil), "assignment yang dicabut membebaskan kuncinya")
	requireDB(t, uoInsert(w, role, subject, orgA, "organization", nil), uniqueViolation, "uq_role_assignments_active")
}

func TestRoleAssignmentsActiveKeyIndexDefinitionCarriesOrganization(t *testing.T) {
	w := newWorld(t)

	def := uoIndexDef(w)

	require.Contains(t, def, "organization_id")
	require.Contains(t, def, "scope_id")
	require.Contains(t, def, "revoked_at IS NULL")
}

func TestRoleAssignmentsUniqueOrgDownRefusesToLoseRowsAndRestoresTheOldKey(t *testing.T) {
	w := newWorld(t)
	orgA, orgB := w.org(), w.org()
	role := w.role("system", nil)
	subject := w.user()
	require.NoError(t, uoInsert(w, role, subject, orgA, "organization", nil))
	require.NoError(t, uoInsert(w, role, subject, orgB, "organization", nil))

	requireRaise(t, w.runScript(uoDown), "migrasi 035")
	require.Contains(t, uoIndexDef(w), "organization_id", "down yang gagal tidak mengubah indeks")

	exec(t, w.p, `UPDATE role_assignments SET revoked_at = now() WHERE organization_id = $1`, orgB)
	require.NoError(t, w.runScript(uoDown))
	require.NotContains(t, uoIndexDef(w), "organization_id", "kunci lama kembali")

	require.NoError(t, w.runScript(uoUp))
	require.Contains(t, uoIndexDef(w), "organization_id")
	require.NoError(t, uoInsert(w, role, subject, orgB, "organization", nil), "setelah up lagi, organisasi B kembali boleh memegang assignment yang sama")
}
