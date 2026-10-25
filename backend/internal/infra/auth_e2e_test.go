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
	"github.com/master-abror/zago-core/backend/pkg/id"
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

func TestAuthRefreshAndSwitchOrganizationEndToEnd(t *testing.T) {
	db := testkit.NewDB(t)
	rs := testkit.Redis(t)
	base, _ := launchAPI(t, e2eEnv(db, rs.URL(), map[string]string{
		"PASSWORD_ARGON2_MEMORY_KIB": "8192", "PASSWORD_ARGON2_TIME": "1", "PASSWORD_ARGON2_PARALLELISM": "2",
	}))
	requireReady(t, base)
	ctx := context.Background()

	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	orgC := testkit.NewOrganization(t, db) // user bukan anggota
	u := testkit.NewUser(t, db, testkit.InOrganization(orgA))
	_, err := db.Migrator.Exec(ctx,
		`INSERT INTO organization_memberships (id, organization_id, user_id, status, joined_at) VALUES ($1, $2, $3, 'active', now())`,
		id.NewID(), orgB.ID, u.ID)
	require.NoError(t, err)
	params, err := security.Argon2ParamsFromInts(8192, 1, 2)
	require.NoError(t, err)
	hasher, err := security.NewArgon2idHasher(params, 1)
	require.NoError(t, err)
	hash, err := hasher.Hash(ctx, e2ePassword)
	require.NoError(t, err)
	_, err = db.Migrator.Exec(ctx,
		`INSERT INTO credentials (user_id, credential_type, secret_hash) VALUES ($1, 'password', $2)`, u.ID, hash)
	require.NoError(t, err)

	authURL := func(p string) string { return base + httpserver.AuthPath + p }
	withSession := func(token, csrf string) map[string]string {
		return map[string]string{"Cookie": e2eCookieName + "=" + token, authhttp.HeaderCSRF: csrf}
	}
	meOrg := func(token string) (int, string) {
		code, body, _ := authE2ERequest(t, http.MethodGet, authURL("/me"), "", map[string]string{"Cookie": e2eCookieName + "=" + token})
		if code != http.StatusOK {
			return code, ""
		}
		var mr authhttp.MeResponse
		authE2EData(t, body, &mr)
		if mr.OrganizationID == nil {
			return code, ""
		}
		return code, mr.OrganizationID.String()
	}

	// Login: organisasi awal adalah keanggotaan tertua (A).
	b, err := json.Marshal(map[string]string{"email": u.Email, "password": e2ePassword})
	require.NoError(t, err)
	code, body, hdr := authE2ERequest(t, http.MethodPost, authURL("/login"), string(b), nil)
	require.Equal(t, http.StatusOK, code, body)
	token1 := authE2ECookies(hdr)[0].Value
	var lr authhttp.LoginResponse
	authE2EData(t, body, &lr)
	code, org := meOrg(token1)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, orgA.ID.String(), org)

	// Refresh: token dan CSRF berganti, organisasi tetap, token lama mati.
	code, body, hdr = authE2ERequest(t, http.MethodPost, authURL("/refresh"), "", withSession(token1, lr.CSRFToken))
	require.Equal(t, http.StatusOK, code, body)
	token2 := authE2ECookies(hdr)[0].Value
	var rr authhttp.RefreshResponse
	authE2EData(t, body, &rr)
	require.NotEqual(t, token1, token2)
	require.NotEqual(t, lr.CSRFToken, rr.CSRFToken)
	code, _ = meOrg(token1)
	require.Equal(t, http.StatusUnauthorized, code, "token lama mati setelah rotasi")
	code, org = meOrg(token2)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, orgA.ID.String(), org)

	// Token CSRF lama tidak berlaku untuk sesi baru.
	code, body, _ = authE2ERequest(t, http.MethodPost, authURL("/refresh"), "", withSession(token2, lr.CSRFToken))
	require.Equal(t, http.StatusForbidden, code, body)

	// Pindah ke organisasi yang bukan haknya ditolak tanpa efek.
	toOrg := func(o testkit.Organization) string {
		out, err := json.Marshal(map[string]string{"organization_id": o.ID.String()})
		require.NoError(t, err)
		return string(out)
	}
	code, body, hdr = authE2ERequest(t, http.MethodPost, authURL("/switch-organization"), toOrg(orgC), withSession(token2, rr.CSRFToken))
	require.Equal(t, http.StatusForbidden, code, body)
	errCode, _ := authE2EError(t, body)
	require.Equal(t, "permission_denied", errCode)
	require.Empty(t, authE2ECookies(hdr))
	code, org = meOrg(token2)
	require.Equal(t, http.StatusOK, code, "sesi tidak tersentuh oleh penolakan")
	require.Equal(t, orgA.ID.String(), org)

	// Pindah ke B (anggota aktif): token berganti dan identitas kini di B.
	code, body, hdr = authE2ERequest(t, http.MethodPost, authURL("/switch-organization"), toOrg(orgB), withSession(token2, rr.CSRFToken))
	require.Equal(t, http.StatusOK, code, body)
	token3 := authE2ECookies(hdr)[0].Value
	var sr authhttp.SwitchOrganizationResponse
	authE2EData(t, body, &sr)
	require.Equal(t, orgB.ID, sr.OrganizationID)
	require.NotEqual(t, rr.CSRFToken, sr.CSRFToken)
	code, _ = meOrg(token2)
	require.Equal(t, http.StatusUnauthorized, code)
	code, org = meOrg(token3)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, orgB.ID.String(), org)

	// Jejak: event keamanan berurutan dan activity pindah organisasi.
	rows, err := db.Migrator.Query(ctx,
		`SELECT event_type FROM security_events WHERE user_id = $1 ORDER BY created_at, id`, u.ID)
	require.NoError(t, err)
	var got []string
	for rows.Next() {
		var typ string
		require.NoError(t, rows.Scan(&typ))
		got = append(got, typ)
	}
	require.NoError(t, rows.Err())
	rows.Close()
	require.Equal(t, []string{service.EventLoginSuccess, service.EventSessionRefreshed, service.EventOrganizationSwitched}, got)
	var switched int
	require.NoError(t, db.Migrator.QueryRow(ctx,
		`SELECT count(*) FROM activities WHERE actor_user_id = $1 AND action = 'auth.switch_organization'`, u.ID).Scan(&switched))
	require.Equal(t, 1, switched)
}

