package migrations_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Tes constraint/trigger/cascade skema (docs/04 §17). Semua berjalan di PostgreSQL 18 sungguhan;
// error yang diharapkan harus datang dari database (SQLSTATE + nama constraint), bukan dari Go.

func TestConstraints(t *testing.T) {
	t.Run("organizations.slug unik", func(t *testing.T) {
		w := newWorld(t)
		exec(t, w.p, `INSERT INTO organizations (name, slug) VALUES ('A', 'acme')`)
		err := tryExec(w.p, `INSERT INTO organizations (name, slug) VALUES ('B', 'acme')`)
		requireDB(t, err, uniqueViolation, "uq_organizations_slug")
	})

	t.Run("users.email huruf besar ditolak", func(t *testing.T) {
		w := newWorld(t)
		err := tryExec(w.p, `INSERT INTO users (display_name, email) VALUES ('U', 'Mixed@Example.org')`)
		requireDB(t, err, checkViolation, "ck_users_email_lower")
		exec(t, w.p, `INSERT INTO users (display_name, email) VALUES ('U', 'ok@example.org')`)
		requireDB(t, tryExec(w.p, `INSERT INTO users (display_name, email) VALUES ('V', 'ok@example.org')`), uniqueViolation, "uq_users_email")
	})

	t.Run("roles: slug unik per boundary", func(t *testing.T) {
		w := newWorld(t)
		orgA, orgB := w.org(), w.org()
		grpA := w.group(orgA, nil)

		require.NoError(t, w.tryRole("admin", "system", nil, nil))
		requireDB(t, w.tryRole("admin", "system", nil, nil), uniqueViolation, "uq_roles_global_slug")

		require.NoError(t, w.tryRole("editor", "organization", orgA, nil))
		requireDB(t, w.tryRole("editor", "organization", orgA, nil), uniqueViolation, "uq_roles_org_slug")
		require.NoError(t, w.tryRole("editor", "organization", orgB, nil), "slug sama boleh di organisasi lain")

		require.NoError(t, w.tryRole("lead", "group", orgA, grpA))
		requireDB(t, w.tryRole("lead", "group", orgA, grpA), uniqueViolation, "uq_roles_group_slug")
	})

	t.Run("roles: ck_roles_boundary", func(t *testing.T) {
		w := newWorld(t)
		org := w.org()
		grp := w.group(org, nil)
		cases := []struct {
			name, typ string
			org, grp  any
			ok        bool
		}{
			{"system", "system", nil, nil, true},
			{"organization", "organization", org, nil, true},
			{"group", "group", org, grp, true},
			{"custom tanpa group", "custom", org, nil, true},
			{"custom dengan group", "custom", org, grp, true},
			{"system dengan org", "system", org, nil, false},
			{"system dengan group tanpa org", "system", nil, grp, false},
			{"organization tanpa org", "organization", nil, nil, false},
			{"organization dengan group", "organization", org, grp, false},
			{"group tanpa group", "group", org, nil, false},
			{"group tanpa org tetapi ber-group_id (setengah NULL)", "group", nil, grp, false},
			{"custom tanpa org", "custom", nil, nil, false},
			{"custom tanpa org ber-group_id", "custom", nil, grp, false},
		}
		for i, c := range cases {
			err := w.tryRole(fmt.Sprintf("b-%d", i), c.typ, c.org, c.grp)
			if c.ok {
				require.NoError(t, err, c.name)
			} else {
				requireDB(t, err, checkViolation, "ck_roles_boundary")
			}
		}
	})

	t.Run("role_assignments: scope", func(t *testing.T) {
		w := newWorld(t)
		org := w.org()
		role := w.role("organization", org)
		subject := w.user()
		ins := func(orgID any, scopeType string, scopeID any) error {
			return tryExec(w.p, `INSERT INTO role_assignments (organization_id, role_id, subject_type, subject_id, scope_type, scope_id)
				VALUES ($1, $2, 'user', $3, $4, $5)`, orgID, role, subject, scopeType, scopeID)
		}
		grp := w.group(org, nil)
		other := w.user() // uuid sembarang sebagai scope_id resource

		// valid
		require.NoError(t, ins(nil, "platform", nil))
		require.NoError(t, ins(org, "organization", nil))
		require.NoError(t, ins(org, "group", grp))
		require.NoError(t, ins(org, "resource", other))
		require.NoError(t, ins(org, "own", nil))

		// kombinasi scope/scope_id tak valid
		requireDB(t, ins(nil, "platform", other), checkViolation, "ck_role_assignments_scope")
		requireDB(t, ins(org, "own", other), checkViolation, "ck_role_assignments_scope")
		requireDB(t, ins(org, "group", nil), checkViolation, "ck_role_assignments_scope")
		requireDB(t, ins(org, "resource", nil), checkViolation, "ck_role_assignments_scope")
		// platform <=> organization_id NULL
		requireDB(t, ins(org, "platform", nil), checkViolation, "ck_role_assignments_org")
		requireDB(t, ins(nil, "organization", nil), checkViolation, "ck_role_assignments_org")
	})

	t.Run("role_assignments: satu assignment aktif per (role, subject, scope)", func(t *testing.T) {
		w := newWorld(t)
		org := w.org()
		role := w.role("organization", org)
		subject := w.user()
		ins := func(scopeType string, scopeID any) error {
			return tryExec(w.p, `INSERT INTO role_assignments (organization_id, role_id, subject_type, subject_id, scope_type, scope_id)
				VALUES ($1, $2, 'user', $3, $4, $5)`, org, role, subject, scopeType, scopeID)
		}
		require.NoError(t, ins("organization", nil))
		// scope_id NULL harus tetap tertangkap (COALESCE di indeks)
		requireDB(t, ins("organization", nil), uniqueViolation, "uq_role_assignments_active")

		exec(t, w.p, `UPDATE role_assignments SET revoked_at = now() WHERE role_id = $1`, role)
		require.NoError(t, ins("organization", nil), "setelah dicabut, assignment yang sama boleh dibuat lagi")
		requireDB(t, ins("organization", nil), uniqueViolation, "uq_role_assignments_active")
	})

	t.Run("settings: (scope, key) unik dengan atau tanpa module_id", func(t *testing.T) {
		w := newWorld(t)
		scope := w.org()
		mod := w.module("billing")
		ins := func(module any) error {
			return tryExec(w.p, `INSERT INTO settings (scope_type, scope_id, module_id, key, value, value_type)
				VALUES ('organization', $1, $2, 'theme', '"dark"', 'string')`, scope, module)
		}
		require.NoError(t, ins(nil))
		requireDB(t, ins(nil), uniqueViolation, "uq_settings_key_no_module")
		require.NoError(t, ins(mod))
		requireDB(t, ins(mod), uniqueViolation, "uq_settings_key_with_module")
	})

	t.Run("module_dependencies: tak boleh bergantung pada diri sendiri", func(t *testing.T) {
		w := newWorld(t)
		a, b := w.module("alpha"), w.module("beta")
		requireDB(t, tryExec(w.p, `INSERT INTO module_dependencies (module_id, dependency_module_id, version_constraint) VALUES ($1, $1, '*')`, a),
			checkViolation, "ck_module_dependencies_self")
		exec(t, w.p, `INSERT INTO module_dependencies (module_id, dependency_module_id, version_constraint) VALUES ($1, $2, '*')`, a, b)
	})

	t.Run("modules.code: format", func(t *testing.T) {
		w := newWorld(t)
		for _, bad := range []string{"Upper", "has-dash", "1starts_digit", "a", "_lead", "sp ace", strings.Repeat("a", 51)} {
			err := tryExec(w.p, `INSERT INTO modules (code, name, version, manifest) VALUES ($1, 'M', '1', '{}')`, bad)
			requireDB(t, err, checkViolation, "ck_modules_code_format")
		}
		for _, good := range []string{"ab", "chat_v2", "a" + strings.Repeat("b", 49)} {
			exec(t, w.p, `INSERT INTO modules (code, name, version, manifest) VALUES ($1, 'M', '1', '{}')`, good)
		}
	})

	t.Run("conversations: direct_key", func(t *testing.T) {
		w := newWorld(t)
		orgA, orgB := w.org(), w.org()
		creator := w.user()
		direct := func(org any, key any) error {
			return tryExec(w.p, `INSERT INTO conversations (organization_id, type, direct_key, created_by) VALUES ($1, 'direct', $2, $3)`, org, key, creator)
		}
		require.NoError(t, direct(orgA, "u1:u2"))
		requireDB(t, direct(orgA, "u1:u2"), uniqueViolation, "uq_conversations_direct")
		require.NoError(t, direct(orgB, "u1:u2"), "pasangan sama di organisasi lain boleh")

		requireDB(t, direct(orgA, nil), checkViolation, "ck_conversations_direct_key")
		requireDB(t, tryExec(w.p, `INSERT INTO conversations (organization_id, type, direct_key, created_by) VALUES ($1, 'group', 'x:y', $2)`, orgA, creator),
			checkViolation, "ck_conversations_direct_key")
		exec(t, w.p, `INSERT INTO conversations (organization_id, type, title, created_by) VALUES ($1, 'group', 'T', $2)`, orgA, creator)
	})

	t.Run("invitations: satu undangan pending per (organization, email)", func(t *testing.T) {
		w := newWorld(t)
		org := w.org()
		inviter := w.user()
		n := 0
		ins := func(status string) error {
			n++
			return tryExec(w.p, `INSERT INTO invitations (organization_id, email, invited_by, token_hash, status, expires_at)
				VALUES ($1, 'new@example.org', $2, $3, $4, now() + interval '1 day')`, org, inviter, []byte(fmt.Sprintf("token-%d", n)), status)
		}
		require.NoError(t, ins("pending"))
		requireDB(t, ins("pending"), uniqueViolation, "uq_invitations_pending")
		require.NoError(t, ins("revoked"), "yang bukan pending tidak dibatasi")
		exec(t, w.p, `UPDATE invitations SET status = 'revoked' WHERE status = 'pending'`)
		require.NoError(t, ins("pending"))
		requireDB(t, tryExec(w.p, `INSERT INTO invitations (organization_id, email, invited_by, token_hash, expires_at)
			VALUES ($1, 'UPPER@example.org', $2, $3, now())`, org, inviter, []byte("t-upper")), checkViolation, "ck_invitations_email_lower")
	})
}

