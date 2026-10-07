package dbmigrate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/master-abror/zago-core/backend/internal/app"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// PlatformVersion dicatat di platforms.version saat baris dibuat. Diganti lewat -ldflags saat
// rilis (M15); sinkronisasi saat upgrade bukan bagian M01.
var PlatformVersion = "0.1.0-dev"

// platformSeedLock adalah kunci advisory (per transaksi) yang menserialkan seeder: tanpa ini dua
// proses bersamaan yang membangkitkan installation_key berbeda akan menghasilkan dua baris platform.
const platformSeedLock int64 = 0x7a61676f01 // "zago" + 01

// Seed memastikan TEPAT SATU baris platform ada (docs/04 §16, langkah 1). Idempoten: bila sudah
// ada, tidak ada yang berubah dan id yang ada dikembalikan. Katalog permission, role sistem, dan
// admin pertama bukan bagian M01 (M04 dan M03).
func (Runner) Seed(ctx context.Context, rawURL string) (app.SeedResult, error) {
	pgURL, err := asPostgres(rawURL)
	if err != nil {
		return app.SeedResult{}, err
	}
	conn, err := pgx.Connect(ctx, pgURL)
	if err != nil {
		return app.SeedResult{}, redact(fmt.Errorf("seed: gagal terhubung: %w", err), rawURL)
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()

	res, err := ensurePlatform(ctx, conn)
	if err != nil {
		return app.SeedResult{}, redact(fmt.Errorf("seed: %w", err), rawURL)
	}
	return res, nil
}

func ensurePlatform(ctx context.Context, conn *pgx.Conn) (res app.SeedResult, err error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return res, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()

	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", platformSeedLock); err != nil {
		return res, err
	}

	var existing string
	switch qerr := tx.QueryRow(ctx, "SELECT id::text FROM platforms ORDER BY created_at, id LIMIT 1").Scan(&existing); {
	case qerr == nil:
		if err = tx.Commit(ctx); err != nil {
			return res, err
		}
		return app.SeedResult{Created: false, ID: existing}, nil
	case !errors.Is(qerr, pgx.ErrNoRows):
		return res, qerr
	}

	key := make([]byte, 16)
	if _, err = rand.Read(key); err != nil {
		return res, err
	}
	newID := id.NewID().String()
	// SQL persis docs/04 §16; $1 dan $2 dibangkitkan sekali oleh seeder.
	if _, err = tx.Exec(ctx, `
		INSERT INTO platforms (id, name, installation_key, version, status)
		VALUES ($1, 'Local Installation', $2, $3, 'active')
		ON CONFLICT (installation_key) DO NOTHING`,
		newID, "inst_"+hex.EncodeToString(key), PlatformVersion); err != nil {
		return res, err
	}
	if err = tx.Commit(ctx); err != nil {
		return res, err
	}
	return app.SeedResult{Created: true, ID: newID}, nil
}
