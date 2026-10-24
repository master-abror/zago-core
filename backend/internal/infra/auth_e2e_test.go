package infra_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/auth/service"
	authhttp "github.com/master-abror/zago-core/backend/internal/auth/transport/http"
	"github.com/master-abror/zago-core/backend/internal/httpserver"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/pkg/security"
)

// Alur autentikasi ujung-ke-ujung lewat composition root NYATA (app.RunAPI) terhadap PostgreSQL
// dan Redis sungguhan: anonim -> login gagal -> login sukses -> me/csrf -> logout -> sesi mati.
// Parameter Argon2id diperkecil lewat env agar tes cepat (termasuk di bawah -race); hash credential
// dibuat dengan parameter yang sama, jadi tidak ada rehash.

const e2ePassword = "correct-horse-battery-staple"

func authE2ERequest(t *testing.T, method, url, body string, headers map[string]string) (int, string, http.Header) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rd)
	require.NoError(t, err)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return httpDo(t, req)
}

func authE2ECookies(h http.Header) []*http.Cookie {
	var out []*http.Cookie
	for _, c := range (&http.Response{Header: h}).Cookies() {
		if c.Name == e2eCookieName {
			out = append(out, c)
		}
	}
	return out
}

func authE2EData(t *testing.T, body string, dst any) {
	t.Helper()
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &env), body)
	require.NoError(t, json.Unmarshal(env.Data, dst), body)
}

func authE2EError(t *testing.T, body string) (code, message string) {
	t.Helper()
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &env), body)
	return env.Error.Code, env.Error.Message
}

