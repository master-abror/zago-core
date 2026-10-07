package infra_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/app"
	"github.com/master-abror/zago-core/backend/internal/infra"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// RunWorker nyata (infra.Deps) terhadap PostgreSQL 18 sungguhan: relay outbox berjalan di worker,
// memproses event pending, dan berhenti bersih saat ctx dibatalkan.
func TestWorkerRunsOutboxRelayEndToEnd(t *testing.T) {
	db := testkit.NewDB(t)
	env := map[string]string{
		"ENVIRONMENT":              "development",
		"DATABASE_URL":             testkit.PostgresURL(db, testpg.AppRole),
		"MAINTENANCE_DATABASE_URL": testkit.PostgresURL(db, testpg.MaintenanceRole),
		"REDIS_URL":                "redis://localhost:6379", // klien lazy: tak ada koneksi yang dibuat
		"SESSION_SECRET":           "dev-only-change-me-dev-only-change-me",
		"COOKIE_SECURE":            "false",
		"ALLOWED_ORIGINS":          "http://localhost:5173",
		"PUBLIC_BASE_URL":          "http://localhost:5173",
		"LOG_LEVEL":                "debug",
	}

	ctx, cancel := context.WithCancel(context.Background())
	codeCh := make(chan int, 1)
	go func() { codeCh <- app.RunWorker(ctx, env, infra.Deps{}, testWriter{t}) }()

	var once sync.Once
	var code int
	stop := func() int {
		once.Do(func() { cancel(); code = <-codeCh })
		return code
	}
	t.Cleanup(func() { stop() }) // jangan biarkan goroutine menulis log setelah tes selesai

	evID := id.NewID().String()
	_, err := db.Migrator.Exec(context.Background(),
		`INSERT INTO outbox_events (id, event_name, payload) VALUES ($1, 'core.user.created', '{}')`, evID)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		var status string
		err := db.App.QueryRow(context.Background(), `SELECT status FROM outbox_events WHERE id = $1`, evID).Scan(&status)
		return err == nil && status == "processed"
	}, 30*time.Second, 100*time.Millisecond, "relay di worker harus memproses event pending")

	require.Equal(t, app.ExitOK, stop())
}
