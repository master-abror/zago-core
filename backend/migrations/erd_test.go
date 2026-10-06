package migrations_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// ERD final (docs/21 M01 T6) dibangkitkan dari katalog PostgreSQL SETELAH semua migrasi berjalan,
// lalu dibandingkan dengan docs/erd.md. Menambah/mengubah migrasi tanpa memperbarui ERD = tes gagal.
// Perbarui dengan:  UPDATE_ERD=1 make db-test

const erdHeader = `# ERD — Skema Inti (final)

> **Dibangkitkan otomatis** oleh ` + "`backend/migrations/erd_test.go`" + ` dari skema PostgreSQL yang benar-benar
> dihasilkan migrasi 001–033. **Jangan sunting manual.** Perbarui: ` + "`UPDATE_ERD=1 make db-test`" + `.
> Penjelasan desain ada di [04-database-schema.md](04-database-schema.md); konsep di [03-database-erd.md](03-database-erd.md).
>
> Label relasi: kolom FK dan aksi ` + "`ON DELETE`" + `. Relasi polimorfik tanpa FK (mis. ` + "`role_assignments.scope_id`" + `,
> referensi lunak ` + "`activities`/`system_logs`" + `) sengaja tidak digambar (04 §12). Tabel versi golang-migrate
> (` + "`schema_migrations`" + `) tidak ditampilkan.

`

var pgTypeShort = strings.NewReplacer(
	"timestamp with time zone", "timestamptz",
	"character varying", "varchar",
	"double precision", "float8",
	" ", "_",
)

func generateERD(t *testing.T, w *world) string {
	t.Helper()

	type col struct {
		name, typ string
		pk, fk    bool
	}
	cols := map[string][]col{}
	var tables []string

	rows, err := w.p.Query(ctx, `
		SELECT c.relname, a.attname, format_type(a.atttypid, a.atttypmod),
		       EXISTS (SELECT 1 FROM pg_constraint k WHERE k.conrelid = c.oid AND k.contype = 'p' AND a.attnum = ANY (k.conkey)),
		       EXISTS (SELECT 1 FROM pg_constraint k WHERE k.conrelid = c.oid AND k.contype = 'f' AND a.attnum = ANY (k.conkey))
		  FROM pg_class c
		  JOIN pg_namespace n ON n.oid = c.relnamespace
		  JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
		 WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p') AND c.relname <> 'schema_migrations'
		 ORDER BY c.relname, a.attnum`)
	require.NoError(t, err)
	for rows.Next() {
		var table string
		var c col
		require.NoError(t, rows.Scan(&table, &c.name, &c.typ, &c.pk, &c.fk))
		if len(cols[table]) == 0 {
			tables = append(tables, table)
		}
		cols[table] = append(cols[table], c)
	}
	require.NoError(t, rows.Err())

	var b strings.Builder
	b.WriteString(erdHeader)
	b.WriteString("```mermaid\nerDiagram\n")
	for _, table := range tables {
		fmt.Fprintf(&b, "    %s {\n", table)
		for _, c := range cols[table] {
			marker := ""
			switch {
			case c.pk && c.fk:
				marker = " PK,FK"
			case c.pk:
				marker = " PK"
			case c.fk:
				marker = " FK"
			}
			fmt.Fprintf(&b, "        %s %s%s\n", pgTypeShort.Replace(c.typ), c.name, marker)
		}
		b.WriteString("    }\n")
	}

	type fkRow struct {
		child, parent, cols, action string
		nullable                    bool
	}
	frows, err := w.p.Query(ctx, `
		SELECT child.relname, parent.relname,
		       string_agg(att.attname, ', ' ORDER BY k.ord),
		       bool_or(NOT att.attnotnull),
		       CASE con.confdeltype WHEN 'c' THEN 'CASCADE' WHEN 'r' THEN 'RESTRICT' WHEN 'n' THEN 'SET NULL'
		                            WHEN 'a' THEN 'NO ACTION' WHEN 'd' THEN 'SET DEFAULT' END
		  FROM pg_constraint con
		  JOIN pg_class child  ON child.oid  = con.conrelid
		  JOIN pg_class parent ON parent.oid = con.confrelid
		  JOIN pg_namespace n  ON n.oid = child.relnamespace AND n.nspname = 'public'
		  CROSS JOIN LATERAL unnest(con.conkey) WITH ORDINALITY AS k(attnum, ord)
		  JOIN pg_attribute att ON att.attrelid = con.conrelid AND att.attnum = k.attnum
		 WHERE con.contype = 'f'
		 GROUP BY con.oid, child.relname, parent.relname, con.conname, con.confdeltype
		 ORDER BY child.relname, con.conname`)
	require.NoError(t, err)
	fks, err := pgx.CollectRows(frows, func(r pgx.CollectableRow) (fkRow, error) {
		var f fkRow
		return f, r.Scan(&f.child, &f.parent, &f.cols, &f.nullable, &f.action)
	})
	require.NoError(t, err)
	b.WriteString("\n")
	for _, f := range fks {
		card := "||--o{" // tepat satu induk
		if f.nullable {
			card = "|o--o{" // induk opsional
		}
		fmt.Fprintf(&b, "    %s %s %s : \"%s [%s]\"\n", f.parent, card, f.child, f.cols, f.action)
	}
	b.WriteString("```\n")
	return b.String()
}

func TestERDIsInSyncWithMigrations(t *testing.T) {
	w := newWorld(t)
	got := generateERD(t, w)

	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "..", "..", "docs", "erd.md")

	if os.Getenv("UPDATE_ERD") == "1" {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
		t.Logf("docs/erd.md diperbarui (%d byte)", len(got))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "docs/erd.md belum ada; jalankan: UPDATE_ERD=1 make db-test")
	require.Equal(t, string(want), got, "docs/erd.md tidak sinkron dengan migrasi; jalankan: UPDATE_ERD=1 make db-test")
}

func TestERDCoversEveryTable(t *testing.T) { // pelindung tes di atas dari kosong-melompong
	w := newWorld(t)
	erd := generateERD(t, w)
	n := scalar[int](t, w.p, `SELECT count(*) FROM pg_tables WHERE schemaname='public' AND tablename <> 'schema_migrations'`)
	require.Equal(t, 31, n, "jumlah tabel inti (002–032 = 31 tabel; 001 dan 033 tanpa tabel)")
	require.Equal(t, n, strings.Count(erd, " {\n"), "setiap tabel harus muncul di ERD")
	require.Contains(t, erd, `organizations ||--o{ groups : "organization_id [RESTRICT]"`)
	require.Contains(t, erd, `groups |o--o{ groups : "parent_group_id [RESTRICT]"`)
}