func TestAuthFlowEndToEndWithRealPostgresAndRedis(t *testing.T) {
	db := testkit.NewDB(t)
	rs := testkit.Redis(t)
	base, _ := launchAPI(t, e2eEnv(db, rs.URL(), map[string]string{
		"PASSWORD_ARGON2_MEMORY_KIB": "8192", "PASSWORD_ARGON2_TIME": "1", "PASSWORD_ARGON2_PARALLELISM": "2",
	}))
	requireReady(t, base)
	ctx := context.Background()

	u := testkit.NewUser(t, db)
	params, err := security.Argon2ParamsFromInts(8192, 1, 2)
	require.NoError(t, err)
	hasher, err := security.NewArgon2idHasher(params, 1)
	require.NoError(t, err)
	hash, err := hasher.Hash(ctx, e2ePassword)
	require.NoError(t, err)
	_, err = db.Migrator.Exec(ctx,
		`INSERT INTO credentials (user_id, credential_type, secret_hash) VALUES ($1, 'password', $2)`, u.ID, hash)
	require.NoError(t, err)

	login := base + httpserver.AuthPath + "/login"
	me := base + httpserver.AuthPath + "/me"
	csrfURL := base + httpserver.AuthPath + "/csrf"
	logout := base + httpserver.AuthPath + "/logout"
	creds := func(email, password string) string {
		b, err := json.Marshal(map[string]string{"email": email, "password": password})
		require.NoError(t, err)
		return string(b)
	}

	// 1) Anonim tidak bisa melihat siapa pun.
	code, body, _ := authE2ERequest(t, http.MethodGet, me, "", nil)
	require.Equal(t, http.StatusUnauthorized, code, body)
	errCode, _ := authE2EError(t, body)
	require.Equal(t, "authentication_required", errCode)

	// 2) Password salah dan email tak dikenal: kode dan pesan sama, tanpa cookie.
	code, wrongBody, wrongHdr := authE2ERequest(t, http.MethodPost, login, creds(u.Email, "password-salah-sekali"), nil)
	require.Equal(t, http.StatusUnauthorized, code, wrongBody)
	wrongCode, wrongMsg := authE2EError(t, wrongBody)
	require.Equal(t, "invalid_credentials", wrongCode)
	require.Empty(t, authE2ECookies(wrongHdr))
	code, unknownBody, unknownHdr := authE2ERequest(t, http.MethodPost, login, creds("tidak-ada@example.test", "password-salah-sekali"), nil)
	require.Equal(t, http.StatusUnauthorized, code, unknownBody)
	unknownCode, unknownMsg := authE2EError(t, unknownBody)
	require.Equal(t, wrongCode, unknownCode)
	require.Equal(t, wrongMsg, unknownMsg, "tidak ada oracle enumerasi akun")
	require.Empty(t, authE2ECookies(unknownHdr))

	// 3) Origin asing ditolak sebelum menyentuh kredensial.
	code, body, _ = authE2ERequest(t, http.MethodPost, login, creds(u.Email, e2ePassword),
		map[string]string{"Origin": "http://evil.example"})
	require.Equal(t, http.StatusForbidden, code, body)
	errCode, _ = authE2EError(t, body)
	require.Equal(t, "csrf_failed", errCode)

	// 4) Login sukses: cookie HttpOnly + csrf_token; token sesi tidak ada di body.
	code, body, hdr := authE2ERequest(t, http.MethodPost, login, creds(strings.ToUpper(u.Email), e2ePassword),
		map[string]string{"Origin": "http://localhost:5173"})
	require.Equal(t, http.StatusOK, code, body)
	require.Equal(t, "no-store", hdr.Get("Cache-Control"))
	cks := authE2ECookies(hdr)
	require.Len(t, cks, 1)
	require.True(t, cks[0].HttpOnly)
	require.NotEmpty(t, cks[0].Value)
	require.NotContains(t, body, cks[0].Value)
	var lr authhttp.LoginResponse
	authE2EData(t, body, &lr)
	require.Equal(t, u.ID, lr.User.ID)
	require.Equal(t, u.Email, lr.User.Email)
	require.NotEmpty(t, lr.CSRFToken)
	cookie := map[string]string{"Cookie": e2eCookieName + "=" + cks[0].Value}

	// 5) Dengan cookie: me dan csrf (token sama dengan yang diberikan login).
	code, body, _ = authE2ERequest(t, http.MethodGet, me, "", cookie)
	require.Equal(t, http.StatusOK, code, body)
	var mr authhttp.MeResponse
	authE2EData(t, body, &mr)
	require.Equal(t, u.ID, mr.User.ID)
	require.Equal(t, u.Email, mr.User.Email)
	code, body, _ = authE2ERequest(t, http.MethodGet, csrfURL, "", cookie)
	require.Equal(t, http.StatusOK, code, body)
	var cr authhttp.CSRFResponse
	authE2EData(t, body, &cr)
	require.Equal(t, lr.CSRFToken, cr.CSRFToken)

	// 6) Logout menuntut CSRF; tanpa token sesi tetap hidup.
	code, body, _ = authE2ERequest(t, http.MethodPost, logout, "", cookie)
	require.Equal(t, http.StatusForbidden, code, body)
	code, _, _ = authE2ERequest(t, http.MethodGet, me, "", cookie)
	require.Equal(t, http.StatusOK, code, "logout yang ditolak tidak boleh mematikan sesi")

	code, body, hdr = authE2ERequest(t, http.MethodPost, logout, "",
		map[string]string{"Cookie": cookie["Cookie"], authhttp.HeaderCSRF: lr.CSRFToken})
	require.Equal(t, http.StatusNoContent, code, body)
	cks = authE2ECookies(hdr)
	require.Len(t, cks, 1)
	require.Less(t, cks[0].MaxAge, 0, "cookie dihapus")

	// 7) Cookie lama sudah mati.
	code, body, _ = authE2ERequest(t, http.MethodGet, me, "", cookie)
	require.Equal(t, http.StatusUnauthorized, code, body)

	// 8) Jejak di database: event keamanan berurutan dan activity login/logout.
	rows, err := db.Migrator.Query(ctx,
		`SELECT event_type, result FROM security_events WHERE user_id = $1 ORDER BY created_at, id`, u.ID)
	require.NoError(t, err)
	var got []string
	for rows.Next() {
		var typ, res string
		require.NoError(t, rows.Scan(&typ, &res))
		got = append(got, typ+":"+res)
	}
	require.NoError(t, rows.Err())
	rows.Close()
	require.Equal(t, []string{
		service.EventLoginFailed + ":failure",
		service.EventLoginSuccess + ":success",
		service.EventLogout + ":success",
	}, got)
	var activities int
	require.NoError(t, db.Migrator.QueryRow(ctx,
		`SELECT count(*) FROM activities WHERE actor_user_id = $1 AND action IN ('auth.login', 'auth.logout')`, u.ID).Scan(&activities))
	require.Equal(t, 2, activities)
}
