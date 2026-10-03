package app_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/app"
)

func devEnv() map[string]string {
	return map[string]string{
		"ENVIRONMENT":              "development",
		"DATABASE_URL":             "postgres://app_user:SECRET-DB-PW@localhost:5432/platform",
		"MAINTENANCE_DATABASE_URL": "postgres://app_maintenance:SECRET-MT-PW@localhost:5432/platform",
		"MIGRATION_DATABASE_URL":   "pgx5://app_migrator:SECRET-MG-PW@localhost:5432/platform",
		"REDIS_URL":                "redis://localhost:6379",
		"SESSION_SECRET":           "dev-only-change-me-dev-only-change-me",
		"COOKIE_SECURE":            "false",
		"ALLOWED_ORIGINS":          "http://localhost:5173",
		"PUBLIC_BASE_URL":          "http://localhost:5173",
		"LOG_LEVEL":                "debug",
	}
}

// fakeDeps mencatat panggilan dan urutan penutupan.
type fakeDeps struct {
	mu       sync.Mutex
	opened   []string
	closed   []string
	pgErr    error
	redisErr error
	pgCheck  func(context.Context) error
	redisChk func(context.Context) error
}

func (f *fakeDeps) res(name string, check func(context.Context) error) app.Resource {
	if check == nil {
		check = func(context.Context) error { return nil }
	}
	f.mu.Lock()
	f.opened = append(f.opened, name)
	f.mu.Unlock()
	return app.Resource{Name: name, Check: check, Close: func() {
		f.mu.Lock()
		f.closed = append(f.closed, name)
		f.mu.Unlock()
	}}
}

func (f *fakeDeps) Postgres(_ context.Context, name, _ string) (app.Resource, error) {
	if f.pgErr != nil {
		return app.Resource{}, f.pgErr
	}
	return f.res(name, f.pgCheck), nil
}

func (f *fakeDeps) Redis(_ context.Context, name, _ string) (app.Resource, error) {
	if f.redisErr != nil {
		return app.Resource{}, f.redisErr
	}
	return f.res(name, f.redisChk), nil
}

func (f *fakeDeps) snapshot() (opened, closed []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.opened...), append([]string(nil), f.closed...)
}

// ---------- RunAPI ----------

func TestAPIFailsFastOnBadConfigBeforeBindingOrOpeningAnything(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	env := devEnv()
	delete(env, "REDIS_URL")
	delete(env, "SESSION_SECRET")
	deps := &fakeDeps{}
	var stderr bytes.Buffer

	code := app.RunAPI(context.Background(), env, deps, &stderr, ln)

	require.Equal(t, app.ExitConfig, code)
	opened, _ := deps.snapshot()
	require.Empty(t, opened, "tidak boleh membuka dependensi bila konfigurasi tidak valid")
	require.Contains(t, stderr.String(), "REDIS_URL")
	require.Contains(t, stderr.String(), "SESSION_SECRET")
	for _, secret := range []string{"SECRET-DB-PW", "SECRET-MT-PW", "dev-only-change-me"} {
		require.NotContains(t, stderr.String(), secret)
	}

	// Listener yang diberikan tidak disentuh (tidak di-Serve, tidak ditutup): masih bisa di-dial.
	c, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second)
	require.NoError(t, err, "listener harus masih terbuka: proses keluar sebelum bind/serve")
	_ = c.Close()
}

func TestAPIWithEmptyEnvironmentExitsWithConfigError(t *testing.T) {
	var stderr bytes.Buffer
	code := app.RunAPI(context.Background(), map[string]string{}, &fakeDeps{}, &stderr, nil)
	require.Equal(t, app.ExitConfig, code)
	require.NotEmpty(t, stderr.String())
}

