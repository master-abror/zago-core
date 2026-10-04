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

	"github.com/caarlos0/env/v11"

	"github.com/master-abror/zago-core/backend/pkg/logger"
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

	// security
	SessionSecret    string   `env:"SESSION_SECRET"` // ≥ 32 byte
	CookieName       string   `env:"COOKIE_NAME" envDefault:"platform_session"`
	CookieSecure     bool     `env:"COOKIE_SECURE" envDefault:"true"`
	AllowedOrigins   []string `env:"ALLOWED_ORIGINS"`
	TrustedProxies   []string `env:"TRUSTED_PROXIES"` // CIDR yang X-Forwarded-*-nya dipercaya
	MFAEncryptionKey string   `env:"MFA_ENCRYPTION_KEY"`

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

	for _, cidr := range c.TrustedProxies {
		if _, err := parseProxy(cidr); err != nil {
			p = append(p, "TRUSTED_PROXIES harus berisi daftar CIDR atau alamat IP yang valid")
			break
		}
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
