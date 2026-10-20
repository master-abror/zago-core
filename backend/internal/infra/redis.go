package infra

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"

	"github.com/master-abror/zago-core/backend/internal/app"
)

// Redis membuat klien go-redis. Seperti pgxpool, koneksi dibuat lazy. Error tidak menyertakan
// URL (bisa memuat password).
func (Deps) Redis(_ context.Context, name, url string) (app.Resource, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return app.Resource{}, fmt.Errorf("%s: URL Redis tidak valid", name)
	}
	// go-redis secara bawaan MENGABAIKAN deadline context pada I/O soket (hanya Read/WriteTimeout
	// klien yang berlaku, 5 dtk pada pengukuran). Tanpa ini batas 250 ms rate limiter, penyelesaian
	// idempotency, dan timeout readiness tak berlaku untuk Redis yang menerima koneksi tetapi macet
	// (ADR-0019). Redis yang mati total (koneksi ditolak) tidak terpengaruh.
	opts.ContextTimeoutEnabled = true
	client := redis.NewClient(opts)
	return app.Resource{
		Name:  name,
		Check: func(ctx context.Context) error { return client.Ping(ctx).Err() },
		Close: func() { _ = client.Close() },
		Redis: client,
	}, nil
}
