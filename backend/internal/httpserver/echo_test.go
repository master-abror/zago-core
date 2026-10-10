package httpserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/httpserver"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/pkg/logger"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

const (
	echoPath   = "/api/v1/_kernel/echo"
	itemsPath  = "/api/v1/_kernel/echo/items"
	testSecret = "0123456789abcdef0123456789abcdef-test-secret"
)

// fakeAudit mencatat setiap activity beserta context pemanggilnya (bukan database: perekam nyata
// diuji di echo_audit_test.go).
type fakeAudit struct {
	mu      sync.Mutex
	entries []modulesdk.ActivityEntry
	ctxs    []context.Context
}

func (f *fakeAudit) Record(ctx context.Context, e modulesdk.ActivityEntry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, e)
	f.ctxs = append(f.ctxs, ctx)
}

func (f *fakeAudit) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.entries)
}

type echoEnv struct {
	h     http.Handler
	audit *fakeAudit
	log   *bytes.Buffer
}

// newEcho merakit handler lengkap lewat httpserver.NewHandler dengan Redis sungguhan.
func newEcho(t *testing.T, rdb redis.UniversalClient, devEndpoints bool) echoEnv {
	t.Helper()
	var buf bytes.Buffer
	log := logger.New(&buf, slog.LevelDebug)
	aud := &fakeAudit{}
	kern, err := httpserver.NewKernelServices(httpserver.KernelDeps{
		Redis: rdb, Log: log, SessionSecret: testSecret, Audit: aud,
	})
	require.NoError(t, err)
	h := httpserver.NewHandler(httpserver.Options{Logger: log, Kernel: kern, DevEndpoints: devEndpoints})
	return echoEnv{h: h, audit: aud, log: &buf}
}

func liveEcho(t *testing.T) echoEnv {
	t.Helper()
	return newEcho(t, testkit.Redis(t).Client(t), true)
}

type reqOpt func(*http.Request)

func hdr(k, v string) reqOpt { return func(r *http.Request) { r.Header.Set(k, v) } }

func (e echoEnv) do(method, target, body string, opts ...reqOpt) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, rd)
	req.RemoteAddr = "192.0.2.10:4321"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, o := range opts {
		o(req)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func (e echoEnv) post(body string, opts ...reqOpt) *httptest.ResponseRecorder {
	return e.do(http.MethodPost, echoPath, body, opts...)
}

type envelope struct {
	Data  json.RawMessage `json:"data"`
	Meta  map[string]any  `json:"meta"`
	Error *struct {
		Code      string         `json:"code"`
		Message   string         `json:"message"`
		RequestID string         `json:"request_id"`
		Details   map[string]any `json:"details"`
	} `json:"error"`
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) envelope {
	t.Helper()
	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env), "body: %s", rec.Body.String())
	return env
}

func requireError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) envelope {
	t.Helper()
	require.Equal(t, status, rec.Code, "body: %s", rec.Body.String())
	env := decode(t, rec)
	require.NotNil(t, env.Error, "body: %s", rec.Body.String())
	require.Equal(t, code, env.Error.Code)
	return env
}

type item struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

func page(t *testing.T, rec *httptest.ResponseRecorder) (items []item, next any, hasMore bool) {
	t.Helper()
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	env := decode(t, rec)
	require.NoError(t, json.Unmarshal(env.Data, &items))
	pg, ok := env.Meta["pagination"].(map[string]any)
	require.True(t, ok, "meta.pagination wajib ada: %s", rec.Body.String())
	return items, pg["next_cursor"], pg["has_more"].(bool)
}

var actorA = uuid.MustParse("0198a000-0000-7000-8000-00000000000a")

func okHeaders(key string) []reqOpt {
	return []reqOpt{hdr("Idempotency-Key", key), hdr("X-Dev-Actor", actorA.String())}
}

