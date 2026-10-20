// Command api menjalankan server HTTP (+ WebSocket di M08). Logika ada di internal/app.
//
//	@title			Open Source Modular Application Platform API
//	@version		0.1
//	@description	API platform. Dokumen ini dihasilkan dari anotasi swag (`make generate`); sumber kebenaran bentuk field. Konvensi: docs/08.
//	@BasePath		/
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
	code := app.RunAPI(ctx, app.EnvMap(os.Environ()), infra.Deps{}, os.Stderr, nil)
	stop()
	os.Exit(code)
}
