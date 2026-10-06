package migrations_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Grants docs/04 §13: kegagalan harus datang dari PostgreSQL sendiri (permission denied, 42501).

var appendOnly = map[string]struct{ insert, updateCol string }{
	"activities": {
		`INSERT INTO activities (action, result) VALUES ('login', 'success')`,
		`UPDATE activities SET action = 'tampered'`,
	},
	"security_events": {
		`INSERT INTO security_events (event_type, result) VALUES ('login', 'success')`,
		`UPDATE security_events SET event_type = 'tampered'`,
	},
	"system_logs": {
		`INSERT INTO system_logs (service, environment, level, event_code, message) VALUES ('api', 'test', 'info', 'x', 'm')`,
		`UPDATE system_logs SET message = 'tampered'`,
	},
}

func TestAppUserCannotModifyAppendOnlyTables(t *testing.T) {
	w := newWorld(t)
	app := w.db.App
	for table, q := range appendOnly {
		t.Run(table, func(t *testing.T) {
			require.NoError(t, tryExec(app, q.insert), "app_user HARUS bisa INSERT")
			require.Equal(t, 1, scalar[int](t, app, `SELECT count(*) FROM `+table), "app_user HARUS bisa SELECT")

			requireDB(t, tryExec(app, q.updateCol), noPrivilege, "")
			requireDB(t, tryExec(app, `DELETE FROM `+table), noPrivilege, "")
			requireDB(t, tryExec(app, `TRUNCATE `+table), noPrivilege, "")
			require.Equal(t, 1, scalar[int](t, w.p, `SELECT count(*) FROM `+table), "baris harus utuh")
		})
	}
}

func TestAppUserHasDMLOnOrdinaryTablesButNoDDL(t *testing.T) {
	w := newWorld(t)
	app := w.db.App
	exec(t, app, `INSERT INTO organizations (name, slug) VALUES ('A', 'a-org')`)
	exec(t, app, `UPDATE organizations SET name = 'B' WHERE slug = 'a-org'`)
	exec(t, app, `DELETE FROM organizations WHERE slug = 'a-org'`)
	exec(t, app, `INSERT INTO outbox_events (event_name) VALUES ('x')`)
	exec(t, app, `UPDATE outbox_events SET status = 'processed', processed_at = now()`) // relay menandai terproses

	requireDB(t, tryExec(app, `CREATE TABLE app_user_ddl (id int)`), noPrivilege, "")
	requireDB(t, tryExec(app, `DROP TABLE organizations`), "42501", "") // bukan pemilik
}

func TestMigrationVersionTableIsReadOnlyForAppUser(t *testing.T) { // ADR-0006
	w := newWorld(t)
	app := w.db.App
	require.Equal(t, 1, scalar[int](t, app, `SELECT count(*) FROM schema_migrations`), "runtime boleh membaca versi skema")
	requireDB(t, tryExec(app, `UPDATE schema_migrations SET dirty = true`), noPrivilege, "")
	requireDB(t, tryExec(app, `DELETE FROM schema_migrations`), noPrivilege, "")
	requireDB(t, tryExec(app, `INSERT INTO schema_migrations (version, dirty) VALUES (999, false)`), noPrivilege, "")
}

func TestTableCreatedLaterByMigratorIsUsableByAppUser(t *testing.T) {
	w := newWorld(t)
	exec(t, w.p, `CREATE TABLE announcements_posts (id uuid PRIMARY KEY DEFAULT uuidv7(), title text NOT NULL)`)
	exec(t, w.db.App, `INSERT INTO announcements_posts (title) VALUES ('hello')`)
	require.Equal(t, 1, scalar[int](t, w.db.App, `SELECT count(*) FROM announcements_posts`))
	exec(t, w.db.App, `UPDATE announcements_posts SET title = 'hi'`)
	exec(t, w.db.App, `DELETE FROM announcements_posts`)

	// modul yang ingin append-only menambahkan REVOKE sendiri (docs/04 §13.2)
	exec(t, w.p, `CREATE TABLE announcements_audit (id uuid PRIMARY KEY DEFAULT uuidv7(), note text)`)
	exec(t, w.p, `REVOKE UPDATE, DELETE ON announcements_audit FROM app_user`)
	exec(t, w.db.App, `INSERT INTO announcements_audit (note) VALUES ('n')`)
	requireDB(t, tryExec(w.db.App, `DELETE FROM announcements_audit`), noPrivilege, "")
}

func TestMaintenanceRoleIsNarrow(t *testing.T) {
	w := newWorld(t)
	m := w.db.Maintenance
	for _, q := range appendOnly {
		exec(t, w.p, q.insert)
	}
	exec(t, w.p, `INSERT INTO outbox_events (event_name, status) VALUES ('done', 'processed')`)

	t.Run("DELETE pada tabel log dan outbox", func(t *testing.T) {
		for _, table := range []string{"activities", "security_events", "system_logs", "outbox_events"} {
			exec(t, m, `DELETE FROM `+table)
			require.Zero(t, scalar[int](t, w.p, `SELECT count(*) FROM `+table), table)
		}
	})

	t.Run("UPDATE hanya pada kolom yang diberikan", func(t *testing.T) {
		exec(t, w.p, `INSERT INTO activities (action, result) VALUES ('a', 'success')`)
		exec(t, w.p, `INSERT INTO security_events (event_type, result) VALUES ('e', 'success')`)

		exec(t, m, `UPDATE activities SET actor_user_id = NULL, metadata = '{"anonymized": true}'`)
		requireDB(t, tryExec(m, `UPDATE activities SET action = 'x'`), noPrivilege, "")
		requireDB(t, tryExec(m, `UPDATE activities SET result = 'failed'`), noPrivilege, "")

		exec(t, m, `UPDATE security_events SET user_id = NULL, ip_address = NULL, user_agent = NULL, metadata = '{}'`)
		requireDB(t, tryExec(m, `UPDATE security_events SET event_type = 'x'`), noPrivilege, "")
		requireDB(t, tryExec(m, `UPDATE system_logs SET message = 'x'`), noPrivilege, "")
	})

	t.Run("tak bisa INSERT dan tak bisa membaca tabel lain", func(t *testing.T) {
		requireDB(t, tryExec(m, `INSERT INTO activities (action, result) VALUES ('x', 'success')`), noPrivilege, "")
		requireDB(t, tryExec(m, `SELECT * FROM users`), noPrivilege, "")
		requireDB(t, tryExec(m, `DELETE FROM organizations`), noPrivilege, "")
	})
}
