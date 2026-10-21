// Package config membaca konfigurasi dari environment dan memvalidasinya SEKALI saat start
// (docs/10 §8). Nilai yang hilang/tidak valid menggagalkan proses SEBELUM port di-bind.
// Nilai konfigurasi tidak pernah masuk pesan error maupun log; hanya nama variabelnya.
package config

import (
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"

	"github.com/master-abror/zago-core/backend/pkg/logger"
	"github.com/master-abror/zago-core/backend/pkg/security"
)

// Role menentukan variabel mana yang wajib. Satu struct Config dipakai tiga binary, tetapi
// tiap binary hanya butuh sebagian (mis. cmd/migrate tidak butuh Redis) — lihat ADR-0003.
type Role string

const (
	RoleAPI     Role = "api"
	RoleWorker  Role = "worker"
	RoleMigrate Role = "migrate"
)

// Config adalah seluruh konfigurasi proses (docs/10 §8). Tidak ada tag `required`: kewajiban
// ditentukan per Role di Validate.
type Config struct {
	// data stores
	DatabaseURL            string `env:"DATABASE_URL"`             // role app_user
	MaintenanceDatabaseURL string `env:"MAINTENANCE_DATABASE_URL"` // role app_maintenance (worker)
	MigrationDatabaseURL   string `env:"MIGRATION_DATABASE_URL"`   // role app_migrator (cmd/migrate)
	RedisURL               string `env:"REDIS_URL"`

	// pool PostgreSQL (docs/10 §6, M02). Batas statement berlaku di sisi server untuk SETIAP query.
	DBMaxConns            int           `env:"DB_MAX_CONNS" envDefault:"10"`
	DBMaintenanceMaxConns int           `env:"DB_MAINTENANCE_MAX_CONNS" envDefault:"2"`
	DBStatementTimeout    time.Duration `env:"DB_STATEMENT_TIMEOUT" envDefault:"15s"`

	// security
	SessionSecret    string   `env:"SESSION_SECRET"` // ≥ 32 byte
	CookieName       string   `env:"COOKIE_NAME" envDefault:"platform_session"`
	CookieSecure     bool     `env:"COOKIE_SECURE" envDefault:"true"`
	AllowedOrigins   []string `env:"ALLOWED_ORIGINS"`
	TrustedProxies   []string `env:"TRUSTED_PROXIES"` // CIDR yang X-Forwarded-*-nya dipercaya
	MFAEncryptionKey string   `env:"MFA_ENCRYPTION_KEY"`

	// hash password Argon2id (docs/05 §4.1, I-04). Setel ulang dengan benchmark di perangkat keras
	// deploy (target 50–150 ms per hash). Hash lama tetap sah; rehash terjadi saat login berikutnya.
	PasswordArgon2MemoryKiB   int `env:"PASSWORD_ARGON2_MEMORY_KIB" envDefault:"65536"`
	PasswordArgon2Time        int `env:"PASSWORD_ARGON2_TIME" envDefault:"3"`
	PasswordArgon2Parallelism int `env:"PASSWORD_ARGON2_PARALLELISM" envDefault:"2"`
	PasswordHashConcurrency   int `env:"PASSWORD_HASH_CONCURRENCY" envDefault:"0"` // 0 = jumlah CPU
	PasswordMinLength         int `env:"PASSWORD_MIN_LENGTH" envDefault:"12"`

	// sesi (docs/05 §12.1): dua jam independen. Idle = sejak aktivitas terakhir; absolut = sejak
	// sesi dibuat, tidak bisa diperpanjang. Nilai ini bawaan; deployment yang lebih ketat dapat
	// menggantinya lewat sumber timeout (settings platform) tanpa mengubah kode sesi.
	SessionIdleTimeout     time.Duration `env:"SESSION_IDLE_TIMEOUT" envDefault:"30m"`
	SessionAbsoluteTimeout time.Duration `env:"SESSION_ABSOLUTE_TIMEOUT" envDefault:"12h"`

	// server
	Port          int    `env:"PORT" envDefault:"8080"`
	Environment   string `env:"ENVIRONMENT" envDefault:"development"`
	LogLevel      string `env:"LOG_LEVEL" envDefault:"info"`
	PublicBaseURL string `env:"PUBLIC_BASE_URL"`

	// email
	SMTPHost string `env:"SMTP_HOST"`
	SMTPPort int    `env:"SMTP_PORT" envDefault:"1025"`
	SMTPUser string `env:"SMTP_USER"`
	SMTPPass string `env:"SMTP_PASS"`
	SMTPFrom string `env:"SMTP_FROM"`

	// storage and telemetry
	StoragePath  string `env:"STORAGE_PATH" envDefault:"./data/uploads"`
	OTLPEndpoint string `env:"OTEL_EXPORTER_OTLP_ENDPOINT"`

	// /metrics Prometheus (docs/14 §5, ADR-0016/0017): port TERPISAH dari PORT, tidak pernah publik.
	// 0 = nonaktif. API membaca METRICS_PORT, worker membaca WORKER_METRICS_PORT (dua variabel
	// karena `make dev` menjalankan keduanya di satu host dengan .env yang sama).
	MetricsPort       int `env:"METRICS_PORT" envDefault:"0"`
	WorkerMetricsPort int `env:"WORKER_METRICS_PORT" envDefault:"0"`
}

