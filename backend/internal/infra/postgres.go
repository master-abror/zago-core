// Package infra berisi adapter tipis ke PostgreSQL (pgx/pgxpool) dan Redis (go-redis) yang
// memenuhi app.Dependencies. Tidak ada logika bisnis di sini; alur start/stop ada di
// internal/app dan dites dengan fake.
package infra

import (
	"context"

	"github.com/master-abror/zago-core/backend/internal/app"
	"github.com/master-abror/zago-core/backend/internal/kernel"
)

// Deps adalah implementasi app.Dependencies yang memakai pgxpool dan go-redis.
type Deps struct{}

var _ app.Dependencies = Deps{}

// Postgres membuat pool pgx lewat kernel.NewPool (statement_timeout dipasang di server).
// Pembuatan pool bersifat lazy (tidak langsung terhubung), sehingga database yang belum siap
// tidak mencegah proses start; /health/ready yang melaporkannya. Error tidak menyertakan URL
// karena URL memuat password.
func (Deps) Postgres(ctx context.Context, spec app.PostgresSpec) (app.Resource, error) {
	pool, err := kernel.NewPool(ctx, kernel.PoolConfig{
		Name:             spec.Name,
		URL:              spec.URL,
		MaxConns:         spec.MaxConns,
		StatementTimeout: spec.StatementTimeout,
	})
	if err != nil {
		return app.Resource{}, err
	}
	return app.Resource{
		Name:  spec.Name,
		Check: pool.Ping,
		Close: pool.Close,
		Pool:  pool,
	}, nil
}
