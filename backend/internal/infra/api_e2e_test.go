package infra_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/app"
	"github.com/master-abror/zago-core/backend/internal/audit/repository"
	authrepo "github.com/master-abror/zago-core/backend/internal/auth/repository"
	"github.com/master-abror/zago-core/backend/internal/auth/service"
	"github.com/master-abror/zago-core/backend/internal/httpserver"
	"github.com/master-abror/zago-core/backend/internal/infra"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Ujung-ke-ujung T5f: composition root NYATA (app.RunAPI / app.RunWorker + infra.Deps) terhadap
// PostgreSQL 18 dan Redis 8 sungguhan, tanpa mock. Mencakup yang tak terjangkau tes tingkat handler:
// pool dan klien Redis dari infra, perekam audit yang dipasang composition root, echo hanya di mode
// development, dan metrik /metrics (HTTP, pool, outbox) terhadap pool dan database sungguhan.

var e2eClient = &http.Client{Timeout: 5 * time.Second}

const (
	e2eSessionSecret = "dev-only-change-me-dev-only-change-me"
	e2eCookieName    = "platform_session" // COOKIE_SECURE=false: tanpa prefiks __Host-
)

func e2eEnv(db *testpg.DB, redisURL string, extra map[string]string) map[string]string {
	env := map[string]string{
		"ENVIRONMENT":              "development",
		"DATABASE_URL":             testkit.PostgresURL(db, testpg.AppRole),
		"MAINTENANCE_DATABASE_URL": testkit.PostgresURL(db, testpg.MaintenanceRole),
		"REDIS_URL":                redisURL,
		"SESSION_SECRET":           e2eSessionSecret,
		"COOKIE_SECURE":            "false",
		"ALLOWED_ORIGINS":          "http://localhost:5173",
		"PUBLIC_BASE_URL":          "http://localhost:5173",
		"LOG_LEVEL":                "debug",
	}
	for k, v := range extra {
		env[k] = v
	}
	return env
}

// freeTCPPort meminta port kosong ke OS lalu melepasnya (jeda sempit sebelum dipakai ulang).
func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())
	return port
}

func stopper(codeCh <-chan int, cancel context.CancelFunc) func() int {
	var once sync.Once
	var code int
	return func() int {
		once.Do(func() { cancel(); code = <-codeCh })
		return code
	}
}

func launchAPI(t *testing.T, env map[string]string) (base string, stop func() int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	codeCh := make(chan int, 1)
	go func() { codeCh <- app.RunAPI(ctx, env, infra.Deps{}, testWriter{t}, ln) }()
	stop = stopper(codeCh, cancel)
	t.Cleanup(func() { stop() }) // jangan biarkan goroutine menulis log setelah tes selesai
	return "http://" + ln.Addr().String(), stop
}

func launchWorker(t *testing.T, env map[string]string) (stop func() int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	codeCh := make(chan int, 1)
	go func() { codeCh <- app.RunWorker(ctx, env, infra.Deps{}, testWriter{t}) }()
	stop = stopper(codeCh, cancel)
	t.Cleanup(func() { stop() })
	return stop
}

func httpDo(t *testing.T, req *http.Request) (int, string, http.Header) {
	t.Helper()
	resp, err := e2eClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(b), resp.Header
}

func httpGetText(t *testing.T, url string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)
	code, body, _ := httpDo(t, req)
	return code, body
}

func scrape(t *testing.T, port int) string {
	t.Helper()
	code, body := httpGetText(t, "http://127.0.0.1:"+strconv.Itoa(port)+"/metrics")
	require.Equal(t, http.StatusOK, code)
	return body
}

// sampleValue mengambil nilai satu deret persis (mis. `db_pool_max_connections{pool="postgres"}`).
func sampleValue(body, series string) (float64, bool) {
	for _, line := range strings.Split(body, "\n") {
		if rest, ok := strings.CutPrefix(line, series+" "); ok {
			v, err := strconv.ParseFloat(strings.TrimSpace(rest), 64)
			return v, err == nil
		}
	}
	return 0, false
}

func requireSample(t *testing.T, body, series string) float64 {
	t.Helper()
	v, ok := sampleValue(body, series)
	require.True(t, ok, "deret %s tidak ada; keluaran:\n%s", series, body)
	return v
}