func TestEchoIsNotMountedUnlessDevEndpointsEnabledWithKernel(t *testing.T) {
	rdb := testkit.Redis(t).Client(t)

	off := newEcho(t, rdb, false)
	requireError(t, off.post(`{"message":"hi"}`, okHeaders("key-off-0001")...), http.StatusNotFound, "resource_not_found")
	requireError(t, off.do(http.MethodGet, itemsPath, ""), http.StatusNotFound, "resource_not_found")

	// DevEndpoints menyala tetapi toolkit kernel tidak dirakit: tidak boleh panik, tetap tidak terpasang.
	var buf bytes.Buffer
	h := httpserver.NewHandler(httpserver.Options{Logger: logger.New(&buf, slog.LevelDebug), DevEndpoints: true})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, itemsPath, nil))
	requireError(t, rec, http.StatusNotFound, "resource_not_found")
}

func TestEchoCreateReturnsEnvelopeAndPropagatesRequestIDActorAndClient(t *testing.T) {
	e := liveEcho(t)

	rec := e.post(`{"message":"  halo dunia  "}`,
		append(okHeaders("key-create-0001"), hdr("X-Request-ID", "req-echo-0001"), hdr("User-Agent", "echo-test/1.0"))...)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Equal(t, "req-echo-0001", rec.Header().Get("X-Request-ID"))
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	var data struct {
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
		ActorID   string `json:"actor_id"`
	}
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &data))
	require.Equal(t, "halo dunia", data.Message, "pesan dipangkas")
	require.Equal(t, "req-echo-0001", data.RequestID)
	require.Equal(t, actorA.String(), data.ActorID)

	require.Equal(t, 1, e.audit.count())
	entry, ctx := e.audit.entries[0], e.audit.ctxs[0]
	require.Equal(t, "kernel.echo", entry.Action)
	require.Equal(t, "kernel_echo", entry.ResourceType)
	require.Equal(t, modulesdk.ResultSuccess, entry.Result)
	require.EqualValues(t, len("halo dunia"), entry.Metadata["message_length"])
	require.NotContains(t, entry.Metadata, "message", "isi pesan tidak masuk audit")
	require.Equal(t, "req-echo-0001", logger.RequestID(ctx))
	actor, ok := kernel.ActorFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, actorA, actor.UserID)
	client, ok := kernel.ClientFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "192.0.2.10", client.IP)
	require.Equal(t, "echo-test/1.0", client.UserAgent)
}

func TestEchoCreateRejectsMissingActorMissingKeyAndBadActor(t *testing.T) {
	e := liveEcho(t)

	rec := e.post(`{"message":"hi"}`, hdr("Idempotency-Key", "key-noactor-01"))
	env := requireError(t, rec, http.StatusUnauthorized, "authentication_required")
	require.Equal(t, rec.Header().Get("X-Request-ID"), env.Error.RequestID, "envelope error membawa request_id")

	rec = e.post(`{"message":"hi"}`, hdr("X-Dev-Actor", actorA.String()))
	env = requireError(t, rec, http.StatusBadRequest, "validation_failed")
	require.Contains(t, rec.Body.String(), "Idempotency-Key")

	rec = e.post(`{"message":"hi"}`, hdr("Idempotency-Key", "key-badactor-1"), hdr("X-Dev-Actor", "bukan-uuid"))
	requireError(t, rec, http.StatusBadRequest, "validation_failed")
	require.Contains(t, rec.Body.String(), "X-Dev-Actor")

	require.Zero(t, e.audit.count(), "handler tidak boleh jalan untuk request yang ditolak")
}

func TestEchoCreateValidatesBody(t *testing.T) {
	e := liveEcho(t)

	rec := e.post(`{"message":"   "}`, okHeaders("key-valid-0001")...)
	requireError(t, rec, http.StatusBadRequest, "validation_failed")
	require.Contains(t, rec.Body.String(), `"field":"message"`)
	require.Contains(t, rec.Body.String(), `"code":"required"`)

	rec = e.post(`{"message":"`+strings.Repeat("x", 201)+`"}`, okHeaders("key-valid-0002")...)
	requireError(t, rec, http.StatusBadRequest, "validation_failed")
	require.Contains(t, rec.Body.String(), `"code":"too_long"`)

	rec = e.post(`{"message":"ok","bukan_field":1}`, okHeaders("key-valid-0003")...)
	requireError(t, rec, http.StatusBadRequest, "validation_failed")

	rec = e.post(`{"message":"ok"}`, append(okHeaders("key-valid-0004"), hdr("Content-Type", "text/plain"))...)
	requireError(t, rec, http.StatusUnsupportedMediaType, "unsupported_media_type")

	require.Zero(t, e.audit.count())
}

