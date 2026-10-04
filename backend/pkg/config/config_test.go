package config_test

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/pkg/config"
)

const goodSecret = "0123456789abcdef0123456789abcdef-prod-secret"

func devEnv() map[string]string {
	return map[string]string{
		"ENVIRONMENT":              "development",
		"DATABASE_URL":             "postgres://app_user:app_dev_pw@localhost:5432/platform",
		"MAINTENANCE_DATABASE_URL": "postgres://app_maintenance:maintenance_dev_pw@localhost:5432/platform",
		"MIGRATION_DATABASE_URL":   "pgx5://app_migrator:migrator_dev_pw@localhost:5432/platform",
		"REDIS_URL":                "redis://localhost:6379",
		"SESSION_SECRET":           "dev-only-change-me-dev-only-change-me",
		"COOKIE_SECURE":            "false",
		"ALLOWED_ORIGINS":          "http://localhost:5173",
		"PUBLIC_BASE_URL":          "http://localhost:5173",
	}
}

func prodEnv() map[string]string {
	e := devEnv()
	e["ENVIRONMENT"] = "production"
	e["COOKIE_SECURE"] = "true"
	e["SESSION_SECRET"] = goodSecret
	e["PUBLIC_BASE_URL"] = "https://platform.example.org"
	e["ALLOWED_ORIGINS"] = "https://platform.example.org"
	return e
}

func with(base map[string]string, mutate func(map[string]string)) map[string]string {
	m := maps.Clone(base)
	mutate(m)
	return m
}

func problems(t *testing.T, err error) []string {
	t.Helper()
	var ce *config.Error
	require.True(t, errors.As(err, &ce), "harus *config.Error, dapat: %v", err)
	return ce.Problems
}

func TestLoadFromDefaultsAndParsing(t *testing.T) {
	e := devEnv()
	e["TRUSTED_PROXIES"] = " 10.0.0.0/8 , ,192.168.0.0/16"
	e["ALLOWED_ORIGINS"] = "http://localhost:5173, https://app.example.org"

	c, err := config.LoadFrom(config.RoleAPI, e)
	require.NoError(t, err)

	require.Equal(t, 8080, c.Port)
	require.Equal(t, "platform_session", c.CookieName)
	require.Equal(t, "info", c.LogLevel)
	require.Equal(t, 1025, c.SMTPPort)
	require.Equal(t, "./data/uploads", c.StoragePath)
	require.False(t, c.CookieSecure)
	require.Equal(t, []string{"10.0.0.0/8", "192.168.0.0/16"}, c.TrustedProxies)
	require.Equal(t, []string{"http://localhost:5173", "https://app.example.org"}, c.AllowedOrigins)
}

func TestCookieSecureDefaultsToTrue(t *testing.T) {
	c, err := config.LoadFrom(config.RoleAPI, with(prodEnv(), func(m map[string]string) { delete(m, "COOKIE_SECURE") }))
	require.NoError(t, err)
	require.True(t, c.CookieSecure)
}

func TestEmptyEnvironmentFailsForEveryRuntimeRole(t *testing.T) {
	for _, role := range []config.Role{config.RoleAPI, config.RoleWorker, config.RoleMigrate} {
		_, err := config.LoadFrom(role, map[string]string{})
		require.Error(t, err, string(role))
	}
}

func TestAPIRequiredVariables(t *testing.T) {
	cases := map[string]string{
		"DATABASE_URL":    "DATABASE_URL",
		"REDIS_URL":       "REDIS_URL",
		"SESSION_SECRET":  "SESSION_SECRET",
		"ALLOWED_ORIGINS": "ALLOWED_ORIGINS",
		"PUBLIC_BASE_URL": "PUBLIC_BASE_URL",
	}
	for key, want := range cases {
		t.Run(key, func(t *testing.T) {
			_, err := config.LoadFrom(config.RoleAPI, with(devEnv(), func(m map[string]string) { delete(m, key) }))
			require.Error(t, err)
			require.Contains(t, strings.Join(problems(t, err), "|"), want)
		})
	}
}

