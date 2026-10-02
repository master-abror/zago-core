// Package health menyediakan handler liveness dan readiness (docs/10 §11).
//
//	GET /health, /health/live — proses hidup; tanpa pemeriksaan dependensi.
//	GET /health/ready          — setiap dependensi terjangkau (masing-masing dibatasi timeout).
//
// Endpoint ini tanpa autentikasi dan tidak membuka versi, hostname, atau teks error dependensi:
// detail kegagalan hanya masuk log server.
package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"
)

// DefaultCheckTimeout membatasi tiap pemeriksaan dependensi (docs/10 §11: 1–2 detik).
const DefaultCheckTimeout = 2 * time.Second

// Checker memeriksa satu dependensi. Check harus menghormati ctx.
type Checker interface {
	Name() string
	Check(ctx context.Context) error
}

type funcChecker struct {
	name string
	fn   func(context.Context) error
}

func (c funcChecker) Name() string                    { return c.name }
func (c funcChecker) Check(ctx context.Context) error { return c.fn(ctx) }

// NewChecker membungkus fungsi menjadi Checker bernama.
func NewChecker(name string, fn func(context.Context) error) Checker {
	return funcChecker{name: name, fn: fn}
}

type report struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Live selalu 200 selama proses mampu melayani request.
func Live() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, report{Status: "ok"})
	})
}

// Ready menjalankan semua checker secara paralel, masing-masing dengan timeout sendiri.
// 200 bila semua sehat; 503 bila ada yang gagal atau melewati timeout.
func Ready(log *slog.Logger, timeout time.Duration, checkers ...Checker) http.Handler {
	if timeout <= 0 {
		timeout = DefaultCheckTimeout
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		results := make(map[string]string, len(checkers))
		var mu sync.Mutex
		var wg sync.WaitGroup

		for _, c := range checkers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(r.Context(), timeout)
				defer cancel()

				// Check berjalan di goroutine sendiri agar checker yang mengabaikan ctx
				// tetap tidak bisa menahan respons lebih lama dari timeout.
				done := make(chan error, 1)
				go func() { done <- c.Check(ctx) }()

				var err error
				select {
				case err = <-done:
				case <-ctx.Done():
					err = ctx.Err()
				}

				state := "ok"
				if err != nil {
					state = "unavailable"
					log.WarnContext(r.Context(), "readiness check gagal", "check", c.Name(), "error", err)
				}
				mu.Lock()
				results[c.Name()] = state
				mu.Unlock()
			}()
		}
		wg.Wait()

		names := make([]string, 0, len(results))
		for n := range results {
			names = append(names, n)
		}
		sort.Strings(names)

		status, code := "ok", http.StatusOK
		for _, n := range names {
			if results[n] != "ok" {
				status, code = "unavailable", http.StatusServiceUnavailable
				break
			}
		}
		writeJSON(w, code, report{Status: status, Checks: results})
	})
}
