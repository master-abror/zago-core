package httpserver

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authhttp "github.com/master-abror/zago-core/backend/internal/auth/transport/http"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/pkg/logger"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

// Endpoint dev/test /api/v1/_kernel/echo (ADR-0016). Ia ada untuk membuktikan toolkit kernel
// bekerja ujung-ke-ujung lewat NewHandler (envelope, request_id, idempotency, rate limit,
// cursor, audit) dan hanya terdaftar bila Options.DevEndpoints menyala — composition root
// menyalakannya hanya untuk ENVIRONMENT development atau test. Bukan bagian API produk.
const (
	// EchoPath adalah path dasar endpoint echo.
	EchoPath = "/api/v1/_kernel/echo"

	echoMaxMessageRunes = 200
	echoDefaultLimit    = 10
	echoMaxLimit        = 20
)

// EchoRule adalah batas laju endpoint echo, per IP klien.
var EchoRule = httpx.Rule{Name: "kernel.echo", Limit: 30, Window: time.Minute}

// echoNamespace menurunkan id item deterministik dari namanya.
var echoNamespace = uuid.MustParse("5f0c3c6e-3a52-4b43-9c3f-0e1b6f6f2a11")

type echoItem struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// echoItems adalah dataset tetap 25 item (item-001..item-025), urut terbaru dulu pada kunci
// keyset (created_at, id), supaya cursor bisa diuji tanpa database.
var echoItems = buildEchoItems(25)

func buildEchoItems(n int) []echoItem {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	items := make([]echoItem, 0, n)
	for i := 1; i <= n; i++ {
		name := fmt.Sprintf("item-%03d", i)
		items = append(items, echoItem{
			ID:        uuid.NewSHA1(echoNamespace, []byte(name)),
			Name:      name,
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}
	sort.Slice(items, func(a, b int) bool { return after(items[a], items[b]) })
	return items
}

// after melaporkan apakah a lebih baru dari b pada kunci (created_at, id).
func after(a, b echoItem) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.After(b.CreatedAt)
	}
	return a.ID.String() > b.ID.String()
}

type echoHandlers struct{ k *KernelServices }

// mountEcho memasang rute echo. Middleware dipasang per-endpoint (r.With) agar pola chi sudah
// final saat idempotency membentuk kunci Redis. POST butuh sesi (Auth.RequireAuth); identitas
// aktor datang dari Authenticate di grup API, bukan dari header apa pun. Tanpa Auth, POST
// menolak semua request dengan 401 (fail closed).
func mountEcho(r chi.Router, k *KernelServices, auth *authhttp.Middleware) {
	h := &echoHandlers{k: k}
	limit := k.RateLimiter.Middleware(EchoRule, func(req *http.Request) string {
		ip := ClientIP(req.Context())
		if ip == "" {
			return ""
		}
		return "ip:" + ip
	})
	r.With(limit).Get(EchoPath+"/items", h.items)
	r.With(limit, requireAuth(k.Responder, auth), k.Idempotency.Require()).Post(EchoPath, h.create)
}

// requireAuth mengembalikan Auth.RequireAuth, atau — bila autentikasi tidak dirakit — middleware
// yang menolak setiap request dengan 401 authentication_required.
func requireAuth(rs *httpx.Responder, auth *authhttp.Middleware) func(http.Handler) http.Handler {
	if auth != nil {
		return auth.RequireAuth
	}
	return func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rs.Error(w, r, httpx.AuthenticationRequired.New())
		})
	}
}

type echoRequest struct {
	Message string `json:"message"`
}

type echoResponse struct {
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	ActorID   string `json:"actor_id,omitempty"`
}

