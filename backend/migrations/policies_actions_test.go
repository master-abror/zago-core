package migrations_test

import (
	"fmt"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/migrations"
)

// Tes migrasi 034 (ADR-0022 butir 2 dan 10): policies.actions. Semua penolakan harus datang dari
// PostgreSQL (SQLSTATE + nama constraint), bukan dari Go.

const notNullViolation = "23502"

// tryPolicy menyisipkan policy dengan ekspresi SQL untuk kolom actions (literal tes, bukan input pengguna).
func (w *world) tryPolicy(org any, code, actions string) error {
	return tryExec(w.p, `INSERT INTO policies (organization_id, name, code, effect, expression, actions)
		VALUES ($1, 'P', $2, 'deny', 'true', `+actions+`)`, org, code)
}

// runScript menjalankan berkas migrasi apa adanya lewat protokol sederhana (banyak pernyataan sekaligus,
// satu transaksi implisit) seperti golang-migrate; galatnya *pgconn.PgError asli dari PostgreSQL.
func (w *world) runScript(name string) error {
	w.t.Helper()
	b, err := fs.ReadFile(migrations.FS, name)
	require.NoError(w.t, err)
	conn, err := w.p.Acquire(ctx)
	require.NoError(w.t, err)
	defer conn.Release()
	_, err = conn.Conn().PgConn().Exec(ctx, string(b)).ReadAll()
	return err
}

func TestPoliciesActionsAcceptsWellFormedValues(t *testing.T) {
	w := newWorld(t)
	org := w.org()
	cases := []struct{ name, actions string }{
		{"satu kode", `ARRAY['invoice.read']`},
		{"semua action satu resource", `ARRAY['invoice.*']`},
		{"resource bertingkat", `ARRAY['organization.member.manage', 'organization.member.*']`},
		{"segmen dengan angka dan garis bawah", `ARRAY['a_b1.c_2']`},
		{"kode dan resource bercampur", `ARRAY['invoice.read', 'invoice.update', 'report.*']`},
		{"tepat 50 elemen", `(SELECT array_agg('res' || g || '.read') FROM generate_series(1, 50) AS g)`},
		{"tepat 255 karakter", `ARRAY[repeat('a', 125) || '.' || repeat('b', 129)]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.NoError(t, w.tryPolicy(nil, fmt.Sprintf("global-%d", w.seq()), c.actions), "policy global")
			require.NoError(t, w.tryPolicy(org, fmt.Sprintf("org-%d", w.seq()), c.actions), "policy organisasi")
		})
	}
}

func TestPoliciesActionsRejectsMalformedValues(t *testing.T) {
	w := newWorld(t)
	cases := []struct{ name, actions string }{
		{"array kosong", `'{}'::text[]`},
		{"hanya NULL", `ARRAY[NULL]::text[]`},
		{"elemen NULL di antara yang sah", `ARRAY['invoice.read', NULL]`},
		{"elemen kosong", `ARRAY['']`},
		{"bintang saja", `ARRAY['*']`},
		{"satu segmen", `ARRAY['invoice']`},
		{"bintang di tengah", `ARRAY['invoice.*.read']`},
		{"bintang ganda", `ARRAY['invoice.**']`},
		{"huruf besar", `ARRAY['Invoice.read']`},
		{"spasi di ujung", `ARRAY['invoice.read ']`},
		{"baris baru menyelundupkan elemen kedua", `ARRAY[E'invoice.read\nreport.read']`},
		{"koma menyelundupkan elemen kedua", `ARRAY['invoice.read,report.read']`},
		{"titik ganda", `ARRAY['invoice..read']`},
		{"diawali titik", `ARRAY['.read']`},
		{"diakhiri titik", `ARRAY['invoice.']`},
		{"segmen diawali angka", `ARRAY['1invoice.read']`},
		{"fragmen SQL", `ARRAY['invoice.read;DROP TABLE policies']`},
		{"duplikat", `ARRAY['invoice.read', 'invoice.read']`},
		{"dua dimensi", `ARRAY[ARRAY['a.b', 'c.d'], ARRAY['e.f', 'g.h']]`},
		{"51 elemen", `(SELECT array_agg('res' || g || '.read') FROM generate_series(1, 51) AS g)`},
		{"256 karakter", `ARRAY[repeat('a', 125) || '.' || repeat('b', 130)]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := w.tryPolicy(nil, fmt.Sprintf("bad-%d", w.seq()), c.actions)
			requireDB(t, err, checkViolation, "ck_policies_actions")
		})
	}
	require.Zero(t, scalar[int](t, w.p, `SELECT count(*) FROM policies`), "tidak ada baris yang lolos")
}

