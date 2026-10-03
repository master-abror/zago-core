// Command worker menjalankan pekerjaan latar (outbox relay, email, job terjadwal — M02+).
// Logika ada di internal/app.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/master-abror/zago-core/backend/internal/app"
	"github.com/master-abror/zago-core/backend/internal/infra"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := app.RunWorker(ctx, app.EnvMap(os.Environ()), infra.Deps{}, os.Stderr)
	stop()
	os.Exit(code)
}
