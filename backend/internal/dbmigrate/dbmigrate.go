// Package dbmigrate menjalankan migrasi inti dengan golang-migrate sebagai library (driver pgx5,
// sumber = backend/migrations yang disematkan) dan seeder baris platform (docs/04 §15–§16).
// Hanya cmd/migrate dan helper tes yang memakai paket ini; API/worker tidak pernah memegang hak DDL.
package dbmigrate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // mendaftarkan skema pgx5://
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/master-abror/zago-core/backend/internal/app"
	"github.com/master-abror/zago-core/backend/migrations"
)

// Runner mengimplementasikan app.MigrationRunner.
type Runner struct{}

var _ app.MigrationRunner = Runner{}

// sqlStateInsufficientPrivilege adalah kode SQLSTATE 42501.
const sqlStateInsufficientPrivilege = "42501"

// redactedError menyimpan pesan yang sudah dibersihkan dari password/URL, tetapi tetap
// mempertahankan rantai error (errors.Is/As) untuk pemanggil.
type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }

// redact membuang password dan URL lengkap dari pesan error. Error dari driver/URL parser
// kadang menyertakan URL koneksi yang memuat password (CLAUDE.md: rahasia tak pernah di-log).
func redact(err error, raw string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if u, perr := url.Parse(raw); perr == nil && u.User != nil {
		if pw, ok := u.User.Password(); ok && pw != "" {
			msg = strings.ReplaceAll(msg, pw, "***")
		}
	}
	if raw != "" {
		msg = strings.ReplaceAll(msg, raw, "<url>")
	}
	return &redactedError{msg: msg, err: err}
}

// withDatabase mengembalikan URL yang sama dengan skema dan nama database diganti.
func withDatabase(raw, scheme, dbName string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", errors.New("URL database tidak dapat diurai")
	}
	u.Scheme = scheme
	u.Path = "/" + dbName
	u.RawPath = ""
	return u.String(), nil
}

// asPostgres mengubah pgx5://… menjadi postgres://… (untuk pgx.Connect langsung).
func asPostgres(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", errors.New("URL database tidak dapat diurai")
	}
	u.Scheme = "postgres"
	return u.String(), nil
}

func newMigrate(ctx context.Context, rawURL string) (*migrate.Migrate, func(), error) {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, nil, fmt.Errorf("sumber migrasi: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, rawURL)
	if err != nil {
		return nil, nil, redact(fmt.Errorf("membuka database untuk migrasi: %w", err), rawURL)
	}
	// Berhenti dengan rapi di antara dua migrasi bila ctx dibatalkan (Ctrl-C / SIGTERM).
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			select {
			case m.GracefulStop <- true:
			default:
			}
		case <-done:
		}
	}()
	closeFn := func() {
		close(done)
		_, _ = m.Close()
	}
	return m, closeFn, nil
}

// Up menerapkan semua migrasi yang tertunda. Tidak ada yang tertunda = sukses.
func (Runner) Up(ctx context.Context, rawURL string) error {
	m, closeFn, err := newMigrate(ctx, rawURL)
	if err != nil {
		return err
	}
	defer closeFn()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return redact(fmt.Errorf("migrate up: %w", err), rawURL)
	}
	return nil
}

// Down memundurkan `steps` migrasi (>= 1). Sudah di versi nol = sukses.
func (Runner) Down(ctx context.Context, rawURL string, steps int) error {
	if steps < 1 {
		return errors.New("migrate down: steps harus >= 1")
	}
	m, closeFn, err := newMigrate(ctx, rawURL)
	if err != nil {
		return err
	}
	defer closeFn()
	// golang-migrate mengembalikan os.ErrNotExist bila Steps(-n) dijalankan di versi nol.
	if _, _, verr := m.Version(); errors.Is(verr, migrate.ErrNilVersion) {
		return nil
	}
	if err := m.Steps(-steps); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return redact(fmt.Errorf("migrate down: %w", err), rawURL)
	}
	return nil
}

// Version mengembalikan versi migrasi terpasang (0 bila belum ada) dan status dirty.
func (Runner) Version(ctx context.Context, rawURL string) (uint, bool, error) {
	m, closeFn, err := newMigrate(ctx, rawURL)
	if err != nil {
		return 0, false, err
	}
	defer closeFn()
	v, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, redact(fmt.Errorf("migrate version: %w", err), rawURL)
	}
	return v, dirty, nil
}

var migrationFile = regexp.MustCompile(`^(\d+)_[a-z0-9_]+\.up\.sql$`)