func TestPoliciesActionsIsRequiredAndStaysValidOnUpdate(t *testing.T) {
	w := newWorld(t)

	t.Run("NULL ditolak", func(t *testing.T) {
		requireDB(t, w.tryPolicy(nil, "p-null", `NULL::text[]`), notNullViolation, "")
	})

	t.Run("dihilangkan ditolak (tanpa default)", func(t *testing.T) {
		err := tryExec(w.p, `INSERT INTO policies (name, code, effect, expression) VALUES ('P', 'p-omit', 'deny', 'true')`)
		requireDB(t, err, notNullViolation, "")
	})

	t.Run("update ke nilai tak sah ditolak, ke nilai sah diterima", func(t *testing.T) {
		require.NoError(t, w.tryPolicy(nil, "p-upd", `ARRAY['invoice.read']`))
		requireDB(t, tryExec(w.p, `UPDATE policies SET actions = '{}' WHERE code = 'p-upd'`), checkViolation, "ck_policies_actions")
		requireDB(t, tryExec(w.p, `UPDATE policies SET actions = ARRAY['x.y', 'x.y'] WHERE code = 'p-upd'`), checkViolation, "ck_policies_actions")
		requireDB(t, tryExec(w.p, `UPDATE policies SET actions = NULL WHERE code = 'p-upd'`), notNullViolation, "")
		exec(t, w.p, `UPDATE policies SET actions = ARRAY['invoice.*'] WHERE code = 'p-upd'`)
		require.Equal(t, "{invoice.*}", scalar[string](t, w.p, `SELECT actions::text FROM policies WHERE code = 'p-upd'`))
	})

	t.Run("pembatasan yang sama berlaku untuk app_user", func(t *testing.T) {
		// Bila fungsi pemeriksa tak dapat dieksekusi app_user, galatnya 42501 (bukan 23514/sukses).
		ok := tryExec(w.db.App, `INSERT INTO policies (name, code, effect, expression, actions) VALUES ('P', 'p-app', 'deny', 'true', ARRAY['invoice.read'])`)
		require.NoError(t, ok)
		bad := tryExec(w.db.App, `INSERT INTO policies (name, code, effect, expression, actions) VALUES ('P', 'p-app-bad', 'deny', 'true', '{}')`)
		requireDB(t, bad, checkViolation, "ck_policies_actions")
	})
}

func TestPoliciesActionsIndexSupportsOverlapLookup(t *testing.T) {
	w := newWorld(t)
	def := scalar[string](t, w.p, `SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname = 'idx_policies_actions'`)
	require.Contains(t, def, "USING gin (actions)")

	require.NoError(t, w.tryPolicy(nil, "p-exact", `ARRAY['invoice.read']`))
	require.NoError(t, w.tryPolicy(nil, "p-wild", `ARRAY['invoice.*']`))
	require.NoError(t, w.tryPolicy(nil, "p-other", `ARRAY['report.read']`))

	// Pola pencarian engine: kode persis ATAU resource.*  (resource = semua segmen sebelum titik terakhir).
	got := scalar[int](t, w.p, `SELECT count(*) FROM policies WHERE actions && ARRAY['invoice.read', 'invoice.*']`)
	require.Equal(t, 2, got)
	got = scalar[int](t, w.p, `SELECT count(*) FROM policies WHERE actions && ARRAY['invoice.update', 'invoice.*']`)
	require.Equal(t, 1, got, "hanya policy resource.* yang cocok")
}

// Guard migrasi: tabel tidak kosong berarti tidak ada cara aman menebak actions baris lama.
func TestMigration034RefusesNonEmptyPolicies(t *testing.T) {
	w := newWorld(t)
	require.NoError(t, w.runScript("034_policies_actions.down.sql"))
	exec(t, w.p, `INSERT INTO policies (name, code, effect, expression) VALUES ('P', 'legacy', 'deny', 'true')`)

	requireRaise(t, w.runScript("034_policies_actions.up.sql"), "policies tidak kosong")

	// Gagal utuh: tidak ada kolom maupun fungsi yang tertinggal.
	require.Zero(t, scalar[int](t, w.p, `SELECT count(*) FROM information_schema.columns WHERE table_name = 'policies' AND column_name = 'actions'`))
	require.Zero(t, scalar[int](t, w.p, `SELECT count(*) FROM pg_proc WHERE proname = 'policy_actions_valid'`))

	// Setelah dikosongkan, migrasi berhasil.
	exec(t, w.p, `DELETE FROM policies`)
	require.NoError(t, w.runScript("034_policies_actions.up.sql"))
	require.NoError(t, w.tryPolicy(nil, "after", `ARRAY['invoice.read']`))
}