func TestEchoIdempotencyReplaysAndRecordsOnce(t *testing.T) {
	e := liveEcho(t)
	body := `{"message":"sekali saja"}`

	first := e.post(body, append(okHeaders("key-replay-0001"), hdr("X-Request-ID", "req-first-0001"))...)
	require.Equal(t, http.StatusCreated, first.Code, first.Body.String())
	second := e.post(body, append(okHeaders("key-replay-0001"), hdr("X-Request-ID", "req-second-001"))...)

	require.Equal(t, http.StatusCreated, second.Code)
	require.Equal(t, "true", second.Header().Get("Idempotent-Replayed"))
	require.Empty(t, first.Header().Get("Idempotent-Replayed"))
	require.JSONEq(t, string(decode(t, first).Data), string(decode(t, second).Data), "respons yang sama persis diputar ulang")
	require.Equal(t, 1, e.audit.count(), "efek samping hanya sekali")

	conflict := e.post(`{"message":"beda isi"}`, okHeaders("key-replay-0001")...)
	requireError(t, conflict, http.StatusUnprocessableEntity, "idempotency_key_conflict")
	require.Equal(t, 1, e.audit.count())

	otherActor := e.post(body, hdr("Idempotency-Key", "key-replay-0001"),
		hdr("X-Dev-Actor", uuid.MustParse("0198a000-0000-7000-8000-00000000000b").String()))
	require.Equal(t, http.StatusCreated, otherActor.Code, "kunci terikat ke aktor: aktor lain tidak memutar ulang respons orang lain")
	require.Equal(t, 2, e.audit.count())
}

func TestEchoIdempotencyConcurrentSameKeyExecutesOnce(t *testing.T) {
	e := liveEcho(t)
	const n = 8
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = e.post(`{"message":"serentak"}`, okHeaders("key-concurrent-1")...).Code
		}()
	}
	wg.Wait()

	require.Equal(t, 1, e.audit.count(), "tepat satu eksekusi untuk satu kunci")
	for _, c := range codes {
		require.Contains(t, []int{http.StatusCreated, http.StatusConflict}, c)
	}
	require.Contains(t, codes, http.StatusCreated)
}

func TestEchoRateLimitHeadersThen429(t *testing.T) {
	e := liveEcho(t)

	first := e.do(http.MethodGet, itemsPath, "")
	require.Equal(t, http.StatusOK, first.Code)
	require.Equal(t, strconv.Itoa(httpserver.EchoRule.Limit), first.Header().Get("X-RateLimit-Limit"))
	require.Equal(t, strconv.Itoa(httpserver.EchoRule.Limit-1), first.Header().Get("X-RateLimit-Remaining"))
	require.NotEmpty(t, first.Header().Get("X-RateLimit-Reset"))

	for i := 1; i < httpserver.EchoRule.Limit; i++ {
		require.Equal(t, http.StatusOK, e.do(http.MethodGet, itemsPath, "").Code, "request ke-%d", i+1)
	}
	rec := e.do(http.MethodGet, itemsPath, "")
	requireError(t, rec, http.StatusTooManyRequests, "rate_limit_exceeded")
	require.Equal(t, "0", rec.Header().Get("X-RateLimit-Remaining"))
	secs, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, secs, 1)
}