// Error mengumpulkan SEMUA masalah validasi sekaligus (nama variabel saja, tanpa nilai).
type Error struct {
	Problems []string
}

func (e *Error) Error() string {
	return "konfigurasi tidak valid: " + strings.Join(e.Problems, "; ")
}

// Load membaca environment proses lalu memvalidasi untuk role tertentu.
func Load(role Role) (*Config, error) {
	var c Config
	if err := env.Parse(&c); err != nil {
		return nil, fmt.Errorf("konfigurasi tidak valid: gagal membaca environment: %w", err)
	}
	return finish(&c, role)
}

// LoadFrom sama dengan Load tetapi membaca dari map (untuk tes; tidak menyentuh os.Environ).
func LoadFrom(role Role, environment map[string]string) (*Config, error) {
	var c Config
	if err := env.ParseWithOptions(&c, env.Options{Environment: environment}); err != nil {
		return nil, fmt.Errorf("konfigurasi tidak valid: gagal membaca environment: %w", err)
	}
	return finish(&c, role)
}

func finish(c *Config, role Role) (*Config, error) {
	c.AllowedOrigins = cleanList(c.AllowedOrigins)
	c.TrustedProxies = cleanList(c.TrustedProxies)
	if err := c.Validate(role); err != nil {
		return nil, err
	}
	return c, nil
}

// IsProduction melaporkan apakah ENVIRONMENT=production.
func (c *Config) IsProduction() bool { return c.Environment == "production" }

// DevEndpointsEnabled melaporkan apakah endpoint dev/test (mis. /api/v1/_kernel/echo) boleh
// terdaftar: hanya ENVIRONMENT development atau test. Staging dan production tidak pernah
// (ADR-0016).
func (c *Config) DevEndpointsEnabled() bool {
	return c.Environment == "development" || c.Environment == "test"
}

// MetricsPortFor mengembalikan port /metrics untuk role (0 = nonaktif): METRICS_PORT untuk API,
// WORKER_METRICS_PORT untuk worker. Role lain tidak punya /metrics.
func (c *Config) MetricsPortFor(role Role) int {
	switch role {
	case RoleAPI:
		return c.MetricsPort
	case RoleWorker:
		return c.WorkerMetricsPort
	default:
		return 0
	}
}

// Validate menerapkan aturan docs/10 §8 untuk role yang diberikan.
func (c *Config) Validate(role Role) error {
	var p []string

	// --- umum ---
	if !slices.Contains([]string{"development", "test", "staging", "production"}, c.Environment) {
		p = append(p, "ENVIRONMENT harus salah satu dari development, test, staging, production")
	}
	if _, err := logger.ParseLevel(c.LogLevel); err != nil {
		p = append(p, "LOG_LEVEL harus salah satu dari debug, info, warn, error")
	}
	if c.Port < 1 || c.Port > 65535 {
		p = append(p, "PORT harus 1–65535")
	}

	switch role {
	case RoleAPI, RoleWorker:
		p = append(p, c.validateRuntime(role)...)
	case RoleMigrate:
		p = append(p, checkURL("MIGRATION_DATABASE_URL", c.MigrationDatabaseURL, "pgx5")...)
	default:
		p = append(p, fmt.Sprintf("role %q tidak dikenal", role))
	}

	if len(p) > 0 {
		return &Error{Problems: p}
	}
	return nil
}