func TestTenantIntegrityCompositeForeignKeys(t *testing.T) {
	w := newWorld(t)
	orgA, orgB := w.org(), w.org()
	grpA := w.group(orgA, nil)
	userB := w.user()
	w.member(orgB, userB) // userB hanya anggota organisasi B

	t.Run("group_membership: user tanpa keanggotaan di organisasi grup ditolak", func(t *testing.T) {
		err := tryExec(w.p, `INSERT INTO group_memberships (group_id, organization_id, user_id) VALUES ($1, $2, $3)`, grpA, orgA, userB)
		requireDB(t, err, fkViolation, "fk_group_memberships_org_member")
	})
	t.Run("group_membership: organization_id berbeda dari milik grup ditolak", func(t *testing.T) {
		err := tryExec(w.p, `INSERT INTO group_memberships (group_id, organization_id, user_id) VALUES ($1, $2, $3)`, grpA, orgB, userB)
		requireDB(t, err, fkViolation, "fk_group_memberships_group")
	})
	t.Run("group_membership sah diterima", func(t *testing.T) {
		userA := w.user()
		w.member(orgA, userA)
		exec(t, w.p, `INSERT INTO group_memberships (group_id, organization_id, user_id) VALUES ($1, $2, $3)`, grpA, orgA, userA)
	})

	convA := w.id(`INSERT INTO conversations (organization_id, type, title, created_by) VALUES ($1, 'group', 'T', $2)`, orgA, w.user())
	t.Run("conversation_member dari organisasi lain ditolak", func(t *testing.T) {
		// tak punya keanggotaan di organisasi percakapan
		err := tryExec(w.p, `INSERT INTO conversation_members (conversation_id, organization_id, user_id) VALUES ($1, $2, $3)`, convA, orgA, userB)
		requireDB(t, err, fkViolation, "fk_conversation_members_org_member")
		// organization_id tak sama dengan milik percakapan
		err = tryExec(w.p, `INSERT INTO conversation_members (conversation_id, organization_id, user_id) VALUES ($1, $2, $3)`, convA, orgB, userB)
		requireDB(t, err, fkViolation, "fk_conversation_members_conversation")
	})

	t.Run("role dengan group_id di organisasi lain ditolak", func(t *testing.T) {
		requireDB(t, w.tryRole("cross", "group", orgB, grpA), fkViolation, "fk_roles_group")
	})

	t.Run("invitation dengan group_id di organisasi lain ditolak", func(t *testing.T) {
		err := tryExec(w.p, `INSERT INTO invitations (organization_id, email, invited_by, group_id, token_hash, expires_at)
			VALUES ($1, 'x@example.org', $2, $3, $4, now())`, orgB, userB, grpA, []byte("cross-inv"))
		requireDB(t, err, fkViolation, "fk_invitations_group")
	})
}

