package authhttp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/auth/service"
	authhttp "github.com/master-abror/zago-core/backend/internal/auth/transport/http"
	"github.com/master-abror/zago-core/backend/pkg/id"
	"github.com/master-abror/zago-core/backend/pkg/security"
)

// Handler POST /security/password dan GET /security/events dengan layanan palsu: validasi, pemetaan
// kesalahan, cookie, dan bentuk respons (ADR-0020 butir 42–46).

const (
	passCurrent = "password-lama-yang-benar-12"
	passNew     = "password-baru-yang-kuat-34"
)

func passwordBody(current, next string) string {
	b, _ := json.Marshal(map[string]string{"current_password": current, "new_password": next})
	return string(b)
}

func changeOK(r *handlerRig) {
	r.svc.changeRes = service.ChangePasswordResult{Issued: rotatedIssued(), RevokedOthers: 2}
}

func TestChangePasswordRotatesTheCookieReturnsANewCSRFTokenAndNeverEchoesPasswords(t *testing.T) {
	r := newHandlerRig(t)
	changeOK(r)

	w := call(r.h.ChangePassword, withIdentity(jsonReq(http.MethodPost, passwordBody(passCurrent, passNew))))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	cks := sessionCookies(w)
	require.Len(t, cks, 1)
	require.Equal(t, rotatedToken, cks[0].Value, "cookie sesi baru")
	require.True(t, cks[0].HttpOnly)
	var out authhttp.ChangePasswordResponse
	dataOf(t, w, &out)
	require.True(t, r.csrf.Verify(hSession2, out.CSRFToken), "csrf_token terikat ke sesi baru")
	require.False(t, r.csrf.Verify(hSession, out.CSRFToken), "bukan token sesi lama")
	require.Equal(t, 2, out.RevokedOtherSessions)
	require.True(t, out.ExpiresAt.Equal(hNow.Add(11*time.Hour)))
	require.NotContains(t, w.Body.String(), passCurrent)
	require.NotContains(t, w.Body.String(), passNew)
	require.NotContains(t, w.Body.String(), rotatedToken, "token sesi hanya di cookie")
	require.Equal(t, service.ChangePasswordInput{Current: passCurrent, New: passNew}, r.svc.gotChange)
	require.Equal(t, hUser, r.svc.gotChgID.UserID)
	require.Equal(t, hSession, r.svc.gotChgID.SessionID, "layanan menerima identitas sesi saat ini")
}

func TestChangePasswordWithoutASessionIs401AndDoesNotReachTheService(t *testing.T) {
	r := newHandlerRig(t)

	w := call(r.h.ChangePassword, jsonReq(http.MethodPost, passwordBody(passCurrent, passNew)))

	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Contains(t, w.Body.String(), "authentication_required")
	require.Zero(t, r.svc.callCount())
	require.Empty(t, sessionCookies(w))
}

func TestChangePasswordRejectsMissingFieldsBadJSONAndOversizedBodiesBeforeTheService(t *testing.T) {
	for name, tc := range map[string]struct {
		body   string
		status int
		code   string
	}{
		"password saat ini kosong": {passwordBody("", passNew), http.StatusBadRequest, "current_password"},
		"password baru kosong":     {passwordBody(passCurrent, ""), http.StatusBadRequest, "new_password"},
		"keduanya kosong":          {`{}`, http.StatusBadRequest, "current_password"},
		"JSON rusak":               {`{"current_password":`, http.StatusBadRequest, ""},
		"field tak dikenal":        {`{"current_password":"a","new_password":"b","role":"admin"}`, http.StatusBadRequest, ""},
		"body terlalu besar":       {passwordBody(passCurrent, strings.Repeat("z", 9<<10)), http.StatusRequestEntityTooLarge, ""},
	} {
		r := newHandlerRig(t)
		w := call(r.h.ChangePassword, withIdentity(jsonReq(http.MethodPost, tc.body)))
		require.Equal(t, tc.status, w.Code, name+": "+w.Body.String())
		if tc.code != "" {
			require.Contains(t, w.Body.String(), tc.code, name)
		}
		require.Zero(t, r.svc.callCount(), name+": layanan tidak boleh dipanggil")
		require.Empty(t, sessionCookies(w), name)
	}
}