// create godoc
//
//	@Summary		Echo (dev/test)
//	@Description	Hanya development/test (ADR-0016): memperagakan envelope, idempotency, rate limit, dan audit. Butuh sesi (cookie) dan X-CSRF-Token. Bukan API produk.
//	@Tags			kernel-dev
//	@Accept			json
//	@Produce		json
//	@Param			Idempotency-Key	header		string		true	"Kunci idempotensi unik per aksi"
//	@Param			X-CSRF-Token	header		string		true	"Token CSRF sesi (GET /api/v1/auth/csrf)"
//	@Param			body			body		echoRequest	true	"Pesan"
//	@Success		201				{object}	httpx.Envelope[httpserver.echoResponse]
//	@Failure		400				{object}	httpx.ErrorEnvelope	"validation_failed"
//	@Failure		401				{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure		403				{object}	httpx.ErrorEnvelope	"csrf_failed"
//	@Failure		409				{object}	httpx.ErrorEnvelope	"idempotency_in_progress"
//	@Failure		422				{object}	httpx.ErrorEnvelope	"idempotency_key_conflict"
//	@Failure		429				{object}	httpx.ErrorEnvelope	"rate_limit_exceeded"
//	@Router			/api/v1/_kernel/echo [post]
func (h *echoHandlers) create(w http.ResponseWriter, r *http.Request) {
	var req echoRequest
	if err := httpx.DecodeJSON(w, r, &req, 0); err != nil {
		h.k.Responder.Error(w, r, err)
		return
	}
	msg := strings.TrimSpace(req.Message)
	var f httpx.Fields
	switch {
	case msg == "":
		f.Add("message", "required", "message is required")
	case utf8.RuneCountInString(msg) > echoMaxMessageRunes:
		f.Add("message", "too_long", "message must be at most 200 characters")
	}
	if err := f.Err(); err != nil {
		h.k.Responder.Error(w, r, err)
		return
	}

	if h.k.Audit != nil {
		h.k.Audit.Record(r.Context(), modulesdk.ActivityEntry{
			Action:       "kernel.echo",
			ResourceType: "kernel_echo",
			Result:       modulesdk.ResultSuccess,
			Metadata:     map[string]any{"message_length": len(msg)},
		})
	}

	out := echoResponse{Message: msg, RequestID: logger.RequestID(r.Context())}
	if a, ok := kernel.ActorFromContext(r.Context()); ok {
		out.ActorID = a.UserID.String()
	}
	h.k.Responder.Created(w, r, out, nil)
}

// items godoc
//
//	@Summary		Daftar item echo (dev/test)
//	@Description	Hanya development/test (ADR-0016): memperagakan pagination keyset dengan cursor bertanda tangan HMAC.
//	@Tags			kernel-dev
//	@Produce		json
//	@Param			limit	query		int		false	"Jumlah item (bawaan 10, maks 20)"
//	@Param			cursor	query		string	false	"Cursor dari meta.next_cursor"
//	@Param			q		query		string	false	"Prefiks nama"
//	@Success		200		{object}	httpx.Envelope[[]httpserver.echoItem]
//	@Failure		400		{object}	httpx.ErrorEnvelope	"validation_failed (termasuk cursor rusak)"
//	@Failure		429		{object}	httpx.ErrorEnvelope	"rate_limit_exceeded"
//	@Router			/api/v1/_kernel/echo/items [get]
func (h *echoHandlers) items(w http.ResponseWriter, r *http.Request) {
	pr, err := httpx.ParsePage(r, httpx.PageOptions{DefaultLimit: echoDefaultLimit, MaxLimit: echoMaxLimit})
	if err != nil {
		h.k.Responder.Error(w, r, err)
		return
	}
	q := r.URL.Query().Get("q")
	hash := httpx.FilterHash(map[string]string{"q": q})

	var pos *httpx.Position
	if pr.Cursor != "" {
		p, err := h.k.Cursors.Decode(pr.Cursor, httpx.Desc, hash)
		if err != nil {
			h.k.Responder.Error(w, r, err)
			return
		}
		pos = &p
	}

	rows := make([]echoItem, 0, pr.FetchLimit())
	for _, it := range echoItems {
		if !strings.HasPrefix(it.Name, q) {
			continue
		}
		if pos != nil && !before(it, *pos) {
			continue
		}
		rows = append(rows, it)
		if len(rows) == pr.FetchLimit() {
			break
		}
	}

	page, meta := httpx.Paginate(rows, pr.Limit, h.k.Cursors, httpx.Desc, hash, func(it echoItem) httpx.Position {
		return httpx.Position{CreatedAt: it.CreatedAt, ID: it.ID}
	})
	h.k.Responder.OK(w, r, page, meta)
}

// before melaporkan apakah it berada SETELAH pos pada urutan menurun (lebih lama dari pos).
func before(it echoItem, pos httpx.Position) bool {
	if !it.CreatedAt.Equal(pos.CreatedAt) {
		return it.CreatedAt.Before(pos.CreatedAt)
	}
	return it.ID.String() < pos.ID.String()
}