func TestTriggers(t *testing.T) {
	t.Run("updated_at berubah saat UPDATE dan tidak pada baris yang tak disentuh", func(t *testing.T) {
		w := newWorld(t)
		a, b := w.org(), w.org()
		at := func(id string) time.Time {
			return scalar[time.Time](t, w.p, `SELECT updated_at FROM organizations WHERE id = $1`, id)
		}
		beforeA, beforeB := at(a), at(b)
		time.Sleep(20 * time.Millisecond)
		exec(t, w.p, `UPDATE organizations SET name = 'Renamed' WHERE id = $1`, a)
		require.True(t, at(a).After(beforeA), "updated_at baris yang diubah harus maju")
		require.Equal(t, beforeB, at(b), "baris yang tak disentuh tak boleh berubah")
	})

	t.Run("setiap tabel ber-updated_at punya trigger (pemindai katalog)", func(t *testing.T) {
		w := newWorld(t)
		scanned := scalar[int](t, w.p, scanTablesWithUpdatedAt)
		require.GreaterOrEqual(t, scanned, 16, "pemindai tak boleh kosong-melompong (inti M01 = 16 tabel ber-updated_at)")
		require.Empty(t, missingUpdatedAtTrigger(t, w))

		// kontrol negatif: pemindai HARUS menandai tabel yang lupa attach_updated_at
		exec(t, w.p, `CREATE TABLE zz_forgot_attach (id int, updated_at timestamptz NOT NULL DEFAULT now())`)
		require.Equal(t, []string{"zz_forgot_attach"}, missingUpdatedAtTrigger(t, w))
		exec(t, w.p, `SELECT attach_updated_at('zz_forgot_attach')`)
		require.Empty(t, missingUpdatedAtTrigger(t, w))
	})

	t.Run("groups: parent di organisasi lain ditolak", func(t *testing.T) {
		w := newWorld(t)
		orgA, orgB := w.org(), w.org()
		parentB := w.group(orgB, nil)
		err := tryExec(w.p, `INSERT INTO groups (organization_id, parent_group_id, name, slug, created_by) VALUES ($1, $2, 'G', 'child', $3)`, orgA, parentB, w.user())
		requireRaise(t, err, "same organization_id")
	})

	t.Run("groups: siklus ditolak (A->B->A dan A->A)", func(t *testing.T) {
		w := newWorld(t)
		org := w.org()
		a := w.group(org, nil)
		b := w.group(org, a)
		requireRaise(t, tryExec(w.p, `UPDATE groups SET parent_group_id = $1 WHERE id = $2`, b, a), "cycle")
		requireRaise(t, tryExec(w.p, `UPDATE groups SET parent_group_id = id WHERE id = $1`, a), "own parent")
	})

	t.Run("groups: kedalaman maksimum 8 level", func(t *testing.T) {
		w := newWorld(t)
		org := w.org()
		var parent any
		for level := 1; level <= 8; level++ {
			parent = w.group(org, parent) // level 8 masih boleh
		}
		err := tryExec(w.p, `INSERT INTO groups (organization_id, parent_group_id, name, slug, created_by) VALUES ($1, $2, 'G', 'too-deep', $3)`, org, parent, w.user())
		requireRaise(t, err, "deeper than 8")
	})

	t.Run("groups.organization_id tidak bisa diubah", func(t *testing.T) {
		w := newWorld(t)
		orgA, orgB := w.org(), w.org()
		g := w.group(orgA, nil)
		requireRaise(t, tryExec(w.p, `UPDATE groups SET organization_id = $1 WHERE id = $2`, orgB, g), "immutable")
	})
}

