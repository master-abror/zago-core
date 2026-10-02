package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// DefaultShutdownTimeout membatasi pengurasan request in-flight saat shutdown.
const DefaultShutdownTimeout = 30 * time.Second

// Server membungkus http.Server dengan graceful shutdown (docs/10 §12): berhenti menerima
// koneksi baru, menunggu request in-flight selesai (dibatasi timeout), lalu keluar.
type Server struct {
	srv             *http.Server
	log             *slog.Logger
	shutdownTimeout time.Duration
}

// NewServer membuat server. Tidak ada Read/WriteTimeout global karena WebSocket dan respons
// panjang datang kemudian; batas per request dipasang middleware Timeout.
func NewServer(addr string, h http.Handler, log *slog.Logger, shutdownTimeout time.Duration) *Server {
	if shutdownTimeout <= 0 {
		shutdownTimeout = DefaultShutdownTimeout
	}
	return &Server{
		srv: &http.Server{
			Addr:              addr,
			Handler:           h,
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       120 * time.Second,
		},
		log:             log,
		shutdownTimeout: shutdownTimeout,
	}
}

// ListenAndServe membuka listener lalu melayani sampai ctx dibatalkan.
func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.srv.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.srv.Addr, err)
	}
	return s.Serve(ctx, ln)
}

// Serve melayani di ln sampai ctx dibatalkan, lalu shutdown graceful. Mengembalikan nil bila
// seluruh request in-flight selesai dalam batas waktu; error bila server gagal atau batas
// waktu terlewati (koneksi tersisa ditutup paksa).
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	errCh := make(chan error, 1)
	go func() { errCh <- s.srv.Serve(ln) }()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	s.log.Info("shutdown dimulai: tidak menerima koneksi baru, menguras request in-flight",
		"timeout", s.shutdownTimeout.String())

	sctx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
	defer cancel()
	if err := s.srv.Shutdown(sctx); err != nil {
		_ = s.srv.Close()
		<-errCh
		return fmt.Errorf("graceful shutdown tidak tuntas dalam %s: %w", s.shutdownTimeout, err)
	}
	<-errCh
	return nil
}
