package authhttp_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/auth/repository"
	"github.com/master-abror/zago-core/backend/internal/auth/service"
	authhttp "github.com/master-abror/zago-core/backend/internal/auth/transport/http"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/internal/testkit"
)

// Rantai Authenticate -> CSRF -> RequireAuth dengan SessionService ASLI (PostgreSQL + Redis nyata):
// cookie -> sesi -> Identity -> CSRF -> pencabutan, tanpa fake.
func TestMiddlewareWithRealSessionService(t *testing.T) {
	ctx := context.Background()
	db := testkit.NewDB(t)
	rdb := testkit.Redis(t).Client(t)
	svc := service.NewSessionService(repository.NewStore(db.App), rdb,
		service.StaticTimeouts{Idle: 30 * time.Minute, Absolute: 12 * time.Hour}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	user := testkit.NewUser(t, db)
	iss, err := svc.Create(ctx, service.CreateParams{UserID: user.ID})
	require.NoError(t, err)

	csrf, err := service.NewCSRF(secret)
	require.NoError(t, err)
	cookie, err := authhttp.NewCookie("platform_session", true)
	require.NoError(t, err)
	m, err := authhttp.New(authhttp.Deps{
		Auth: svc, Cookie: cookie, CSRF: csrf, Origins: authhttp.NewOriginPolicy([]string{origin}),
		Responder: httpx.NewResponder(slog.New(slog.NewTextHandler(io.Discard, nil))),
	})
	require.NoError(t, err)
	h := m.Authenticate(m.CSRF(m.RequireAuth(http.HandlerFunc(respondWho))))

	// Cookie ditulis oleh Cookie.Set lalu dikirim balik persis seperti peramban.
	setW := httptest.NewRecorder()
	cookie.Set(setW, iss.Token, iss.Session.ExpiresAt, time.Now())
	sent := setW.Result().Cookies()[0]

	do := func(method string, withToken bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/private", nil)
		r.AddCookie(sent)
		if withToken {
			r.Header.Set(authhttp.HeaderCSRF, csrf.Token(iss.Session.ID))
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	w := do(http.MethodGet, false)
	require.Equal(t, http.StatusOK, w.Code)
	got := decodeWho(t, w)
	require.Equal(t, user.ID.String(), got.UserID)
	require.Equal(t, iss.Session.ID.String(), got.SessionID)

	require.Equal(t, http.StatusForbidden, do(http.MethodPost, false).Code, "POST tanpa token CSRF")
	require.Equal(t, http.StatusOK, do(http.MethodPost, true).Code, "POST dengan token CSRF sesi ini")

	require.NoError(t, svc.Revoke(ctx, user.ID, iss.Session.ID, service.ReasonLogout))
	w = do(http.MethodGet, false)
	require.Equal(t, http.StatusUnauthorized, w.Code, "sesi dicabut: cookie yang sama tak lagi berlaku")
	require.Equal(t, "authentication_required", errCode(t, w))
	var cleared bool
	for _, ck := range w.Result().Cookies() {
		cleared = cleared || (ck.Name == cookie.Name() && ck.MaxAge < 0)
	}
	require.True(t, cleared)
}
