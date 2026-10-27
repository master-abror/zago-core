package authhttp_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	authhttp "github.com/master-abror/zago-core/backend/internal/auth/transport/http"
)

func TestCookieNameGetsHostPrefixOnlyWhenSecure(t *testing.T) {
	secure, err := authhttp.NewCookie("platform_session", true)
	require.NoError(t, err)
	require.Equal(t, "__Host-platform_session", secure.Name())

	dev, err := authhttp.NewCookie("platform_session", false)
	require.NoError(t, err)
	require.Equal(t, "platform_session", dev.Name(), "peramban menolak __Host- tanpa Secure")
}

func TestNewCookieRejectsBadNames(t *testing.T) {
	for name, n := range map[string]string{
		"kosong":            "",
		"prefiks host":      "__Host-x",
		"prefiks secure":    "__Secure-x",
		"garis bawah ganda": "__x",
		"spasi":             "a b",
		"titik koma":        "a;b",
		"sama dengan":       "a=b",
		"koma":              "a,b",
		"non-ASCII":         "sesi\u00e9",
		"garis miring":      "a/b",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := authhttp.NewCookie(n, true)
			require.Error(t, err)
		})
	}
}

func TestCookieSetAttributes(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	for _, secure := range []bool{true, false} {
		c, err := authhttp.NewCookie("platform_session", secure)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c.Set(w, "tok-123", now.Add(90*time.Minute), now)
		cookies := w.Result().Cookies()
		require.Len(t, cookies, 1)
		ck := cookies[0]

		require.Equal(t, c.Name(), ck.Name)
		require.Equal(t, "tok-123", ck.Value)
		require.True(t, ck.HttpOnly, "HttpOnly: JavaScript tidak boleh membaca cookie sesi")
		require.Equal(t, secure, ck.Secure)
		require.Equal(t, http.SameSiteLaxMode, ck.SameSite)
		require.Equal(t, "/", ck.Path)
		require.Empty(t, ck.Domain, "tanpa Domain: cookie terikat ke host persis (syarat __Host-)")
		require.Equal(t, 90*60, ck.MaxAge, "umur cookie = sisa umur absolut sesi")
		require.WithinDuration(t, now.Add(90*time.Minute), ck.Expires, time.Second)
	}
}

func TestCookieNeverOutlivesTheSession(t *testing.T) {
	c, _ := authhttp.NewCookie("platform_session", true)
	now := time.Now()

	w := httptest.NewRecorder()
	c.Set(w, "tok", now.Add(1500*time.Millisecond), now)
	require.Equal(t, 2, w.Result().Cookies()[0].MaxAge, "dibulatkan ke atas")

	for name, exp := range map[string]time.Time{"sudah lewat": now.Add(-time.Minute), "tepat sekarang": now} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c.Set(w, "tok", exp, now)
			ck := w.Result().Cookies()[0]
			require.Negative(t, ck.MaxAge, "sesi habis: cookie dihapus, bukan ditulis")
			require.Empty(t, ck.Value)
		})
	}
}

func TestCookieClear(t *testing.T) {
	c, _ := authhttp.NewCookie("platform_session", true)
	w := httptest.NewRecorder()
	c.Clear(w)
	ck := w.Result().Cookies()[0]
	require.Equal(t, c.Name(), ck.Name)
	require.Empty(t, ck.Value)
	require.Negative(t, ck.MaxAge)
	require.True(t, ck.HttpOnly)
	require.True(t, ck.Secure)
	require.Equal(t, "/", ck.Path, "Path harus sama agar peramban benar-benar menghapus")
}

func TestCookieTokenRead(t *testing.T) {
	c, _ := authhttp.NewCookie("platform_session", false)
	req := func(cookies ...*http.Cookie) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		for _, ck := range cookies {
			r.AddCookie(ck)
		}
		return r
	}

	tok, ok := c.Token(req(&http.Cookie{Name: "platform_session", Value: "abc"}))
	require.True(t, ok)
	require.Equal(t, "abc", tok)

	_, ok = c.Token(req())
	require.False(t, ok, "tanpa cookie")
	_, ok = c.Token(req(&http.Cookie{Name: "platform_session", Value: ""}))
	require.False(t, ok, "cookie kosong")
	_, ok = c.Token(req(&http.Cookie{Name: "other", Value: "abc"}))
	require.False(t, ok, "cookie bernama lain")

	secure, _ := authhttp.NewCookie("platform_session", true)
	_, ok = secure.Token(req(&http.Cookie{Name: "platform_session", Value: "abc"}))
	require.False(t, ok, "cookie tanpa prefiks __Host- tidak diterima saat mode Secure (anti cookie-tossing)")
}
