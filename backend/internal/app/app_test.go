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

	"github.com/redis/go-redis/v9"
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
	specs    []app.PostgresSpec
	rdb      redis.UniversalClient // klien Redis yang dikembalikan fake (nil = tanpa klien)
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

func (f *fakeDeps) Postgres(_ context.Context, spec app.PostgresSpec) (app.Resource, error) {
	if f.pgErr != nil {
		return app.Resource{}, f.pgErr
	}
	f.mu.Lock()
	f.specs = append(f.specs, spec)
	f.mu.Unlock()
	return f.res(spec.Name, f.pgCheck), nil
}

func (f *fakeDeps) Redis(_ context.Context, name, _ string) (app.Resource, error) {
	if f.redisErr != nil {
		return app.Resource{}, f.redisErr
	}
	r := f.res(name, f.redisChk)
	r.Redis = f.rdb
	return r, nil
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

// testClient TIDAK memakai keep-alive. http.Transport bawaan kadang membuka koneksi cadangan
// (dial race) yang masuk pool tanpa pernah dipakai; di sisi server koneksi baru tanpa request itu
// baru dianggap idle setelah 5 dtk, sehingga Server.Shutdown tertahan melewati batas tunggu tes
// (diukur: 9 dari 30 percobaan gagal di bawah -race sebelum perbaikan ini).
var testClient = &http.Client{
	Transport: &http.Transport{DisableKeepAlives: true},
	Timeout:   5 * time.Second,
}

func startAPI(t *testing.T, deps *fakeDeps) (base string, stop func() int) {
	t.Helper()
	return startAPIEnv(t, deps, devEnv())
}

func startAPIEnv(t *testing.T, deps *fakeDeps, env map[string]string) (base string, stop func() int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	codeCh := make(chan int, 1)
	var stderr bytes.Buffer
	go func() { codeCh <- app.RunAPI(ctx, env, deps, &stderr, ln) }()
	t.Cleanup(cancel)

	base = "http://" + ln.Addr().String()
	require.Eventually(t, func() bool {
		resp, err := testClient.Get(base + "/health")
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
	resp, err := testClient.Get(url)
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

// fakeRunner mencatat panggilan dan mengembalikan hasil yang diatur tes (tanpa PostgreSQL).
type fakeRunner struct {
	calls   []string
	err     error
	version uint
	dirty   bool
	seed    app.SeedResult
}

func (f *fakeRunner) rec(s string) { f.calls = append(f.calls, s) }

func (f *fakeRunner) Up(_ context.Context, url string) error { f.rec("up " + url); return f.err }
func (f *fakeRunner) Down(_ context.Context, _ string, n int) error {
	f.rec("down " + strconv.Itoa(n))
	return f.err
}
func (f *fakeRunner) Version(_ context.Context, _ string) (uint, bool, error) {
	f.rec("version")
	return f.version, f.dirty, f.err
}
func (f *fakeRunner) Roundtrip(_ context.Context, _ string) error { f.rec("roundtrip"); return f.err }
func (f *fakeRunner) Seed(_ context.Context, _ string) (app.SeedResult, error) {
	f.rec("seed")
	return f.seed, f.err
}

func TestMigrateDispatchesCommands(t *testing.T) {
	cases := []struct {
		args      []string
		wantCalls []string
		wantOut   string
	}{
		{[]string{"up"}, []string{"up pgx5://app_migrator:SECRET-MG-PW@localhost:5432/platform", "version"}, "versi 33"},
		{[]string{"down", "2"}, []string{"down 2", "version"}, "migrate down 2"},
		{[]string{"version"}, []string{"version"}, "versi 33 dirty=false"},
		{[]string{"roundtrip"}, []string{"roundtrip"}, "roundtrip"},
		{[]string{"seed"}, []string{"seed"}, "baris platform dibuat (id abc)"},
	}
	for _, c := range cases {
		f := &fakeRunner{version: 33, seed: app.SeedResult{Created: true, ID: "abc"}}
		var out, errb bytes.Buffer
		code := app.Migrate(context.Background(), c.args, migrateEnv(), f, &out, &errb)
		require.Equal(t, app.ExitOK, code, "%v: %s", c.args, errb.String())
		require.Equal(t, c.wantCalls, f.calls, "%v", c.args)
		require.Contains(t, out.String(), c.wantOut)
	}
}

func TestMigrateSeedReportsExistingRow(t *testing.T) {
	f := &fakeRunner{seed: app.SeedResult{Created: false, ID: "abc"}}
	var out, errb bytes.Buffer
	require.Equal(t, app.ExitOK, app.Migrate(context.Background(), []string{"seed"}, migrateEnv(), f, &out, &errb))
	require.Contains(t, out.String(), "sudah ada")
}

func TestMigrateReportsRuntimeFailureAsExitFailed(t *testing.T) {
	for _, args := range [][]string{{"up"}, {"down", "1"}, {"version"}, {"roundtrip"}, {"seed"}} {
		f := &fakeRunner{err: errors.New("boom")}
		var out, errb bytes.Buffer
		code := app.Migrate(context.Background(), args, migrateEnv(), f, &out, &errb)
		require.Equal(t, app.ExitFailed, code, "%v", args)
		require.Contains(t, errb.String(), "boom")
		require.NotContains(t, errb.String(), "SECRET-MG-PW")
		require.Empty(t, out.String())
	}
}

func TestMigrateRejectsBadArguments(t *testing.T) {
	cases := [][]string{
		nil, {"sideways"}, {"down"}, {"down", "0"}, {"down", "-3"}, {"down", "abc"}, {"down", "1", "2"},
		{"up", "extra"}, {"version", "x"}, {"roundtrip", "x"}, {"seed", "x"},
	}
	for _, args := range cases {
		f := &fakeRunner{}
		var out, errb bytes.Buffer
		code := app.Migrate(context.Background(), args, migrateEnv(), f, &out, &errb)
		require.Equal(t, app.ExitConfig, code, "%v", args)
		require.NotEmpty(t, errb.String())
		require.Empty(t, f.calls, "argumen buruk tak boleh menyentuh database: %v", args)
	}
}

func TestMigrateFailsFastWithoutOrWithWrongMigrationURL(t *testing.T) {
	for _, env := range []map[string]string{
		{},
		{"MIGRATION_DATABASE_URL": "postgres://app_migrator:SECRET-MG-PW@localhost:5432/platform"}, // skema bukan pgx5
	} {
		f := &fakeRunner{}
		var out, errb bytes.Buffer
		code := app.Migrate(context.Background(), []string{"up"}, env, f, &out, &errb)
		require.Equal(t, app.ExitConfig, code)
		require.Contains(t, errb.String(), "MIGRATION_DATABASE_URL")
		require.NotContains(t, errb.String(), "SECRET-MG-PW")
		require.Empty(t, out.String())
		require.Empty(t, f.calls, "konfigurasi buruk tak boleh menyentuh database")
	}
}

func TestEnvMap(t *testing.T) {
	m := app.EnvMap([]string{"A=1", "B=x=y", "BROKEN", "C="})
	require.Equal(t, map[string]string{"A": "1", "B": "x=y", "C": ""}, m)
}

// ---------- Spesifikasi pool dari konfigurasi ----------

func TestWorkerOpensPoolsWithSizesAndStatementTimeoutFromConfig(t *testing.T) {
	env := devEnv()
	env["DB_MAX_CONNS"] = "7"
	env["DB_MAINTENANCE_MAX_CONNS"] = "3"
	env["DB_STATEMENT_TIMEOUT"] = "4s"

	deps := &fakeDeps{}
	ctx, cancel := context.WithCancel(context.Background())
	codeCh := make(chan int, 1)
	var stderr bytes.Buffer
	go func() { codeCh <- app.RunWorker(ctx, env, deps, &stderr) }()

	require.Eventually(t, func() bool { o, _ := deps.snapshot(); return len(o) == 3 }, 2*time.Second, 10*time.Millisecond)
	cancel()
	require.Equal(t, app.ExitOK, <-codeCh)

	deps.mu.Lock()
	defer deps.mu.Unlock()
	require.Len(t, deps.specs, 2)
	require.Equal(t, "postgres", deps.specs[0].Name)
	require.EqualValues(t, 7, deps.specs[0].MaxConns)
	require.Equal(t, 4*time.Second, deps.specs[0].StatementTimeout)
	require.Equal(t, "postgres_maintenance", deps.specs[1].Name)
	require.EqualValues(t, 3, deps.specs[1].MaxConns)
	require.Contains(t, deps.specs[0].URL, "app_user")
	require.Contains(t, deps.specs[1].URL, "app_maintenance")
}

// deadRedisClient mensimulasikan Redis yang tak terjangkau (ditolak seketika): rate limit
// non-sensitif fail open, jadi endpoint tanpa idempotency tetap melayani.
func deadRedisClient(t *testing.T) redis.UniversalClient {
	t.Helper()
	c := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 100 * time.Millisecond, MaxRetries: -1})
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func envFor(environment string) map[string]string {
	env := devEnv()
	env["ENVIRONMENT"] = environment
	return env
}

func TestAPIMountsEchoOnlyInDevelopmentAndTestWhenRedisClientExists(t *testing.T) {
	for environment, want := range map[string]int{
		"development": http.StatusOK,
		"test":        http.StatusOK,
		"staging":     http.StatusNotFound,
	} {
		t.Run(environment, func(t *testing.T) {
			base, stop := startAPIEnv(t, &fakeDeps{rdb: deadRedisClient(t)}, envFor(environment))
			require.Equal(t, want, status(t, base+"/api/v1/_kernel/echo/items"))
			require.Equal(t, app.ExitOK, stop())
		})
	}
}

func TestAPIDoesNotMountEchoWithoutRedisClient(t *testing.T) {
	base, stop := startAPI(t, &fakeDeps{}) // fake tanpa klien Redis
	defer stop()

	require.Equal(t, http.StatusOK, status(t, base+"/health/live"), "API tetap hidup")
	require.Equal(t, http.StatusNotFound, status(t, base+"/api/v1/_kernel/echo/items"))
}