func TestAuthSessionManagementEndToEnd(t *testing.T) {
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

	type sess struct {
		token, csrf string
		id          string
	}
	loginBody, err := json.Marshal(map[string]string{"email": u.Email, "password": e2ePassword})
	require.NoError(t, err)
	newSession := func() sess {
		code, body, hdr := authE2ERequest(t, http.MethodPost, base+httpserver.AuthPath+"/login", string(loginBody), nil)
		require.Equal(t, http.StatusOK, code, body)
		var lr authhttp.LoginResponse
		authE2EData(t, body, &lr)
		return sess{token: authE2ECookies(hdr)[0].Value, csrf: lr.CSRFToken}
	}
	headers := func(s sess) map[string]string {
		return map[string]string{"Cookie": e2eCookieName + "=" + s.token, authhttp.HeaderCSRF: s.csrf}
	}
	listURL := base + httpserver.SecurityPath + "/sessions"
	list := func(s sess) []authhttp.SessionView {
		code, body, _ := authE2ERequest(t, http.MethodGet, listURL, "", headers(s))
		require.Equal(t, http.StatusOK, code, body)
		var out authhttp.SessionsResponse
		authE2EData(t, body, &out)
		return out.Sessions
	}
	currentID := func(views []authhttp.SessionView) string {
		var cur string
		for _, v := range views {
			if v.Current {
				require.Empty(t, cur, "tepat satu sesi bertanda current")
				cur = v.ID.String()
			}
		}
		require.NotEmpty(t, cur)
		return cur
	}

	s1, s2, s3 := newSession(), newSession(), newSession()
	views := list(s1)
	require.Len(t, views, 3)
	s1.id, s2.id = currentID(views), currentID(list(s2))
	require.NotEqual(t, s1.id, s2.id)

	// Cabut s2 dari s1: s2 mati, s1 dan s3 hidup. Mengulang menghasilkan 404.
	code, body, _ := authE2ERequest(t, http.MethodDelete, listURL+"/"+s2.id, "", headers(s1))
	require.Equal(t, http.StatusNoContent, code, body)
	code, _, _ = authE2ERequest(t, http.MethodGet, base+httpserver.AuthPath+"/me", "", headers(s2))
	require.Equal(t, http.StatusUnauthorized, code)
	code, body, _ = authE2ERequest(t, http.MethodDelete, listURL+"/"+s2.id, "", headers(s1))
	require.Equal(t, http.StatusNotFound, code, body)
	code, _, _ = authE2ERequest(t, http.MethodDelete, listURL+"/bukan-uuid", "", headers(s1))
	require.Equal(t, http.StatusNotFound, code)
	require.Len(t, list(s3), 2)

	// Pencabutan tanpa CSRF ditolak dan tidak berefek.
	code, body, _ = authE2ERequest(t, http.MethodPost, listURL+"/revoke-others", "",
		map[string]string{"Cookie": e2eCookieName + "=" + s1.token})
	require.Equal(t, http.StatusForbidden, code, body)
	require.Len(t, list(s1), 2)

	// Cabut yang lain dari s1: hanya s1 yang tersisa.
	code, body, _ = authE2ERequest(t, http.MethodPost, listURL+"/revoke-others", "", headers(s1))
	require.Equal(t, http.StatusOK, code, body)
	var ro authhttp.RevokeOthersResponse
	authE2EData(t, body, &ro)
	require.Equal(t, 1, ro.Revoked)
	code, _, _ = authE2ERequest(t, http.MethodGet, base+httpserver.AuthPath+"/me", "", headers(s3))
	require.Equal(t, http.StatusUnauthorized, code)
	require.Len(t, list(s1), 1)

	// Mencabut sesi saat ini = logout: cookie dihapus dan sesi mati.
	code, body, hdr := authE2ERequest(t, http.MethodDelete, listURL+"/"+s1.id, "", headers(s1))
	require.Equal(t, http.StatusNoContent, code, body)
	cks := authE2ECookies(hdr)
	require.Len(t, cks, 1)
	require.Less(t, cks[0].MaxAge, 0)
	code, _, _ = authE2ERequest(t, http.MethodGet, base+httpserver.AuthPath+"/me", "", headers(s1))
	require.Equal(t, http.StatusUnauthorized, code)

	var revoked, others int
	require.NoError(t, db.Migrator.QueryRow(ctx,
		`SELECT count(*) FROM security_events WHERE user_id = $1 AND event_type = $2`, u.ID, service.EventSessionRevoked).Scan(&revoked))
	require.NoError(t, db.Migrator.QueryRow(ctx,
		`SELECT count(*) FROM security_events WHERE user_id = $1 AND event_type = $2`, u.ID, service.EventSessionsRevokedOthers).Scan(&others))
	require.Equal(t, 2, revoked, "s2 oleh s1, lalu s1 sendiri")
	require.Equal(t, 1, others)
}