func startAPI(t *testing.T, deps *fakeDeps) (base string, stop func() int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	codeCh := make(chan int, 1)
	var stderr bytes.Buffer
	go func() { codeCh <- app.RunAPI(ctx, devEnv(), deps, &stderr, ln) }()
	t.Cleanup(cancel)

	base = "http://" + ln.Addr().String()
	require.Eventually(t, func() bool {
		resp, err := http.Get(base + "/health")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 3*time.Second, 20*time.Millisecond)

	return base, func() int {
		cancel()
		select {
		case c := <-codeCh:
			return c
		case <-time.After(5 * time.Second):
			t.Fatal("RunAPI tidak berhenti setelah ctx dibatalkan")
			return -1
		}
	}
}

func status(t *testing.T, url string) int {
	t.Helper()
	resp, err := http.Get(url)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func TestAPIServesHealthAndShutsDownClosingResourcesInReverseOrder(t *testing.T) {
	deps := &fakeDeps{}
	base, stop := startAPI(t, deps)

	require.Equal(t, http.StatusOK, status(t, base+"/health/live"))
	require.Equal(t, http.StatusOK, status(t, base+"/health/ready"))

	require.Equal(t, app.ExitOK, stop())

	opened, closed := deps.snapshot()
	require.Equal(t, []string{"postgres", "redis"}, opened)
	require.Equal(t, []string{"redis", "postgres"}, closed, "dependensi ditutup terbalik dari urutan dibuka")
}

func TestAPIReadyIs503WhenPostgresIsDown(t *testing.T) {
	var down bool
	var mu sync.Mutex
	deps := &fakeDeps{pgCheck: func(context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		if down {
			return errors.New("connection refused")
		}
		return nil
	}}
	base, stop := startAPI(t, deps)
	defer stop()

	require.Equal(t, http.StatusOK, status(t, base+"/health/ready"))
	mu.Lock()
	down = true
	mu.Unlock()
	require.Equal(t, http.StatusServiceUnavailable, status(t, base+"/health/ready"))
	require.Equal(t, http.StatusOK, status(t, base+"/health"), "liveness tidak boleh ikut gagal")
}

func TestAPIDependencyOpenFailureClosesWhatWasOpened(t *testing.T) {
	deps := &fakeDeps{redisErr: errors.New("redis url rusak")}
	var stderr bytes.Buffer
	code := app.RunAPI(context.Background(), devEnv(), deps, &stderr, nil)

	require.Equal(t, app.ExitFailed, code)
	opened, closed := deps.snapshot()
	require.Equal(t, []string{"postgres"}, opened)
	require.Equal(t, []string{"postgres"}, closed)
}

func TestAPIPortAlreadyInUseExitsWithErrorAndClosesResources(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = busy.Close() }()

	env := devEnv()
	env["PORT"] = strconv.Itoa(busy.Addr().(*net.TCPAddr).Port)

	deps := &fakeDeps{}
	var stderr bytes.Buffer
	// Bind ke :PORT bentrok dengan listener 127.0.0.1:PORT di Linux.
	code := app.RunAPI(context.Background(), env, deps, &stderr, nil)

	require.Equal(t, app.ExitFailed, code)
	_, closed := deps.snapshot()
	require.Equal(t, []string{"redis", "postgres"}, closed)
}

// ---------- RunWorker ----------

func TestWorkerRequiresMaintenanceDatabaseURL(t *testing.T) {
	env := devEnv()
	delete(env, "MAINTENANCE_DATABASE_URL")
	deps := &fakeDeps{}
	var stderr bytes.Buffer

	code := app.RunWorker(context.Background(), env, deps, &stderr)

	require.Equal(t, app.ExitConfig, code)
	require.Contains(t, stderr.String(), "MAINTENANCE_DATABASE_URL")
	opened, _ := deps.snapshot()
	require.Empty(t, opened)
}

func TestWorkerStopsOnContextCancelAndClosesResourcesInReverseOrder(t *testing.T) {
	deps := &fakeDeps{}
	ctx, cancel := context.WithCancel(context.Background())
	codeCh := make(chan int, 1)
	var stderr bytes.Buffer
	go func() { codeCh <- app.RunWorker(ctx, devEnv(), deps, &stderr) }()

	require.Eventually(t, func() bool { o, _ := deps.snapshot(); return len(o) == 3 }, 2*time.Second, 10*time.Millisecond)
	select {
	case <-codeCh:
		t.Fatal("worker berhenti sebelum diminta")
	case <-time.After(100 * time.Millisecond):
	}

	cancel()
	select {
	case code := <-codeCh:
		require.Equal(t, app.ExitOK, code)
	case <-time.After(3 * time.Second):
		t.Fatal("worker tidak berhenti")
	}
	opened, closed := deps.snapshot()
	require.Equal(t, []string{"postgres", "postgres_maintenance", "redis"}, opened)
	require.Equal(t, []string{"redis", "postgres_maintenance", "postgres"}, closed)
}

// ---------- Migrate ----------

func migrateEnv() map[string]string {
	return map[string]string{"MIGRATION_DATABASE_URL": "pgx5://app_migrator:SECRET-MG-PW@localhost:5432/platform"}
}

func TestMigrateSkeleton(t *testing.T) {
	for _, args := range [][]string{{"up"}, {"roundtrip"}, {"down", "1"}} {
		var out, errb bytes.Buffer
		code := app.Migrate(context.Background(), args, migrateEnv(), &out, &errb)
		require.Equal(t, app.ExitOK, code, "%v: %s", args, errb.String())
		require.Contains(t, out.String(), "tidak ada migrasi")
	}
}

func TestMigrateRejectsBadArguments(t *testing.T) {
	cases := [][]string{
		nil, {"sideways"}, {"down"}, {"down", "0"}, {"down", "-3"}, {"down", "abc"}, {"down", "1", "2"}, {"up", "extra"},
	}
	for _, args := range cases {
		var out, errb bytes.Buffer
		code := app.Migrate(context.Background(), args, migrateEnv(), &out, &errb)
		require.Equal(t, app.ExitConfig, code, "%v", args)
		require.NotEmpty(t, errb.String())
	}
}

func TestMigrateFailsFastWithoutOrWithWrongMigrationURL(t *testing.T) {
	for _, env := range []map[string]string{
		{},
		{"MIGRATION_DATABASE_URL": "postgres://app_migrator:SECRET-MG-PW@localhost:5432/platform"}, // skema bukan pgx5
	} {
		var out, errb bytes.Buffer
		code := app.Migrate(context.Background(), []string{"up"}, env, &out, &errb)
		require.Equal(t, app.ExitConfig, code)
		require.Contains(t, errb.String(), "MIGRATION_DATABASE_URL")
		require.NotContains(t, errb.String(), "SECRET-MG-PW")
		require.Empty(t, out.String())
	}
}

func TestEnvMap(t *testing.T) {
	m := app.EnvMap([]string{"A=1", "B=x=y", "BROKEN", "C="})
	require.Equal(t, map[string]string{"A": "1", "B": "x=y", "C": ""}, m)
}
