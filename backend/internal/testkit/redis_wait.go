package testkit

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// redisReadyTimeout membatasi penantian Redis menjawab PING setelah container hidup.
const redisReadyTimeout = 60 * time.Second

// waitRedisReady menunggu Redis di addr menjawab PING lewat jalur yang SAMA dengan yang dipakai tes
// (alamat host:port terpetakan), sampai timeout atau ctx batal. Ini menggantikan penantian port
// berbasis polling Docker API yang gagal saat Docker Desktop kewalahan padahal Redis sudah siap
// (ADR-0017). Memakai PING inline mentah, bukan go-redis, supaya tak ada negosiasi protokol
// (HELLO/CLIENT SETINFO) yang ikut memengaruhi hasil.
func waitRedisReady(ctx context.Context, addr string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var last error
	for {
		err := pingOnce(ctx, addr)
		if err == nil {
			return nil
		}
		// Percobaan yang terpotong batas waktu hanya membawa "context deadline exceeded"; simpan
		// alasan nyata dari percobaan sebelumnya supaya galat akhir tetap menjelaskan penyebabnya.
		if ctx.Err() == nil || last == nil {
			last = err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("redis %s tidak menjawab PING dalam %s: %w", addr, timeout, last)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func pingOnce(ctx context.Context, addr string) error {
	d := net.Dialer{Timeout: time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	_ = conn.SetDeadline(time.Now().Add(time.Second))
	if _, err := io.WriteString(conn, "PING\r\n"); err != nil {
		return err
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return err
	}
	if !strings.HasPrefix(line, "+PONG") {
		return fmt.Errorf("balasan PING tak terduga: %q", strings.TrimSpace(line))
	}
	return nil
}