func TestAPIDoesNotRequireWorkerOrMigratorURLs(t *testing.T) {
	e := with(devEnv(), func(m map[string]string) {
		delete(m, "MAINTENANCE_DATABASE_URL")
		delete(m, "MIGRATION_DATABASE_URL")
	})
	_, err := config.LoadFrom(config.RoleAPI, e)
	require.NoError(t, err)
}

func TestWorkerRequiresMaintenanceDatabaseURL(t *testing.T) {
	e := with(devEnv(), func(m map[string]string) { delete(m, "MAINTENANCE_DATABASE_URL") })
	_, err := config.LoadFrom(config.RoleWorker, e)
	require.Error(t, err)
	require.Contains(t, strings.Join(problems(t, err), "|"), "MAINTENANCE_DATABASE_URL")

	_, err = config.LoadFrom(config.RoleWorker, devEnv())
	require.NoError(t, err)
}

func TestMigrateNeedsOnlyMigrationURL(t *testing.T) {
	_, err := config.LoadFrom(config.RoleMigrate, map[string]string{
		"MIGRATION_DATABASE_URL": "pgx5://app_migrator:pw@localhost:5432/platform",
	})
	require.NoError(t, err)

	_, err = config.LoadFrom(config.RoleMigrate, map[string]string{
		"MIGRATION_DATABASE_URL": "postgres://app_migrator:pw@localhost:5432/platform", // skema salah
	})
	require.Error(t, err)
	require.Contains(t, strings.Join(problems(t, err), "|"), "MIGRATION_DATABASE_URL")
}

func TestSessionSecretMinimumLength(t *testing.T) {
	_, err := config.LoadFrom(config.RoleAPI, with(devEnv(), func(m map[string]string) { m["SESSION_SECRET"] = "short" }))
	require.Error(t, err)
	require.Contains(t, strings.Join(problems(t, err), "|"), "SESSION_SECRET minimal 32 byte")
}

func TestProductionRules(t *testing.T) {
	_, err := config.LoadFrom(config.RoleAPI, prodEnv())
	require.NoError(t, err, "baseline production harus valid")

	cases := map[string]struct {
		mutate func(map[string]string)
		want   string
	}{
		"cookie tidak secure":   {func(m map[string]string) { m["COOKIE_SECURE"] = "false" }, "COOKIE_SECURE"},
		"base url bukan https":  {func(m map[string]string) { m["PUBLIC_BASE_URL"] = "http://platform.example.org" }, "PUBLIC_BASE_URL harus https"},
		"secret contoh dev":     {func(m map[string]string) { m["SESSION_SECRET"] = "dev-only-change-me-dev-only-change-me" }, "SESSION_SECRET masih nilai contoh"},
		"secret terlalu pendek": {func(m map[string]string) { m["SESSION_SECRET"] = "short" }, "SESSION_SECRET minimal 32 byte"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := config.LoadFrom(config.RoleAPI, with(prodEnv(), tc.mutate))
			require.Error(t, err)
			require.Contains(t, strings.Join(problems(t, err), "|"), tc.want)
		})
	}
}

func TestInvalidValues(t *testing.T) {
	cases := map[string]struct {
		mutate func(map[string]string)
		want   string
	}{
		"environment asing":   {func(m map[string]string) { m["ENVIRONMENT"] = "prod" }, "ENVIRONMENT"},
		"level log asing":     {func(m map[string]string) { m["LOG_LEVEL"] = "verbose" }, "LOG_LEVEL"},
		"port nol":            {func(m map[string]string) { m["PORT"] = "0" }, "PORT"},
		"port terlalu besar":  {func(m map[string]string) { m["PORT"] = "70000" }, "PORT"},
		"database url salah":  {func(m map[string]string) { m["DATABASE_URL"] = "mysql://x@localhost/db" }, "DATABASE_URL"},
		"redis url salah":     {func(m map[string]string) { m["REDIS_URL"] = "localhost:6379" }, "REDIS_URL"},
		"origin pakai path":   {func(m map[string]string) { m["ALLOWED_ORIGINS"] = "http://localhost:5173/app" }, "ALLOWED_ORIGINS"},
		"origin tanpa skema":  {func(m map[string]string) { m["ALLOWED_ORIGINS"] = "localhost:5173" }, "ALLOWED_ORIGINS"},
		"base url relatif":    {func(m map[string]string) { m["PUBLIC_BASE_URL"] = "/relatif" }, "PUBLIC_BASE_URL"},
		"base url skema aneh": {func(m map[string]string) { m["PUBLIC_BASE_URL"] = "ftp://x.example.org" }, "PUBLIC_BASE_URL"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := config.LoadFrom(config.RoleAPI, with(devEnv(), tc.mutate))
			require.Error(t, err)
			require.Contains(t, strings.Join(problems(t, err), "|"), tc.want)
		})
	}
}