func requirePortClosed(t *testing.T, port int) {
	t.Helper()
	require.Eventually(t, func() bool {
		c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 200*time.Millisecond)
		if err != nil {
			return true
		}
		_ = c.Close()
		return false
	}, 5*time.Second, 50*time.Millisecond, "port metrics harus tertutup setelah proses berhenti")
}

func requireReady(t *testing.T, base string) {
	t.Helper()
	require.Eventually(t, func() bool {
		resp, err := e2eClient.Get(base + "/health/ready")
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode == http.StatusOK
	}, 30*time.Second, 100*time.Millisecond, "/health/ready harus 200 dengan PostgreSQL dan Redis nyata")
}

// e2eSession adalah sesi nyata (baris sessions di PostgreSQL + entri Redis) milik pengguna nyata.
// Login belum ada di T4a; sesi dibuat lewat SessionService yang sama dengan yang dirakit
// composition root, lalu dipakai sebagai cookie + token CSRF.
type e2eSession struct {
	token  string
	csrf   string
	userID uuid.UUID
}

func newE2ESession(t *testing.T, db *testpg.DB, rs *testkit.RedisServer) e2eSession {
	t.Helper()
	u := testkit.NewUser(t, db)
	svc := service.NewSessionService(authrepo.NewStore(db.App), rs.Client(t),
		service.StaticTimeouts{Idle: 30 * time.Minute, Absolute: 12 * time.Hour}, nil)
	issued, err := svc.Create(context.Background(), service.CreateParams{UserID: u.ID})
	require.NoError(t, err)
	csrf, err := service.NewCSRF(e2eSessionSecret)
	require.NoError(t, err)
	return e2eSession{token: issued.Token, csrf: csrf.Token(issued.Session.ID), userID: u.ID}
}

func echoPost(t *testing.T, base, key, requestID string, sess e2eSession, body string) (int, string, http.Header) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+httpserver.EchoPath, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	req.Header.Set("X-Request-ID", requestID)
	req.Header.Set("Cookie", e2eCookieName+"="+sess.token)
	req.Header.Set("X-CSRF-Token", sess.csrf)
	return httpDo(t, req)
}