const scanTablesWithUpdatedAt = `
	SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
	 WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p')
	   AND EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'updated_at' AND NOT a.attisdropped)`

// missingUpdatedAtTrigger mengembalikan tabel public ber-kolom updated_at yang tak punya trigger
// set_updated_at (docs/04 §4; berlaku juga untuk tabel modul).
func missingUpdatedAtTrigger(t *testing.T, w *world) []string {
	t.Helper()
	rows, err := w.p.Query(ctx, `
		SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p')
		   AND EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'updated_at' AND NOT a.attisdropped)
		   AND NOT EXISTS (SELECT 1 FROM pg_trigger g WHERE g.tgrelid = c.oid AND NOT g.tgisinternal AND g.tgfoid = 'set_updated_at'::regproc)
		 ORDER BY c.relname`)
	require.NoError(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		out = append(out, name)
	}
	require.NoError(t, rows.Err())
	return out
}

func TestCascadesAndRestricts(t *testing.T) {
	t.Run("hapus organisasi: cascade ke role/assignment/instalasi modul/percakapan/undangan; user, audit, outbox tak tersentuh", func(t *testing.T) {
		w := newWorld(t)
		org := w.org()
		keep := w.org() // organisasi lain tak boleh ikut terhapus
		user := w.user()

		role := w.role("organization", org)
		exec(t, w.p, `INSERT INTO role_assignments (organization_id, role_id, subject_type, subject_id, scope_type) VALUES ($1, $2, 'user', $3, 'organization')`, org, role, user)
		mod := w.module("announcements")
		exec(t, w.p, `INSERT INTO module_installations (organization_id, module_id, version, installed_by) VALUES ($1, $2, '1.0.0', $3)`, org, mod, user)
		conv := w.id(`INSERT INTO conversations (organization_id, type, title, created_by) VALUES ($1, 'group', 'T', $2)`, org, user)
		exec(t, w.p, `INSERT INTO invitations (organization_id, email, invited_by, token_hash, expires_at) VALUES ($1, 'i@example.org', $2, $3, now())`, org, user, []byte("inv-del"))
		exec(t, w.p, `INSERT INTO activities (organization_id, action, result) VALUES ($1, 'x', 'success')`, org)
		exec(t, w.p, `INSERT INTO security_events (organization_id, event_type, result) VALUES ($1, 'x', 'success')`, org)
		exec(t, w.p, `INSERT INTO outbox_events (organization_id, event_name) VALUES ($1, 'x')`, org)

		exec(t, w.p, `DELETE FROM organizations WHERE id = $1`, org)

		for table, check := range map[string]struct {
			query string
			arg   string
		}{
			"roles":                {`SELECT count(*) FROM roles WHERE id = $1`, role},
			"role_assignments":     {`SELECT count(*) FROM role_assignments WHERE role_id = $1`, role},
			"module_installations": {`SELECT count(*) FROM module_installations WHERE organization_id = $1`, org},
			"conversations":        {`SELECT count(*) FROM conversations WHERE id = $1`, conv},
			"invitations":          {`SELECT count(*) FROM invitations WHERE organization_id = $1`, org},
		} {
			require.Zero(t, scalar[int](t, w.p, check.query, check.arg), "%s harus ikut terhapus", table)
		}
		require.Equal(t, 1, scalar[int](t, w.p, `SELECT count(*) FROM users WHERE id = $1`, user), "user tak boleh ikut terhapus")
		require.Equal(t, 1, scalar[int](t, w.p, `SELECT count(*) FROM organizations WHERE id = $1`, keep))
		require.Equal(t, 1, scalar[int](t, w.p, `SELECT count(*) FROM activities WHERE organization_id = $1`, org), "activities tak terpengaruh")
		require.Equal(t, 1, scalar[int](t, w.p, `SELECT count(*) FROM security_events WHERE organization_id = $1`, org))
		require.Equal(t, 1, scalar[int](t, w.p, `SELECT count(*) FROM outbox_events WHERE organization_id = $1`, org))
	})

	t.Run("hapus organisasi diblokir (RESTRICT) selama masih punya grup atau keanggotaan", func(t *testing.T) {
		w := newWorld(t)
		withGroup := w.org()
		w.group(withGroup, nil)
		requireRestricted(t, tryExec(w.p, `DELETE FROM organizations WHERE id = $1`, withGroup), "")

		withMember := w.org()
		w.member(withMember, w.user())
		requireRestricted(t, tryExec(w.p, `DELETE FROM organizations WHERE id = $1`, withMember), "")
	})

	t.Run("hapus user diblokir (RESTRICT) selama punya keanggotaan", func(t *testing.T) {
		w := newWorld(t)
		org, user := w.org(), w.user()
		w.member(org, user)
		requireRestricted(t, tryExec(w.p, `DELETE FROM users WHERE id = $1`, user), "")
	})

	t.Run("hapus grup: cascade ke group_memberships; diblokir selama role merujuknya", func(t *testing.T) {
		w := newWorld(t)
		org := w.org()
		user := w.user()
		w.member(org, user)

		free := w.group(org, nil)
		exec(t, w.p, `INSERT INTO group_memberships (group_id, organization_id, user_id) VALUES ($1, $2, $3)`, free, org, user)
		exec(t, w.p, `DELETE FROM groups WHERE id = $1`, free)
		require.Zero(t, scalar[int](t, w.p, `SELECT count(*) FROM group_memberships WHERE group_id = $1`, free))

		held := w.group(org, nil)
		require.NoError(t, w.tryRole("held", "group", org, held))
		requireRestricted(t, tryExec(w.p, `DELETE FROM groups WHERE id = $1`, held), "fk_roles_group")

		// grup yang masih punya anak juga tak bisa dihapus
		parent := w.group(org, nil)
		w.group(org, parent)
		requireRestricted(t, tryExec(w.p, `DELETE FROM groups WHERE id = $1`, parent), "")
	})

	t.Run("hapus grup: undangan ber-group_id ikut terhapus (ADR-0008)", func(t *testing.T) {
		w := newWorld(t)
		org := w.org()
		grp := w.group(org, nil)
		inviter := w.user()
		exec(t, w.p, `INSERT INTO invitations (organization_id, email, invited_by, group_id, token_hash, expires_at)
			VALUES ($1, 'g@example.org', $2, $3, $4, now() + interval '1 day')`, org, inviter, grp, []byte("inv-group"))
		exec(t, w.p, `DELETE FROM groups WHERE id = $1`, grp)
		require.Zero(t, scalar[int](t, w.p, `SELECT count(*) FROM invitations WHERE organization_id = $1`, org))
	})

	// KARAKTERISASI (ADR-0008): scope_id role_assignments polimorfik, jadi database TIDAK memblokir
	// penghapusan group yang masih dirujuk assignment. Pembersihannya tugas aplikasi (layanan group, M05).
	// Bila nanti ditambahkan trigger/FK, ubah tes ini dengan sengaja bersama ADR baru.
	t.Run("hapus grup tidak diblokir database oleh role_assignments ber-scope group (ADR-0008)", func(t *testing.T) {
		w := newWorld(t)
		org := w.org()
		grp := w.group(org, nil)
		role := w.role("organization", org)
		exec(t, w.p, `INSERT INTO role_assignments (organization_id, role_id, subject_type, subject_id, scope_type, scope_id)
			VALUES ($1, $2, 'user', $3, 'group', $4)`, org, role, w.user(), grp)
		exec(t, w.p, `DELETE FROM groups WHERE id = $1`, grp)
		require.Equal(t, 1, scalar[int](t, w.p, `SELECT count(*) FROM role_assignments WHERE scope_id = $1`, grp),
			"assignment yatim tersisa: aplikasi wajib mencabutnya sebelum menghapus group")
	})
}