// LatestVersion mengembalikan nomor migrasi tertinggi di folder yang disematkan.
func LatestVersion() (uint, error) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return 0, err
	}
	var latest uint64
	for _, e := range entries {
		if m := migrationFile.FindStringSubmatch(e.Name()); m != nil {
			n, perr := strconv.ParseUint(m[1], 10, 32)
			if perr == nil && n > latest {
				latest = n
			}
		}
	}
	if latest == 0 {
		return 0, errors.New("tidak ada migrasi di folder yang disematkan")
	}
	return uint(latest), nil
}

// Roundtrip membuktikan migrasi dapat dimaju-mundurkan: membuat database SEMENTARA di server
// yang sama, lalu up -> down(semua) -> up, memeriksa tak ada objek tersisa setelah down, dan
// selalu menghapus database sementara itu. Data pengembangan tak pernah tersentuh.
// Role di rawURL butuh CREATEDB (deploy/db/init/01-dev-privileges.sql; ADR-0003).
func (r Runner) Roundtrip(ctx context.Context, rawURL string) (err error) {
	latest, err := LatestVersion()
	if err != nil {
		return err
	}
	adminURL, err := withDatabase(rawURL, "postgres", "postgres")
	if err != nil {
		return err
	}
	suffix := make([]byte, 6)
	if _, err = rand.Read(suffix); err != nil {
		return fmt.Errorf("roundtrip: %w", err)
	}
	name := "roundtrip_" + hex.EncodeToString(suffix) // hanya [a-z0-9_]: aman sebagai identifier

	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return redact(fmt.Errorf("roundtrip: gagal terhubung ke server: %w", err), rawURL)
	}
	defer func() { _ = admin.Close(context.WithoutCancel(ctx)) }()

	if _, err = admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == sqlStateInsufficientPrivilege {
			return errors.New("roundtrip: role migrator butuh CREATEDB (lihat deploy/db/init/01-dev-privileges.sql)")
		}
		return redact(fmt.Errorf("roundtrip: gagal membuat database sementara: %w", err), rawURL)
	}
	defer func() {
		dropCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if _, derr := admin.Exec(dropCtx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); derr != nil && err == nil {
			err = redact(fmt.Errorf("roundtrip: gagal menghapus database sementara %s: %w", name, derr), rawURL)
		}
	}()

	tmpURL, err := withDatabase(rawURL, "pgx5", name)
	if err != nil {
		return err
	}
	check := func(step string, want uint) error {
		v, dirty, verr := r.Version(ctx, tmpURL)
		if verr != nil {
			return verr
		}
		if dirty || v != want {
			return fmt.Errorf("roundtrip: setelah %s versi=%d dirty=%t, diharapkan %d bersih", step, v, dirty, want)
		}
		return nil
	}

	if err = r.Up(ctx, tmpURL); err != nil {
		return fmt.Errorf("roundtrip (up pertama): %w", err)
	}
	if err = check("up pertama", latest); err != nil {
		return err
	}
	if err = r.Down(ctx, tmpURL, int(latest)); err != nil {
		return fmt.Errorf("roundtrip (down semua): %w", err)
	}
	if err = check("down semua", 0); err != nil {
		return err
	}
	if err = leftovers(ctx, rawURL, name); err != nil {
		return err
	}
	if err = r.Up(ctx, tmpURL); err != nil {
		return fmt.Errorf("roundtrip (up kedua): %w", err)
	}
	return check("up kedua", latest)
}

// leftovers memastikan down(semua) tidak meninggalkan tabel atau fungsi di schema public
// (selain tabel versi golang-migrate). Migrasi down yang lupa DROP ketahuan di sini.
func leftovers(ctx context.Context, rawURL, dbName string) error {
	tmpURL, err := withDatabase(rawURL, "postgres", dbName)
	if err != nil {
		return err
	}
	conn, err := pgx.Connect(ctx, tmpURL)
	if err != nil {
		return redact(fmt.Errorf("roundtrip: gagal memeriksa sisa objek: %w", err), rawURL)
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()

	rows, err := conn.Query(ctx, `
		SELECT 'table ' || tablename FROM pg_tables
		 WHERE schemaname = 'public' AND tablename <> 'schema_migrations'
		UNION ALL
		SELECT 'function ' || p.proname FROM pg_proc p
		  JOIN pg_namespace n ON n.oid = p.pronamespace WHERE n.nspname = 'public'
		ORDER BY 1`)
	if err != nil {
		return redact(fmt.Errorf("roundtrip: gagal memeriksa sisa objek: %w", err), rawURL)
	}
	left, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return redact(fmt.Errorf("roundtrip: gagal membaca sisa objek: %w", err), rawURL)
	}
	if len(left) > 0 {
		return fmt.Errorf("roundtrip: objek tersisa setelah down semua: %s", strings.Join(left, ", "))
	}
	return nil
}
