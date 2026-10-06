// Package testpg menyediakan PostgreSQL 18 SUNGGUHAN untuk tes integrasi lewat testcontainers —
// tanpa mock database (docs/18, docs/22 §10.1). Satu container per paket tes (lazy), satu database
// per tes. Container diberi role aplikasi dari deploy/db/init/*.sql, sama dengan compose dev.
//
// Tanpa Docker: tes di-skip, KECUALI REQUIRE_DOCKER=1 (diset oleh make db-test, make
// migrate-roundtrip, make verify, dan CI) — maka tes gagal agar tak ada "hijau palsu".
package testpg

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/dbmigrate"
)

// Password dev; HARUS sama dengan deploy/db/init/00-roles.sql (container memakai berkas itu, jadi
// ketidakcocokan langsung terlihat sebagai gagal login).
const (
	MigratorRole    = "app_migrator"
	AppRole         = "app_user"
	MaintenanceRole = "app_maintenance"
)

var rolePassword = map[string]string{
	MigratorRole:    "migrator_dev_pw",
	AppRole:         "app_dev_pw",
	MaintenanceRole: "maintenance_dev_pw",
}

// instance adalah server yang berjalan: alamat host:port dan fungsi penghenti.
type instance struct {
	hostPort  string
	terminate func()
}

var (
	once      sync.Once
	shared    *Server
	sharedErr error
	counter   atomic.Int64
)

// Server adalah PostgreSQL bersama untuk satu paket tes.
type Server struct {
	hostPort string
	stop     func()

	tmplOnce sync.Once
	tmplErr  error
}

// Shared mengembalikan server bersama; menyalakannya pada pemanggilan pertama.
func Shared(t *testing.T) *Server {
	t.Helper()
	once.Do(func() {
		inst, err := startInstance(t)
		if err != nil {
			sharedErr = err
			return
		}
		shared = &Server{hostPort: inst.hostPort, stop: inst.terminate}
		sharedErr = shared.waitReady()
	})
	require.NoError(t, sharedErr)
	if shared == nil {
		// sync.Once menganggap selesai walau tes pertama di-skip (Docker tak sehat): skip juga di sini.
		t.Skip("PostgreSQL tes tidak tersedia (Docker tidak sehat)")
	}
	return shared
}

// Terminate menghentikan server bersama; panggil dari TestMain setelah m.Run().
func Terminate() {
	if shared != nil && shared.stop != nil {
		shared.stop()
	}
}

func (s *Server) urlFor(role, db, scheme string) string {
	u := url.URL{
		Scheme: scheme,
		User:   url.UserPassword(role, rolePassword[role]),
		Host:   s.hostPort,
		Path:   "/" + db,
	}
	if role == superUser {
		u.User = url.UserPassword(superUser, superPassword)
	}
	return u.String()
}

