package app

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/master-abror/zago-core/backend/pkg/config"
)

const migrateUsage = "usage: migrate <up | down N | roundtrip>"

// Migrate menjalankan cmd/migrate. KERANGKA M00: konfigurasi (MIGRATION_DATABASE_URL, skema
// pgx5://) divalidasi dan perintah diurai, tetapi belum ada migrasi untuk diterapkan; golang-migrate
// dihubungkan di M01 bersama migrasi pertama (docs/21). Karena itu `up` dan `roundtrip`
// berhasil tanpa menyentuh database.
func Migrate(_ context.Context, args []string, env map[string]string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, migrateUsage)
		return ExitConfig
	}
	switch args[0] {
	case "up", "roundtrip":
		if len(args) != 1 {
			_, _ = fmt.Fprintln(stderr, migrateUsage)
			return ExitConfig
		}
	case "down":
		if len(args) != 2 {
			_, _ = fmt.Fprintln(stderr, migrateUsage)
			return ExitConfig
		}
		if n, err := strconv.Atoi(args[1]); err != nil || n < 1 {
			_, _ = fmt.Fprintln(stderr, "migrate down: N harus bilangan bulat >= 1")
			return ExitConfig
		}
	default:
		_, _ = fmt.Fprintf(stderr, "migrate: perintah tidak dikenal %q\n%s\n", args[0], migrateUsage)
		return ExitConfig
	}

	if _, err := config.LoadFrom(config.RoleMigrate, env); err != nil {
		_, _ = fmt.Fprintf(stderr, "migrate: %v\n", err)
		return ExitConfig
	}
	_, _ = fmt.Fprintf(stdout, "migrate %s: tidak ada migrasi (migrasi pertama datang di M01)\n", args[0])
	return ExitOK
}
