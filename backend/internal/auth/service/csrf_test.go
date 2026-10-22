package service_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/auth/service"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

const csrfSecret = "unit-test-secret-unit-test-secret-0123"

func newCSRF(t *testing.T, secret string) *service.CSRF {
	t.Helper()
	c, err := service.NewCSRF(secret)
	require.NoError(t, err)
	return c
}

func TestCSRFTokenIsBoundToTheSession(t *testing.T) {
	c := newCSRF(t, csrfSecret)
	a, b := id.NewID(), id.NewID()

	ta := c.Token(a)
	require.Len(t, ta, 43)
	require.Equal(t, ta, c.Token(a), "deterministik: tanpa penyimpanan")
	require.NotEqual(t, ta, c.Token(b), "sesi berbeda -> token berbeda")

	require.True(t, c.Verify(a, ta))
	require.False(t, c.Verify(b, ta), "token dari sesi lain ditolak")
	require.False(t, c.Verify(a, c.Token(b)), "token sesi lain ditolak (arah sebaliknya)")
}

func TestCSRFRejectsMalformedTokens(t *testing.T) {
	c := newCSRF(t, csrfSecret)
	sid := id.NewID()
	good := c.Token(sid)
	flipped := []byte(good)
	if flipped[0] == 'A' {
		flipped[0] = 'B'
	} else {
		flipped[0] = 'A'
	}

	for name, tok := range map[string]string{
		"kosong":             "",
		"spasi":              "   ",
		"terlalu pendek":     good[:42],
		"terlalu panjang":    good + "A",
		"satu karakter beda": string(flipped),
		"huruf besar semua":  strings.ToUpper(good),
		"berpadding":         good[:42] + "=",
		"karakter terlarang": strings.Repeat("!", 43),
		"hex 64":             strings.Repeat("a", 64),
		"ujung berspasi":     good + " ",
	} {
		t.Run(name, func(t *testing.T) {
			require.False(t, c.Verify(sid, tok))
		})
	}
	require.True(t, c.Verify(sid, good), "token asli tetap sah")
}

func TestCSRFDependsOnTheSecret(t *testing.T) {
	sid := id.NewID()
	c1 := newCSRF(t, csrfSecret)
	c2 := newCSRF(t, csrfSecret+"-lain")

	require.NotEqual(t, c1.Token(sid), c2.Token(sid))
	require.False(t, c2.Verify(sid, c1.Token(sid)), "token dari rahasia lain ditolak")
}

func TestCSRFRequiresAStrongSecret(t *testing.T) {
	_, err := service.NewCSRF("")
	require.Error(t, err)
	_, err = service.NewCSRF(strings.Repeat("x", 31))
	require.Error(t, err)
	_, err = service.NewCSRF(strings.Repeat("x", 32))
	require.NoError(t, err)
}
