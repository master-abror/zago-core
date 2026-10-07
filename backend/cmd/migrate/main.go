// Command migrate menerapkan migrasi inti (golang-migrate sebagai library) dan seeder platform.
// Logika ada di internal/app dan internal/dbmigrate; berkas ini hanya wiring.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/master-abror/zago-core/backend/internal/app"
	"github.com/master-abror/zago-core/backend/internal/dbmigrate"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := app.Migrate(ctx, os.Args[1:], app.EnvMap(os.Environ()), dbmigrate.Runner{}, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