func TestTrustedProxies(t *testing.T) {
	e := with(devEnv(), func(m map[string]string) { m["TRUSTED_PROXIES"] = "10.0.0.0/8, 192.168.1.5, ::1" })
	c, err := config.LoadFrom(config.RoleAPI, e)
	require.NoError(t, err)

	var got []string
	for _, p := range c.TrustedProxyPrefixes() {
		got = append(got, p.String())
	}
	require.Equal(t, []string{"10.0.0.0/8", "192.168.1.5/32", "::1/128"}, got)

	_, err = config.LoadFrom(config.RoleAPI, with(devEnv(), func(m map[string]string) { m["TRUSTED_PROXIES"] = "10.0.0.0/8, bukan-cidr" }))
	require.Error(t, err)
	require.Contains(t, strings.Join(problems(t, err), "|"), "TRUSTED_PROXIES")
}

func TestUnparseablePortFailsLoad(t *testing.T) {
	_, err := config.LoadFrom(config.RoleAPI, with(devEnv(), func(m map[string]string) { m["PORT"] = "abc" }))
	require.Error(t, err)
}

func TestAllProblemsAreReportedAtOnce(t *testing.T) {
	_, err := config.LoadFrom(config.RoleAPI, map[string]string{})
	require.Error(t, err)
	joined := strings.Join(problems(t, err), "|")
	for _, name := range []string{"DATABASE_URL", "REDIS_URL", "SESSION_SECRET", "ALLOWED_ORIGINS", "PUBLIC_BASE_URL"} {
		require.Contains(t, joined, name)
	}
}

func TestUnknownRoleIsRejected(t *testing.T) {
	_, err := config.LoadFrom(config.Role("bogus"), devEnv())
	require.Error(t, err)
}

// Rahasia tidak boleh muncul di pesan error maupun saat Config dicetak/dilog.
func TestSecretsNeverLeak(t *testing.T) {
	const secretPW = "SUPER-SECRET-DB-PASSWORD"
	const secretSession = "S3SSION-SECRET-VALUE-THAT-IS-LONG-ENOUGH-1234567890"

	bad := with(devEnv(), func(m map[string]string) {
		m["DATABASE_URL"] = "mysql://u:" + secretPW + "@h/db" // skema salah -> error
		m["SESSION_SECRET"] = secretSession
	})
	_, err := config.LoadFrom(config.RoleAPI, bad)
	require.Error(t, err)
	require.NotContains(t, err.Error(), secretPW)
	require.NotContains(t, err.Error(), secretSession)

	good := with(devEnv(), func(m map[string]string) {
		m["DATABASE_URL"] = "postgres://u:" + secretPW + "@h:5432/db"
		m["SESSION_SECRET"] = secretSession
		m["MFA_ENCRYPTION_KEY"] = "MFA-KEY-VALUE"
		m["SMTP_PASS"] = "SMTP-PASSWORD-VALUE"
	})
	c, err := config.LoadFrom(config.RoleAPI, good)
	require.NoError(t, err)

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("start", "config", c)
	out := strings.Join([]string{
		fmt.Sprintf("%v", c), fmt.Sprintf("%+v", c), fmt.Sprintf("%#v", c), c.String(), buf.String(),
	}, "\n")
	for _, s := range []string{secretPW, secretSession, "MFA-KEY-VALUE", "SMTP-PASSWORD-VALUE"} {
		require.NotContains(t, out, s)
	}
	require.Contains(t, buf.String(), `"port":8080`)
}