func TestChangePasswordMapsServiceErrorsAndNeverTouchesTheCookieOnFailure(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		status int
		code   string
	}{
		"password saat ini salah": {domain.ErrInvalidCredentials, http.StatusUnauthorized, "invalid_credentials"},
		"terlalu pendek":          {security.ErrPasswordTooShort, http.StatusBadRequest, "too_short"},
		"terlalu panjang":         {security.ErrPasswordTooLong, http.StatusBadRequest, "too_long"},
		"sama dengan yang lama":   {domain.ErrSamePassword, http.StatusBadRequest, "same_as_current"},
		"sesi sudah tidak sah":    {domain.ErrUnauthenticated, http.StatusUnauthorized, "authentication_required"},
	} {
		r := newHandlerRig(t)
		r.svc.changeErr = tc.err

		w := call(r.h.ChangePassword, withIdentity(jsonReq(http.MethodPost, passwordBody(passCurrent, passNew))))

		require.Equal(t, tc.status, w.Code, name+": "+w.Body.String())
		require.Contains(t, w.Body.String(), tc.code, name)
		require.Empty(t, sessionCookies(w), name+": kegagalan tidak boleh mengganti atau menghapus cookie")
		require.NotContains(t, w.Body.String(), passCurrent, name)
		require.NotContains(t, w.Body.String(), passNew, name)
		if tc.status == http.StatusBadRequest {
			require.Contains(t, w.Body.String(), "new_password", name)
		}
	}
}

func TestChangePasswordHidesInfrastructureFailuresAsA500(t *testing.T) {
	r := newHandlerRig(t)
	r.svc.changeErr = errString("koneksi database putus: password=rahasia")

	w := call(r.h.ChangePassword, withIdentity(jsonReq(http.MethodPost, passwordBody(passCurrent, passNew))))

	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.NotContains(t, w.Body.String(), "rahasia")
	require.Empty(t, sessionCookies(w))
}

type errString string

func (e errString) Error() string { return string(e) }

// ---- GET /security/events ----

func eventRows(n int) []service.SecurityEventInfo {
	base := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	out := make([]service.SecurityEventInfo, 0, n)
	for i := 0; i < n; i++ {
		ua := "agen-uji/1.0"
		out = append(out, service.SecurityEventInfo{
			ID: id.NewID(), Type: "auth.login.success", Result: "success", CreatedAt: base.Add(-time.Duration(i) * time.Minute),
			IPAddress: "203.0.113.9", UserAgent: &ua,
		})
	}
	return out
}

func eventsReq(query string) *http.Request {
	return withIdentity(httptest.NewRequest(http.MethodGet, "/api/v1/security/events"+query, nil))
}

func TestListSecurityEventsPublishesOnlyTheAllowedFieldsAndNeverMetadata(t *testing.T) {
	r := newHandlerRig(t)
	r.svc.events = eventRows(2)
	org := hOrg
	sid := hSession
	r.svc.events[0].OrganizationID, r.svc.events[0].SessionID = &org, &sid

	w := call(r.h.ListSecurityEvents, eventsReq(""))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var rows []map[string]any
	dataOf(t, w, &rows)
	require.Len(t, rows, 2)
	keys := make([]string, 0, len(rows[0]))
	for k := range rows[0] {
		keys = append(keys, k)
	}
	require.ElementsMatch(t, []string{"id", "event_type", "result", "created_at", "ip_address", "user_agent", "organization_id", "session_id"}, keys)
	require.NotContains(t, w.Body.String(), "metadata")
	require.NotContains(t, w.Body.String(), "request_id")
	require.Equal(t, 51, r.svc.gotEventsLim, "bawaan 50 + 1 baris untuk has_more")
	require.Nil(t, r.svc.gotEventsPos, "halaman pertama tanpa posisi")
}

