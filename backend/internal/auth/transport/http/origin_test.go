package authhttp_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	authhttp "github.com/master-abror/zago-core/backend/internal/auth/transport/http"
)

func TestOriginPolicy(t *testing.T) {
	p := authhttp.NewOriginPolicy([]string{"https://app.example.com", " HTTP://Localhost:5173/ ", ""})

	for _, ok := range []string{"https://app.example.com", "http://localhost:5173"} {
		require.True(t, p.Allows(ok), ok)
	}
	for name, bad := range map[string]string{
		"kosong":              "",
		"null":                "null",
		"subdomain lain":      "https://evil.app.example.com",
		"domain mirip":        "https://app.example.com.evil.test",
		"awalan domain":       "https://evilapp.example.com",
		"skema lain":          "http://app.example.com",
		"port lain":           "https://app.example.com:8443",
		"slash penutup":       "https://app.example.com/",
		"dengan path":         "https://app.example.com/x",
		"huruf besar":         "https://APP.example.com",
		"localhost port lain": "http://localhost:3000",
		"userinfo":            "https://app.example.com@evil.test",
		"spasi":               " https://app.example.com",
	} {
		t.Run(name, func(t *testing.T) {
			require.False(t, p.Allows(bad))
		})
	}
	require.False(t, authhttp.NewOriginPolicy(nil).Allows("https://app.example.com"), "tanpa daftar: tolak semua")
}
