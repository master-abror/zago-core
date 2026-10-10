package httpx

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Token yang MAC-nya sah tetapi isinya tidak boleh diterima (versi/arah/id tak dikenal).
func TestDecodeRejectsValidlySignedButUnacceptablePayloads(t *testing.T) {
	c, err := NewCursorCodec("dev-only-session-secret-0123456789abcdef")
	require.NoError(t, err)
	good := cursorPayload{V: cursorVersion, T: time.Now().UnixNano(), I: id.NewID().String(), D: string(Desc), F: ""}

	_, err = c.Decode(c.seal(good), Desc, "")
	require.NoError(t, err)

	for name, mut := range map[string]func(p *cursorPayload){
		"versi lain":  func(p *cursorPayload) { p.V = 2 },
		"versi nol":   func(p *cursorPayload) { p.V = 0 },
		"arah aneh":   func(p *cursorPayload) { p.D = "sideways" },
		"arah kosong": func(p *cursorPayload) { p.D = "" },
		"id bukan uuid": func(p *cursorPayload) {
			p.I = "bukan-uuid"
		},
		"id kosong": func(p *cursorPayload) { p.I = "" },
	} {
		p := good
		mut(&p)
		_, err := c.Decode(c.seal(p), Desc, "")
		require.Error(t, err, name)
		require.True(t, HasCode(err, ValidationFailed), name)
	}
}

func TestCursorKeyIsDomainSeparatedFromSecret(t *testing.T) {
	secret := "dev-only-session-secret-0123456789abcdef"
	c, err := NewCursorCodec(secret)
	require.NoError(t, err)
	require.Len(t, c.key, 32)
	require.NotEqual(t, []byte(secret), c.key)
}
