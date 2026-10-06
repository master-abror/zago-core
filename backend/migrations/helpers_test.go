package migrations_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/testpg"
)

var ctx = context.Background()

// SQLSTATE yang dipakai tes.
const (
	uniqueViolation = "23505"
	checkViolation  = "23514"
	fkViolation     = "23503"
	raiseException  = "P0001"
	noPrivilege     = "42501"
)

func server(t *testing.T) *testpg.Server { t.Helper(); return testpg.Shared(t) }

func tryExec(p *pgxpool.Pool, sql string, args ...any) error {
	_, err := p.Exec(ctx, sql, args...)
	return err
}

func exec(t *testing.T, p *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	require.NoError(t, tryExec(p, sql, args...), sql)
}

func scalar[T any](t *testing.T, p *pgxpool.Pool, sql string, args ...any) T {
	t.Helper()
	var v T
	require.NoError(t, p.QueryRow(ctx, sql, args...).Scan(&v), sql)
	return v
}

// requireDB menegaskan error berasal dari PostgreSQL sendiri dengan SQLSTATE (dan nama constraint
// bila diberikan) — bukan dari kode aplikasi.
func requireDB(t *testing.T, err error, code, constraint string) {
	t.Helper()
	require.Error(t, err)
	var pe *pgconn.PgError
	require.True(t, errors.As(err, &pe), "bukan error PostgreSQL: %v", err)
	require.Equal(t, code, pe.Code, "SQLSTATE: %s", pe.Message)
	if constraint != "" {
		require.Equal(t, constraint, pe.ConstraintName, pe.Message)
	}
}

// requireRaise menegaskan trigger melempar RAISE EXCEPTION dengan pesan yang memuat substr.
func requireRaise(t *testing.T, err error, substr string) {
	t.Helper()
	requireDB(t, err, raiseException, "")
	var pe *pgconn.PgError
	require.True(t, errors.As(err, &pe))
	require.Contains(t, pe.Message, substr)
}

// world membangun data uji lewat pool migrator (pemilik skema; tak terkena batas grant).
type world struct {
	t  *testing.T
	db *testpg.DB
	p  *pgxpool.Pool
	n  int
}

func newWorld(t *testing.T) *world {
	t.Helper()
	db := server(t).NewDB(t)
	return &world{t: t, db: db, p: db.Migrator}
}

func (w *world) seq() int { w.n++; return w.n }

func (w *world) id(sql string, args ...any) string {
	w.t.Helper()
	return scalar[string](w.t, w.p, sql+" RETURNING id::text", args...)
}

func (w *world) org() string {
	return w.id(`INSERT INTO organizations (name, slug) VALUES ($1, $2)`, "Org", fmt.Sprintf("org-%d", w.seq()))
}

func (w *world) user() string {
	return w.id(`INSERT INTO users (display_name, email) VALUES ('U', $1)`, fmt.Sprintf("u%d@example.org", w.seq()))
}

func (w *world) member(org, user string) {
	w.t.Helper()
	exec(w.t, w.p, `INSERT INTO organization_memberships (organization_id, user_id, status) VALUES ($1, $2, 'active')`, org, user)
}

// group membuat grup (dan pembuatnya) di org; parent boleh nil.
func (w *world) group(org string, parent any) string {
	w.t.Helper()
	return w.id(`INSERT INTO groups (organization_id, parent_group_id, name, slug, created_by) VALUES ($1, $2, 'G', $3, $4)`,
		org, parent, fmt.Sprintf("g-%d", w.seq()), w.user())
}

func (w *world) module(code string) string {
	return w.id(`INSERT INTO modules (code, name, version, manifest) VALUES ($1, 'M', '1.0.0', '{}')`, code)
}

// role membuat role dengan boundary apa adanya (org/grp boleh nil); mengembalikan error mentah.
func (w *world) tryRole(slug, typ string, org, grp any) error {
	return tryExec(w.p, `INSERT INTO roles (organization_id, group_id, name, slug, role_type) VALUES ($1, $2, 'R', $3, $4)`, org, grp, slug, typ)
}

func (w *world) role(typ string, org any) string {
	w.t.Helper()
	return w.id(`INSERT INTO roles (organization_id, name, slug, role_type) VALUES ($1, 'R', $2, $3)`, org, fmt.Sprintf("r-%d", w.seq()), typ)
}