func TestListSecurityEventsPagesWithAnHMACCursorAndHonoursLimit(t *testing.T) {
	r := newHandlerRig(t)
	r.svc.events = eventRows(4) // layanan mengembalikan limit+1 baris -> masih ada halaman berikutnya

	w := call(r.h.ListSecurityEvents, eventsReq("?limit=3"))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, 4, r.svc.gotEventsLim)
	var env struct {
		Data []authhttp.SecurityEventView `json:"data"`
		Meta struct {
			Pagination struct {
				NextCursor *string `json:"next_cursor"`
				HasMore    bool    `json:"has_more"`
			} `json:"pagination"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env), w.Body.String())
	require.Len(t, env.Data, 3, "dipotong ke limit")
	require.True(t, env.Meta.Pagination.HasMore)
	require.NotNil(t, env.Meta.Pagination.NextCursor)

	// Cursor yang sama dibawa kembali: layanan menerima posisi baris terakhir halaman pertama.
	r2 := call(r.h.ListSecurityEvents, eventsReq("?limit=3&cursor="+*env.Meta.Pagination.NextCursor))
	require.Equal(t, http.StatusOK, r2.Code, r2.Body.String())
	require.NotNil(t, r.svc.gotEventsPos)
	require.Equal(t, env.Data[2].ID, r.svc.gotEventsPos.ID)
	require.True(t, env.Data[2].CreatedAt.Equal(r.svc.gotEventsPos.CreatedAt))
}

func TestListSecurityEventsLastPageHasNoNextCursor(t *testing.T) {
	r := newHandlerRig(t)
	r.svc.events = eventRows(2)

	w := call(r.h.ListSecurityEvents, eventsReq("?limit=5"))

	var env struct {
		Meta struct {
			Pagination struct {
				NextCursor *string `json:"next_cursor"`
				HasMore    bool    `json:"has_more"`
			} `json:"pagination"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
	require.False(t, env.Meta.Pagination.HasMore)
	require.Nil(t, env.Meta.Pagination.NextCursor)
}

func TestListSecurityEventsRejectsBadLimitAndForgedCursorsWithoutCallingTheService(t *testing.T) {
	forged := strings.Repeat("A", 40)
	for name, q := range map[string]string{
		"limit nol":         "?limit=0",
		"limit negatif":     "?limit=-3",
		"limit bukan angka": "?limit=banyak",
		"cursor palsu":      "?cursor=" + forged,
		"cursor sampah":     "?cursor=bukan-token-cursor",
	} {
		r := newHandlerRig(t)
		w := call(r.h.ListSecurityEvents, eventsReq(q))
		require.Equal(t, http.StatusBadRequest, w.Code, name+": "+w.Body.String())
		require.Zero(t, r.svc.callCount(), name)
	}
}

func TestListSecurityEventsIgnoresAnyUserSelectorAndRequiresASession(t *testing.T) {
	r := newHandlerRig(t)
	r.svc.events = eventRows(1)

	// Tanpa sesi: 401 dan layanan tidak dipanggil.
	w := call(r.h.ListSecurityEvents, httptest.NewRequest(http.MethodGet, "/api/v1/security/events", nil))
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Zero(t, r.svc.callCount())

	// Parameter pengguna tidak ada artinya: identitas selalu dari sesi (dibuktikan lewat service.User di tes layanan).
	w = call(r.h.ListSecurityEvents, eventsReq("?user_id="+id.NewID().String()))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestListSecurityEventsMapsServiceFailuresTo500WithoutLeakingDetails(t *testing.T) {
	r := newHandlerRig(t)
	r.svc.eventsErr = errString("relation security_events rusak")

	w := call(r.h.ListSecurityEvents, eventsReq(""))

	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.NotContains(t, w.Body.String(), "security_events")
}