func (c *Config) validateRuntime(role Role) []string {
	var p []string

	p = append(p, checkURL("DATABASE_URL", c.DatabaseURL, "postgres", "postgresql")...)
	p = append(p, checkURL("REDIS_URL", c.RedisURL, "redis", "rediss")...)
	if role == RoleWorker {
		p = append(p, checkURL("MAINTENANCE_DATABASE_URL", c.MaintenanceDatabaseURL, "postgres", "postgresql")...)
	}

	switch {
	case c.SessionSecret == "":
		p = append(p, "SESSION_SECRET wajib diisi")
	case len(c.SessionSecret) < 32:
		p = append(p, "SESSION_SECRET minimal 32 byte")
	case c.IsProduction() && strings.HasPrefix(c.SessionSecret, "dev-only"):
		p = append(p, "SESSION_SECRET masih nilai contoh pengembangan; tidak boleh dipakai di production")
	}

	if len(c.AllowedOrigins) == 0 {
		p = append(p, "ALLOWED_ORIGINS wajib diisi (minimal satu origin)")
	}
	for _, o := range c.AllowedOrigins {
		u, err := url.Parse(o)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
			(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
			p = append(p, "ALLOWED_ORIGINS harus berisi origin (skema://host[:port]) tanpa path, query, atau fragment")
			break
		}
	}

	if role == RoleAPI {
		p = append(p, c.validateAuth()...)
	}

	for _, cidr := range c.TrustedProxies {
		if _, err := parseProxy(cidr); err != nil {
			p = append(p, "TRUSTED_PROXIES harus berisi daftar CIDR atau alamat IP yang valid")
			break
		}
	}

	if c.DBMaxConns < 1 || c.DBMaxConns > 200 {
		p = append(p, "DB_MAX_CONNS harus 1–200")
	}
	if c.DBMaintenanceMaxConns < 1 || c.DBMaintenanceMaxConns > 50 {
		p = append(p, "DB_MAINTENANCE_MAX_CONNS harus 1–50")
	}
	if c.DBStatementTimeout < 100*time.Millisecond || c.DBStatementTimeout > 10*time.Minute {
		p = append(p, "DB_STATEMENT_TIMEOUT harus antara 100ms dan 10m")
	}

	for _, mp := range []struct {
		name string
		port int
	}{{"METRICS_PORT", c.MetricsPort}, {"WORKER_METRICS_PORT", c.WorkerMetricsPort}} {
		if mp.port < 0 || mp.port > 65535 {
			p = append(p, mp.name+" harus 0–65535 (0 = nonaktif)")
		}
	}
	if role == RoleAPI && c.MetricsPort > 0 && c.MetricsPort == c.Port {
		p = append(p, "METRICS_PORT tidak boleh sama dengan PORT")
	}

	// Opsional: kosong = tracing no-op (docs/14 §6). Bila diisi, harus URL dasar kolektor OTLP/HTTP.
	if c.OTLPEndpoint != "" && !validOTLPEndpoint(c.OTLPEndpoint) {
		p = append(p, "OTEL_EXPORTER_OTLP_ENDPOINT harus URL http/https tanpa kredensial, query, atau fragment")
	}

	p = append(p, checkURL("PUBLIC_BASE_URL", c.PublicBaseURL, "http", "https")...)
	if c.IsProduction() {
		if !c.CookieSecure {
			p = append(p, "COOKIE_SECURE=false ditolak saat ENVIRONMENT=production")
		}
		if u, err := url.Parse(c.PublicBaseURL); err == nil && c.PublicBaseURL != "" && u.Scheme != "https" {
			p = append(p, "PUBLIC_BASE_URL harus https saat ENVIRONMENT=production")
		}
	}
	return p
}

// Argon2Params mengembalikan parameter hash password. Hanya bermakna setelah Validate berhasil
// (role API); nilai tak valid menghasilkan parameter nol yang ditolak konstruktor hasher.
func (c *Config) Argon2Params() security.Argon2Params {
	p, err := security.Argon2ParamsFromInts(c.PasswordArgon2MemoryKiB, c.PasswordArgon2Time, c.PasswordArgon2Parallelism)
	if err != nil {
		return security.Argon2Params{}
	}
	return p
}

