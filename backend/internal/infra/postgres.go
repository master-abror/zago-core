// Package infra berisi adapter tipis ke PostgreSQL (pgx/pgxpool) dan Redis (go-redis) yang
// memenuhi app.Dependencies. Tidak ada logika bisnis di sini; alur start/stop ada di
// internal/app dan dites dengan fake.
package infra

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"platform/backend/internal/app"
)

// Deps adalah implementasi app.Dependencies yang memakai pgxpool dan go-redis.
type Deps struct{}

var _ app.Dependencies = Deps{}

// Postgres membuat pool pgx. pgxpool.New bersifat lazy (tidak langsung terhubung), sehingga
// database yang belum siap tidak mencegah proses start; /health/ready yang melaporkannya.
// Error tidak menyertakan URL karena URL memuat password.
func (Deps) Postgres(ctx context.Context, name, url string) (app.Resource, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return app.Resource{}, fmt.Errorf("%s: URL PostgreSQL tidak valid atau pool gagal dibuat", name)
	}
	return app.Resource{
		Name:  name,
		Check: pool.Ping,
		Close: pool.Close,
	}, nil
}