func TestAPIEndToEndThroughEchoWithRealPostgresRedisAndMetrics(t *testing.T) {
	db := testkit.NewDB(t)
	rs := testkit.Redis(t)
	metricsPort := freeTCPPort(t)
	base, stop := launchAPI(t, e2eEnv(db, rs.URL(), map[string]string{"METRICS_PORT": strconv.Itoa(metricsPort)}))
	requireReady(t, base)

	// 0) Tanpa sesi ditolak; X-Dev-Actor (pengganti autentikasi M02) tidak lagi memberi identitas.
	anon, err := http.NewRequest(http.MethodPost, base+httpserver.EchoPath, strings.NewReader(`{"message":"anonim"}`))
	require.NoError(t, err)
	anon.Header.Set("Content-Type", "application/json")
	anon.Header.Set("Idempotency-Key", "e2e-key-anon")
	anon.Header.Set("X-Dev-Actor", id.NewID().String())
	anonCode, anonBody, _ := httpDo(t, anon)
	require.Equal(t, http.StatusUnauthorized, anonCode, anonBody)
	require.Contains(t, anonBody, `"code":"authentication_required"`)

	// 1) Echo lewat composition root dengan sesi nyata: envelope, request_id, aktor; replay idempoten.
	sess := newE2ESession(t, db, rs)
	actor := sess.userID
	code, body, _ := echoPost(t, base, "e2e-key-0001", "e2e-req-0001", sess, `{"message":"ujung ke ujung"}`)
	require.Equal(t, http.StatusCreated, code, body)
	require.Contains(t, body, `"message":"ujung ke ujung"`)
	require.Contains(t, body, `"request_id":"e2e-req-0001"`)
	require.Contains(t, body, `"actor_id":"`+actor.String()+`"`)

	code, replayBody, replayHdr := echoPost(t, base, "e2e-key-0001", "e2e-req-0001", sess, `{"message":"ujung ke ujung"}`)
	require.Equal(t, http.StatusCreated, code)
	require.Equal(t, "true", replayHdr.Get("Idempotent-Replayed"))
	require.Equal(t, body, replayBody, "replay memutar ulang respons yang sama")

	// 2) Audit: tepat satu activity (replay tak menulis lagi), dibaca lewat query sqlc T5e.
	rows, err := repository.New(db.App).ListActivitiesByRequestID(context.Background(),
		repository.ListActivitiesByRequestIDParams{RequestID: "e2e-req-0001"})
	require.NoError(t, err)
	require.Len(t, rows, 1, "replay tidak boleh menulis activity kedua")
	a := rows[0]
	require.Equal(t, "kernel.echo", a.Action)
	require.Equal(t, "success", a.Result)
	require.NotNil(t, a.ActorUserID)
	require.Equal(t, actor, *a.ActorUserID)
	require.NotNil(t, a.ResourceType)
	require.Equal(t, "kernel_echo", *a.ResourceType)

	// 3) /metrics di port terpisah: HTTP per pola rute (dua request, status 201), pool nyata.
	echoSeries := `http_requests_total{method="POST",route="` + httpserver.EchoPath + `",status="201"}`
	require.Eventually(t, func() bool {
		v, ok := sampleValue(scrape(t, metricsPort), echoSeries)
		return ok && v == 2
	}, 5*time.Second, 50*time.Millisecond, "dua request echo harus terhitung per pola rute")

	m := scrape(t, metricsPort)
	require.Greater(t, requireSample(t, m, `db_pool_max_connections{pool="postgres"}`), 0.0)
	requireSample(t, m, `db_pool_acquired_connections{pool="postgres"}`)
	requireSample(t, m, `db_pool_idle_connections{pool="postgres"}`)
	require.NotContains(t, m, "outbox_events_dead_total", "metrik worker tidak ada di API")
	require.NotContains(t, m, "worker_job_")

	// 4) Gauge outbox dibaca dari database sungguhan saat scrape; tanpa worker, event tetap pending.
	require.Equal(t, 0.0, requireSample(t, m, "outbox_events_pending"))
	require.Equal(t, 0.0, requireSample(t, m, "outbox_oldest_pending_age_seconds"))
	_, err = db.Migrator.Exec(context.Background(),
		`INSERT INTO outbox_events (id, event_name, payload) VALUES ($1, 'core.user.created', '{}')`, id.NewID().String())
	require.NoError(t, err)
	m = scrape(t, metricsPort)
	require.Equal(t, 1.0, requireSample(t, m, "outbox_events_pending"))
	require.Greater(t, requireSample(t, m, "outbox_oldest_pending_age_seconds"), 0.0)

	// 5) /metrics tidak ada di port API; berhenti bersih menutup port metrics.
	code, _ = httpGetText(t, base+"/metrics")
	require.Equal(t, http.StatusNotFound, code)
	require.Equal(t, app.ExitOK, stop())
	requirePortClosed(t, metricsPort)
}

func TestWorkerExposesPoolAndDeadLetterMetricsWithRealPostgres(t *testing.T) {
	db := testkit.NewDB(t)
	metricsPort := freeTCPPort(t)
	// Redis tak dipakai worker (klien lazy): tak perlu container.
	stop := launchWorker(t, e2eEnv(db, "redis://localhost:6379", map[string]string{"WORKER_METRICS_PORT": strconv.Itoa(metricsPort)}))

	var m string
	require.Eventually(t, func() bool {
		resp, err := e2eClient.Get("http://127.0.0.1:" + strconv.Itoa(metricsPort) + "/metrics")
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		m = string(b)
		return resp.StatusCode == http.StatusOK
	}, 15*time.Second, 50*time.Millisecond)

	require.Greater(t, requireSample(t, m, `db_pool_max_connections{pool="postgres"}`), 0.0)
	require.Greater(t, requireSample(t, m, `db_pool_max_connections{pool="postgres_maintenance"}`), 0.0)
	require.Equal(t, 0.0, requireSample(t, m, "outbox_events_dead_total"))
	require.NotContains(t, m, "outbox_events_pending", "gauge outbox milik API, bukan worker (ADR-0017)")
	require.NotContains(t, m, "http_requests_total")

	require.Equal(t, app.ExitOK, stop())
	requirePortClosed(t, metricsPort)
}