// PasswordPolicy mengembalikan kebijakan password (panjang minimum, docs/05 §4.1).
func (c *Config) PasswordPolicy() security.PasswordPolicy {
	return security.PasswordPolicy{MinLength: c.PasswordMinLength}
}

func (c *Config) validateAuth() []string {
	var p []string
	if c.SessionIdleTimeout < time.Minute || c.SessionIdleTimeout > 24*time.Hour {
		p = append(p, "SESSION_IDLE_TIMEOUT harus antara 1m dan 24h")
	}
	if c.SessionAbsoluteTimeout < 5*time.Minute || c.SessionAbsoluteTimeout > 30*24*time.Hour {
		p = append(p, "SESSION_ABSOLUTE_TIMEOUT harus antara 5m dan 720h")
	}
	if c.SessionIdleTimeout > c.SessionAbsoluteTimeout {
		p = append(p, "SESSION_IDLE_TIMEOUT tidak boleh melebihi SESSION_ABSOLUTE_TIMEOUT")
	}
	if _, err := security.Argon2ParamsFromInts(c.PasswordArgon2MemoryKiB, c.PasswordArgon2Time, c.PasswordArgon2Parallelism); err != nil {
		p = append(p, "PASSWORD_ARGON2_MEMORY_KIB (8–1048576, minimal 8 x paralelisme), PASSWORD_ARGON2_TIME (1–16), "+
			"dan PASSWORD_ARGON2_PARALLELISM (1–64) harus dalam rentang aman")
	}
	if c.PasswordHashConcurrency < 0 || c.PasswordHashConcurrency > 256 {
		p = append(p, "PASSWORD_HASH_CONCURRENCY harus 0–256 (0 = jumlah CPU)")
	}
	if c.PasswordMinLength < 8 || c.PasswordMinLength > 128 {
		p = append(p, "PASSWORD_MIN_LENGTH harus 8–128")
	}
	return p
}

// TrustedProxyPrefixes mengembalikan TRUSTED_PROXIES sebagai prefix jaringan. Hanya dipanggil
// setelah Validate berhasil, jadi entri yang tak valid sudah ditolak.
func (c *Config) TrustedProxyPrefixes() []netip.Prefix {
	out := make([]netip.Prefix, 0, len(c.TrustedProxies))
	for _, s := range c.TrustedProxies {
		if p, err := parseProxy(s); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// parseProxy menerima CIDR ("10.0.0.0/8") atau satu alamat IP ("10.0.0.1" -> /32 atau /128).
func parseProxy(s string) (netip.Prefix, error) {
	if p, err := netip.ParsePrefix(s); err == nil {
		return p.Masked(), nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(a, a.BitLen()), nil
}

// checkURL memeriksa bahwa nilai berupa URL absolut dengan salah satu skema yang diizinkan.
// Pesan hanya memuat nama variabel — tidak pernah nilainya (bisa mengandung kredensial).
func checkURL(name, raw string, schemes ...string) []string {
	if raw == "" {
		return []string{name + " wajib diisi"}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || !slices.Contains(schemes, u.Scheme) {
		return []string{fmt.Sprintf("%s harus URL absolut dengan skema %s", name, strings.Join(schemes, "/"))}
	}
	return nil
}

// validOTLPEndpoint memeriksa URL dasar kolektor OTLP: http/https dengan host, tanpa kredensial,
// query, atau fragment (kredensial kolektor lewat header/env standar OTel, bukan di URL).
func validOTLPEndpoint(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return false
	}
	return u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == ""
}

func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// String, GoString, dan LogValue memastikan Config yang tak sengaja dicetak/dilog tidak
// membocorkan rahasia: hanya bidang non-rahasia yang tampil.
func (c Config) String() string   { return c.redacted() }
func (c Config) GoString() string { return c.redacted() }

func (c Config) redacted() string {
	return fmt.Sprintf("config{environment=%s port=%d log_level=%s}", c.Environment, c.Port, c.LogLevel)
}

// LogValue mengimplementasikan slog.LogValuer.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("environment", c.Environment),
		slog.Int("port", c.Port),
		slog.String("log_level", c.LogLevel),
	)
}
