package app

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/master-abror/zago-core/backend/pkg/config"
)

const migrateUsage = "usage: migrate <up | down N | version | roundtrip | seed>"

// SeedResult adalah hasil seeder baris platform.
type SeedResult struct {
	Created bool   // true bila baris baru dibuat pada pemanggilan ini
	ID      string // id baris platform (UUID)
}

// MigrationRunner menjalankan migrasi dan seeder pada database. Implementasi nyata:
// internal/dbmigrate (golang-migrate + pgx); di sini hanya antarmuka agar alur perintah dan
// pesan error bisa dites dengan fake tanpa PostgreSQL (pola yang sama dengan Dependencies).
type MigrationRunner interface {
	Up(ctx context.Context, url string) error
	Down(ctx context.Context, url string, steps int) error
	Version(ctx context.Context, url string) (version uint, dirty bool, err error)
	Roundtrip(ctx context.Context, url string) error
	Seed(ctx context.Context, url string) (SeedResult, error)
}

// Migrate menjalankan cmd/migrate. Urutan: argumen -> konfigurasi (MIGRATION_DATABASE_URL, skema
// pgx5) -> perintah. Argumen/konfigurasi buruk selalu ExitConfig sebelum menyentuh database;
// kegagalan runtime ExitFailed. Pesan error tak pernah memuat URL (runner membersihkannya).
func Migrate(ctx context.Context, args []string, env map[string]string, runner MigrationRunner, stdout, stderr io.Writer) int {
	cmd, steps, ok := parseMigrateArgs(args, stderr)
	if !ok {
		return ExitConfig
	}
	cfg, err := config.LoadFrom(config.RoleMigrate, env)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "migrate: %v\n", err)
		return ExitConfig
	}
	url := cfg.MigrationDatabaseURL

	fail := func(err error) int {
		_, _ = fmt.Fprintf(stderr, "migrate %s: %v\n", cmd, err)
		return ExitFailed
	}
	switch cmd {
	case "up":
		if err := runner.Up(ctx, url); err != nil {
			return fail(err)
		}
		v, dirty, err := runner.Version(ctx, url)
		if err != nil {
			return fail(err)
		}
		_, _ = fmt.Fprintf(stdout, "migrate up: selesai, versi %d (dirty=%t)\n", v, dirty)
	case "down":
		if err := runner.Down(ctx, url, steps); err != nil {
			return fail(err)
		}
		v, dirty, err := runner.Version(ctx, url)
		if err != nil {
			return fail(err)
		}
		_, _ = fmt.Fprintf(stdout, "migrate down %d: selesai, versi %d (dirty=%t)\n", steps, v, dirty)
	case "version":
		v, dirty, err := runner.Version(ctx, url)
		if err != nil {
			return fail(err)
		}
		_, _ = fmt.Fprintf(stdout, "versi %d dirty=%t\n", v, dirty)
	case "roundtrip":
		if err := runner.Roundtrip(ctx, url); err != nil {
			return fail(err)
		}
		_, _ = fmt.Fprintln(stdout, "migrate roundtrip: up -> down(semua) -> up pada database sementara OK")
	case "seed":
		res, err := runner.Seed(ctx, url)
		if err != nil {
			return fail(err)
		}
		if res.Created {
			_, _ = fmt.Fprintf(stdout, "migrate seed: baris platform dibuat (id %s)\n", res.ID)
		} else {
			_, _ = fmt.Fprintf(stdout, "migrate seed: baris platform sudah ada (id %s), tidak ada perubahan\n", res.ID)
		}
	}
	return ExitOK
}

// parseMigrateArgs memvalidasi argumen; steps hanya bermakna untuk `down`.
func parseMigrateArgs(args []string, stderr io.Writer) (cmd string, steps int, ok bool) {
	usage := func() { _, _ = fmt.Fprintln(stderr, migrateUsage) }
	if len(args) == 0 {
		usage()
		return "", 0, false
	}
	switch args[0] {
	case "up", "version", "roundtrip", "seed":
		if len(args) != 1 {
			usage()
			return "", 0, false
		}
		return args[0], 0, true
	case "down":
		if len(args) != 2 {
			usage()
			return "", 0, false
		}
		n, err := strconv.Atoi(args[1])
		if err != nil || n < 1 {
			_, _ = fmt.Fprintln(stderr, "migrate down: N harus bilangan bulat >= 1")
			return "", 0, false
		}
		return "down", n, true
	default:
		_, _ = fmt.Fprintf(stderr, "migrate: perintah tidak dikenal %q\n%s\n", args[0], migrateUsage)
		return "", 0, false
	}
}
