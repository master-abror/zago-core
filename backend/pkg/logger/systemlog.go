package logger

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// systemLogKey adalah kunci atribut penanda. Penanda selalu dibuang sebelum record sampai ke
// handler JSON, sehingga tidak pernah muncul di stdout.
const systemLogKey = "_system_log"

// SystemLog menandai satu panggilan log agar JUGA ditulis ke system_logs (docs/14 §3), selain
// stdout. Contoh:
//
//	log.ErrorContext(ctx, "job gagal setelah semua percobaan", "error", err, logger.SystemLog("worker.job_failed"))
//
// Hanya log yang melewati level minimum logger yang diproses; system log dimaksudkan untuk
// warn/error. Kode kosong diabaikan (penanda tetap dibuang).
func SystemLog(eventCode string) slog.Attr { return slog.String(systemLogKey, eventCode) }

// SystemEntry adalah satu log bertanda SystemLog yang diteruskan ke SystemSink.
type SystemEntry struct {
	Level     slog.Level
	EventCode string
	Message   string
	// Fields berisi atribut milik panggilan log ini (grup diratakan "a.b", error menjadi teks).
	// Atribut yang dipasang lewat With()/WithGroup() tidak ikut. request_id dan trace_id
	// tidak ada di sini: sink membacanya dari context.
	Fields map[string]any
}

// SystemSink menerima log bertanda SystemLog. Implementasi nyata: systemlog.Writer (lewat
// Writer.Sink). Implementasi tidak boleh memblokir lama dan tidak mengembalikan error —
// kegagalan menulis system log tidak boleh mengganggu pemanggil.
type SystemSink interface {
	SystemLog(ctx context.Context, e SystemEntry)
}

// Gate adalah penghubung yang bisa dipasang SESUDAH logger dibuat. Logger dibuat paling awal
// (sebelum pool PostgreSQL ada), sedangkan sink butuh pool; Gate menutup celah itu: selama
// belum ada sink yang di-attach, log bertanda SystemLog hanya ke stdout. Aman dipakai
// serentak dari banyak goroutine. Gate nil sama dengan tanpa sink.
type Gate struct {
	sink atomic.Pointer[sinkHolder]
}

type sinkHolder struct{ s SystemSink }

// NewGate membuat Gate kosong (tanpa sink).
func NewGate() *Gate { return &Gate{} }

// Attach memasang sink. s nil sama dengan Detach.
func (g *Gate) Attach(s SystemSink) {
	if s == nil {
		g.sink.Store(nil)
		return
	}
	g.sink.Store(&sinkHolder{s: s})
}

// Detach melepas sink; log bertanda SystemLog berikutnya hanya ke stdout. Panggil sebelum
// menutup pool yang dipakai sink.
func (g *Gate) Detach() { g.sink.Store(nil) }

func (g *Gate) load() SystemSink {
	if g == nil {
		return nil
	}
	if h := g.sink.Load(); h != nil {
		return h.s
	}
	return nil
}

// Option mengubah perilaku logger.New.
type Option func(*contextHandler)

// WithSystemGate menghubungkan logger ke gate system log.
func WithSystemGate(g *Gate) Option { return func(h *contextHandler) { h.gate = g } }

// hasSystemLogMarker memeriksa cepat apakah record membawa penanda (kasus umum: tidak).
func hasSystemLogMarker(r slog.Record) bool {
	found := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == systemLogKey {
			found = true
			return false
		}
		return true
	})
	return found
}

// splitSystemLog membuang penanda dari record dan mengumpulkan atribut lain sebagai Fields.
func splitSystemLog(r slog.Record) (clean slog.Record, code string, fields map[string]any) {
	clean = slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	fields = map[string]any{}
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == systemLogKey {
			code = a.Value.String()
			return true
		}
		clean.AddAttrs(a)
		flattenAttr("", a, fields)
		return true
	})
	return clean, code, fields
}

// flattenAttr meratakan grup menjadi kunci "a.b" dan mengubah error menjadi teks.
func flattenAttr(prefix string, a slog.Attr, out map[string]any) {
	a.Value = a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return
	}
	key := prefix
	if a.Key != "" {
		if key != "" {
			key += "."
		}
		key += a.Key
	}
	if a.Value.Kind() == slog.KindGroup {
		for _, g := range a.Value.Group() {
			flattenAttr(key, g, out)
		}
		return
	}
	if key == "" {
		return
	}
	if err, ok := a.Value.Any().(error); ok {
		out[key] = err.Error()
		return
	}
	out[key] = a.Value.Any()
}

// deliver meneruskan entri ke sink. Panic di sink dipulihkan (logging tidak boleh menjatuhkan
// pemanggil) dan dilaporkan sebagai peringatan ke stdout.
func (h contextHandler) deliver(ctx context.Context, e SystemEntry) {
	s := h.gate.load()
	if s == nil {
		return
	}
	defer func() {
		if p := recover(); p != nil {
			rec := slog.NewRecord(time.Now(), slog.LevelWarn, "sink system log panik; log tetap tertulis ke stdout", 0)
			rec.AddAttrs(slog.String("event_code", e.EventCode), slog.Any("panic", p))
			_ = h.Handle(ctx, rec)
		}
	}()
	s.SystemLog(ctx, e)
}
