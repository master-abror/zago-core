// Command migrate menerapkan migrasi (golang-migrate sebagai library, M01). Di M00 hanya kerangka.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"platform/backend/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := app.Migrate(ctx, os.Args[1:], app.EnvMap(os.Environ()), os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
