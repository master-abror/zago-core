// Package kernel adalah fondasi teknis yang dipakai semua domain: pool database, transaksi,
// outbox + event bus, dan HTTP toolkit (docs/10 §3, §6, §13). Tidak ada logika bisnis di sini.
package kernel

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolConfig mengonfigurasi satu pool PostgreSQL. Satu proses membuka satu pool per role DB
// (app_user untuk runtime, app_maintenance untuk worker) — docs/04 §13.
type PoolConfig struct {
	Name             string // label pendek ("app", "maintenance"); jadi application_name dan awalan pesan error
	URL              string // postgres://… (TIDAK pernah masuk pesan error: memuat password)
	MaxConns         int32
	MinConns         int32
	StatementTimeout time.Duration // batas server-side untuk SETIAP statement
}

// NewPool membuat pgxpool dengan statement_timeout dipasang di sisi server pada setiap koneksi.
// Seperti pgxpool.New, koneksi dibuat lazy: database yang belum siap dilaporkan oleh
// /health/ready, bukan mencegah proses start. Error tidak pernah memuat URL.
func NewPool(ctx context.Context, c PoolConfig) (*pgxpool.Pool, error) {
	if c.Name == "" {
		return nil, fmt.Errorf("kernel: PoolConfig.Name wajib diisi")
	}
	if c.MaxConns < 1 {
		return nil, fmt.Errorf("%s: MaxConns minimal 1", c.Name)
	}
	if c.MinConns < 0 || c.MinConns > c.MaxConns {
		return nil, fmt.Errorf("%s: MinConns harus 0..MaxConns", c.Name)
	}
	if c.StatementTimeout < time.Millisecond {
		return nil, fmt.Errorf("%s: StatementTimeout wajib diisi (≥ 1ms)", c.Name)
	}

	cfg, err := pgxpool.ParseConfig(c.URL)
	if err != nil {
		return nil, fmt.Errorf("%s: URL PostgreSQL tidak valid", c.Name)
	}
	cfg.MaxConns = c.MaxConns
	cfg.MinConns = c.MinConns
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = strconv.FormatInt(c.StatementTimeout.Milliseconds(), 10)
	cfg.ConnConfig.RuntimeParams["application_name"] = "zago-" + c.Name

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("%s: pool gagal dibuat", c.Name)
	}
	return pool, nil
}
