// Command api menjalankan server HTTP (+ WebSocket di M08). Logika ada di internal/app.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"platform/backend/internal/app"
	"platform/backend/internal/infra"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := app.RunAPI(ctx, app.EnvMap(os.Environ()), infra.Deps{}, os.Stderr, nil)
	stop()
	os.Exit(code)
}