func (s *Server) waitReady() error {
	deadline := time.Now().Add(60 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		pool, err := pgxpool.New(ctx, s.urlFor(superUser, "postgres", "postgres"))
		if err == nil {
			last = pool.Ping(ctx)
			pool.Close()
			if last == nil {
				cancel()
				return nil
			}
		} else {
			last = err
		}
		cancel()
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("PostgreSQL tidak siap: %w", last)
}

// DB adalah satu database tes beserta pool untuk ketiga role aplikasi.
type DB struct {
	Name        string
	Migrator    *pgxpool.Pool // app_migrator: DDL
	App         *pgxpool.Pool // app_user: runtime
	Maintenance *pgxpool.Pool // app_maintenance: retensi/anonimisasi
	server      *Server
}

// MigratorURL mengembalikan URL pgx5:// (skema cmd/migrate) untuk database ini.
func (d *DB) MigratorURL() string { return d.server.urlFor(MigratorRole, d.Name, "pgx5") }

// RoleURL mengembalikan URL pgx5:// untuk role tertentu ke database ini.
func (d *DB) RoleURL(role string) string { return d.server.urlFor(role, d.Name, "pgx5") }

// RoleURL mengembalikan URL pgx5:// untuk role tertentu ke database `platform` (dipakai tes
// Roundtrip, yang membuat database sementaranya sendiri di server ini).
func (s *Server) RoleURL(role string) string { return s.urlFor(role, "platform", "pgx5") }

// NewDB membuat database baru yang SUDAH dimigrasi sampai head (klon dari template yang
// dimigrasi sekali per paket). Dihapus otomatis saat tes selesai.
func (s *Server) NewDB(t *testing.T) *DB {
	t.Helper()
	s.tmplOnce.Do(func() { s.tmplErr = s.buildTemplate() })
	require.NoError(t, s.tmplErr, "gagal membangun template database termigrasi")
	return s.createDB(t, "TEMPLATE "+templateName)
}

// EmptyDB membuat database kosong (milik app_migrator) untuk tes yang menjalankan migrasi sendiri.
func (s *Server) EmptyDB(t *testing.T) *DB {
	t.Helper()
	return s.createDB(t, "")
}

const templateName = "tmpl_migrated"

func (s *Server) superPool(ctx context.Context) (*pgxpool.Pool, error) {
	return pgxpool.New(ctx, s.urlFor(superUser, "postgres", "postgres"))
}

func (s *Server) buildTemplate() error {
	ctx := context.Background()
	sp, err := s.superPool(ctx)
	if err != nil {
		return err
	}
	defer sp.Close()
	// Idempoten: bila server dipakai ulang (bukan container baru), buang sisa template sebelumnya.
	if _, err = sp.Exec(ctx, "DROP DATABASE IF EXISTS "+templateName+" WITH (FORCE)"); err != nil {
		return err
	}
	if _, err = sp.Exec(ctx, "CREATE DATABASE "+templateName+" OWNER "+MigratorRole); err != nil {
		return err
	}
	// Meniru deploy/db/init/00-roles.sql untuk schema public di database ini.
	tp, err := pgxpool.New(ctx, s.urlFor(superUser, templateName, "postgres"))
	if err != nil {
		return err
	}
	for _, stmt := range []string{
		"ALTER SCHEMA public OWNER TO " + MigratorRole,
		"REVOKE ALL ON SCHEMA public FROM PUBLIC",
		"GRANT USAGE ON SCHEMA public TO " + AppRole + ", " + MaintenanceRole,
	} {
		if _, err = tp.Exec(ctx, stmt); err != nil {
			tp.Close()
			return err
		}
	}
	tp.Close()
	if err = (dbmigrate.Runner{}).Up(ctx, s.urlFor(MigratorRole, templateName, "pgx5")); err != nil {
		return err
	}
	// CREATE DATABASE ... TEMPLATE menolak bila template punya koneksi aktif; Up sudah menutupnya.
	return nil
}

func (s *Server) createDB(t *testing.T, clause string) *DB {
	t.Helper()
	ctx := context.Background()
	name := fmt.Sprintf("t_%d_%d", time.Now().UnixNano()%1_000_000_000, counter.Add(1))

	sp, err := s.superPool(ctx)
	require.NoError(t, err)
	_, err = sp.Exec(ctx, "CREATE DATABASE "+name+" OWNER "+MigratorRole+" "+clause)
	require.NoError(t, err)

	d := &DB{Name: name, server: s}
	open := func(role string) *pgxpool.Pool {
		p, perr := pgxpool.New(ctx, s.urlFor(role, name, "postgres"))
		require.NoError(t, perr)
		return p
	}
	d.Migrator, d.App, d.Maintenance = open(MigratorRole), open(AppRole), open(MaintenanceRole)

	t.Cleanup(func() {
		d.Migrator.Close()
		d.App.Close()
		d.Maintenance.Close()
		_, _ = sp.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		sp.Close()
	})
	return d
}
