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
	client := redis.NewClient(opts)
	return app.Resource{
		Name:  name,
		Check: func(ctx context.Context) error { return client.Ping(ctx).Err() },
		Close: func() { _ = client.Close() },
		Redis: client,
	}, nil
}