func TestEchoItemsWalkAllPagesWithoutGapsOrDuplicates(t *testing.T) {
	e := liveEcho(t)

	var all []item
	target := itemsPath + "?limit=10"
	for pageNo := 1; ; pageNo++ {
		require.LessOrEqual(t, pageNo, 5, "paginasi tidak berhenti")
		items, next, hasMore := page(t, e.do(http.MethodGet, target, ""))
		all = append(all, items...)
		if !hasMore {
			require.Nil(t, next, "halaman terakhir: next_cursor null")
			require.Len(t, items, 5)
			break
		}
		require.Len(t, items, 10)
		require.NotEmpty(t, next)
		target = itemsPath + "?limit=10&cursor=" + next.(string)
	}

	require.Len(t, all, 25)
	seen := map[string]bool{}
	for i, it := range all {
		require.False(t, seen[it.ID], "duplikat %s", it.Name)
		seen[it.ID] = true
		if i > 0 {
			require.True(t, all[i-1].CreatedAt.After(it.CreatedAt), "urutan terbaru dulu")
		}
	}
	require.Equal(t, "item-025", all[0].Name)
	require.Equal(t, "item-001", all[24].Name)
}

func TestEchoItemsFilterAndLimitBounds(t *testing.T) {
	e := liveEcho(t)

	items, _, hasMore := page(t, e.do(http.MethodGet, itemsPath+"?q=item-02", ""))
	require.False(t, hasMore)
	require.Len(t, items, 6, "item-020..item-025")

	items, _, hasMore = page(t, e.do(http.MethodGet, itemsPath+"?limit=1000", ""))
	require.Len(t, items, 20, "limit dipotong ke maksimum endpoint (20)")
	require.True(t, hasMore)

	items, next, hasMore := page(t, e.do(http.MethodGet, itemsPath+"?q=tidak-ada", ""))
	require.Empty(t, items)
	require.NotNil(t, items, "koleksi kosong tampil sebagai [] bukan null")
	require.False(t, hasMore)
	require.Nil(t, next)

	requireError(t, e.do(http.MethodGet, itemsPath+"?limit=0", ""), http.StatusBadRequest, "validation_failed")
	requireError(t, e.do(http.MethodGet, itemsPath+"?limit=abc", ""), http.StatusBadRequest, "validation_failed")
}

func TestEchoItemsRejectTamperedAndMismatchedCursor(t *testing.T) {
	e := liveEcho(t)
	_, next, hasMore := page(t, e.do(http.MethodGet, itemsPath+"?limit=5&q=item-0", ""))
	require.True(t, hasMore)
	cursor := next.(string)

	tampered := cursor[:len(cursor)-2] + "AA"
	if tampered == cursor {
		tampered = cursor[:len(cursor)-2] + "BB"
	}
	rec := e.do(http.MethodGet, itemsPath+"?limit=5&q=item-0&cursor="+tampered, "")
	requireError(t, rec, http.StatusBadRequest, "validation_failed")
	require.Contains(t, rec.Body.String(), "invalid_cursor")

	rec = e.do(http.MethodGet, itemsPath+"?limit=5&q=item-1&cursor="+cursor, "")
	requireError(t, rec, http.StatusBadRequest, "validation_failed")
	require.Contains(t, rec.Body.String(), "cursor_mismatch", "cursor sah tidak boleh dipakai dengan filter lain")
}

func TestEchoWithRedisDownFailsClosedForIdempotencyAndOpenForRateLimit(t *testing.T) {
	dead := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 100 * time.Millisecond, MaxRetries: -1})
	t.Cleanup(func() { _ = dead.Close() })
	e := newEcho(t, dead, true)

	requireError(t, e.post(`{"message":"hi"}`, okHeaders("key-down-00001")...), http.StatusServiceUnavailable, "service_unavailable")
	require.Zero(t, e.audit.count(), "handler tidak dieksekusi saat Redis mati (fail closed)")

	items, _, _ := page(t, e.do(http.MethodGet, itemsPath, ""))
	require.Len(t, items, 10, "rate limit non-sensitif fail open: endpoint tetap melayani (limit bawaan 10)")
}

func TestEchoMethodNotAllowedUsesEnvelope(t *testing.T) {
	e := liveEcho(t)
	requireError(t, e.do(http.MethodDelete, echoPath, ""), http.StatusMethodNotAllowed, "method_not_allowed")
}
