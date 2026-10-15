package infra_test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/infra"
)

// silentServer menerima koneksi TCP tetapi tak pernah membalas: meniru Redis yang "hidup tapi macet"
// (partisi jaringan, failover tersendat). Berbeda dari Redis mati, koneksi TIDAK ditolak.
func silentServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
	})
	return ln.Addr().String()
}

// Tanpa ContextTimeoutEnabled, go-redis mengabaikan deadline context pada I/O soket dan baru menyerah
// setelah timeout bawaannya (5 detik pada pengukuran), sehingga batas 250 ms di rate limiter dan
// timeout readiness tidak berlaku untuk Redis yang macet (ADR-0019).
func TestRedisResourceHonorsContextDeadlineWhenServerStopsAnswering(t *testing.T) {
	res, err := infra.Deps{}.Redis(context.Background(), "redis", "redis://"+silentServer(t))
	require.NoError(t, err)
	t.Cleanup(res.Close)

	calls := map[string]func(context.Context) error{
		"perintah lewat klien bersama": func(ctx context.Context) error { return res.Redis.Ping(ctx).Err() },
		"pemeriksaan readiness":        res.Check,
	}
	for name, call := range calls {
		ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		start := time.Now()
		err := call(ctx)
		cancel()

		require.Error(t, err, name)
		// Batas atas longgar (3 dtk) supaya mesin lambat tak membuat tes gagal acak, namun jauh di
		// bawah 5 dtk bawaan go-redis yang diukur saat context diabaikan.
		require.Less(t, time.Since(start), 3*time.Second, "%s: deadline context harus dihormati", name)
	}
}
