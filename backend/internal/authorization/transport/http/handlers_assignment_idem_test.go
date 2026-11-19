package authzhttp_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	authzhttp "github.com/master-abror/zago-core/backend/internal/authorization/transport/http"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Replay Idempotency-Key pada POST /role-assignments dengan Redis 8 asli dan Idempotency
// sungguhan (ADR-0022 butir 67). Subjek idempotensi adalah user id, jadi tanpa opsi Scope pemanggil
// yang sama di dua organisasi akan memutar ulang respons organisasi lain.

func ahIdempotency(t *testing.T, rdb redis.UniversalClient, withScope bool) func(http.Handler) http.Handler {
	t.Helper()
	o := httpx.IdempotencyOptions{
		Redis:     rdb,
		Responder: httpx.NewResponder(slog.New(slog.NewTextHandler(io.Discard, nil))),
		Subject: func(r *http.Request) (string, bool) {
			ident, ok := authdomain.IdentityFromContext(r.Context())
			if !ok {
				return "", false
			}
			return ident.UserID.String(), true
		},
		Route: func(r *http.Request) string { return chi.RouteContext(r.Context()).RoutePattern() },
	}
	if withScope {
		o.Scope = authzhttp.IdempotencyScope
	}
	i, err := httpx.NewIdempotency(o)
	require.NoError(t, err)
	return i.Require()
}

func newIdemRig(t *testing.T, withScope bool) (*ahRig, redis.UniversalClient) {
	t.Helper()
	rdb := testkit.Redis(t).Client(t)
	return newAssignmentRig(t, tenantIdentity(), ahIdempotency(t, rdb, withScope)), rdb
}

// switchTo memindahkan pemanggil yang SAMA (user id tetap) ke organisasi dan grup aktif lain.
func (g *ahRig) switchTo(t *testing.T, org uuid.UUID, group *uuid.UUID) {
	t.Helper()
	g.ident.OrganizationID = &org
	s, err := domain.NewScope(&org, group)
	require.NoError(t, err)
	g.scope = s
}

func ahIdemKeys(t *testing.T, rdb redis.UniversalClient) []string {
	t.Helper()
	keys, err := rdb.Keys(context.Background(), "idem:*").Result()
	require.NoError(t, err)
	return keys
}

// KONTROL: tanpa opsi Scope, pemanggil yang sama yang pindah organisasi mendapat replay respons
// organisasi sebelumnya dan penulis tidak dipanggil untuk organisasi baru. Tes ini membuktikan
// risiko yang ditutup IdempotencyScope; bila kelak gagal, periksa ADR-0022 butir 67.
func TestAssignmentCreateWithoutScopeOptionReplaysAcrossOrganizations(t *testing.T) {
	rg, _ := newIdemRig(t, false)
	body := ahPostBody("organization")

	first := rg.doKey(http.MethodPost, "/role-assignments", body, "key-0001")
	rg.switchTo(t, id.NewID(), nil)
	second := rg.doKey(http.MethodPost, "/role-assignments", body, "key-0001")

	require.Equal(t, http.StatusCreated, first.Code, first.Body.String())
	require.Equal(t, "true", second.Header().Get("Idempotent-Replayed"))
	require.Len(t, rg.write.calls, 1, "organisasi kedua tidak pernah dieksekusi: inilah risikonya")
}

func TestAssignmentCreateReplayNeverCrossesOrganizations(t *testing.T) {
	rg, _ := newIdemRig(t, true)
	orgA := *rg.ident.OrganizationID
	body := ahPostBody("organization")

	first := rg.doKey(http.MethodPost, "/role-assignments", body, "key-0001")
	require.Equal(t, http.StatusCreated, first.Code, first.Body.String())
	require.Empty(t, first.Header().Get("Idempotent-Replayed"))

	rg.switchTo(t, id.NewID(), nil)
	second := rg.doKey(http.MethodPost, "/role-assignments", body, "key-0001")
	require.Equal(t, http.StatusCreated, second.Code, second.Body.String())
	require.Empty(t, second.Header().Get("Idempotent-Replayed"), "organisasi lain harus dieksekusi, bukan diputar ulang")
	require.Len(t, rg.write.calls, 2)
	require.NotEqual(t, rg.write.grantors[0].Scope, rg.write.grantors[1].Scope)

	rg.switchTo(t, orgA, nil)
	again := rg.doKey(http.MethodPost, "/role-assignments", body, "key-0001")
	require.Equal(t, "true", again.Header().Get("Idempotent-Replayed"), "replay sah di scope yang sama")
	require.Len(t, rg.write.calls, 2)
}

func TestAssignmentCreateReplayNeverCrossesActiveGroups(t *testing.T) {
	rg, _ := newIdemRig(t, true)
	org := *rg.ident.OrganizationID
	g1, g2 := id.NewID(), id.NewID()
	body := ahPostBody("organization")

	for _, group := range []*uuid.UUID{nil, &g1, &g2} {
		rg.switchTo(t, org, group)
		w := rg.doKey(http.MethodPost, "/role-assignments", body, "key-0001")
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		require.Empty(t, w.Header().Get("Idempotent-Replayed"))
	}
	require.Len(t, rg.write.calls, 3, "grup aktif berbeda adalah scope berbeda")

	rg.switchTo(t, org, &g1)
	w := rg.doKey(http.MethodPost, "/role-assignments", body, "key-0001")
	require.Equal(t, "true", w.Header().Get("Idempotent-Replayed"))
	require.Len(t, rg.write.calls, 3)
}

func TestAssignmentCreateAuthorizeRunsBeforeTheIdempotencyKeyIsRequired(t *testing.T) {
	rg, rdb := newIdemRig(t, true)
	rg.authz.decision = domain.Decision{Allowed: false, Reason: domain.ReasonNoPermission}

	w := rg.do(http.MethodPost, "/role-assignments", ahPostBody("organization"))

	require.Equal(t, http.StatusForbidden, w.Code, "tanpa hak, kunci tidak pernah diminta: "+w.Body.String())
	require.Equal(t, httpx.PermissionDenied.Name, rhErrCode(t, w))
	require.Empty(t, rg.write.calls)
	require.Empty(t, ahIdemKeys(t, rdb))

	rg.authz.decision = domain.Decision{Allowed: true, Reason: domain.ReasonGranted, Level: domain.LevelOrganization}
	w = rg.do(http.MethodPost, "/role-assignments", ahPostBody("organization"))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Equal(t, "required", ahFields(t, w)["Idempotency-Key"])
	require.Empty(t, rg.write.calls)
}

func TestAssignmentCreateRevokedPermissionIsNotAnsweredFromAReplay(t *testing.T) {
	rg, _ := newIdemRig(t, true)
	body := ahPostBody("organization")

	first := rg.doKey(http.MethodPost, "/role-assignments", body, "key-0001")
	require.Equal(t, http.StatusCreated, first.Code, first.Body.String())

	rg.authz.decision = domain.Decision{Allowed: false, Reason: domain.ReasonNoPermission}
	second := rg.doKey(http.MethodPost, "/role-assignments", body, "key-0001")

	require.Equal(t, http.StatusForbidden, second.Code, second.Body.String())
	require.Empty(t, second.Header().Get("Idempotent-Replayed"), "hak yang dicabut tidak boleh dijawab dari replay")
	require.Len(t, rg.write.calls, 1)
}

func TestAssignmentCreateFailureIsNotStoredSoTheSameKeyCanRetry(t *testing.T) {
	rg, rdb := newIdemRig(t, true)
	body := ahPostBody("organization")
	rg.write.err = domain.ErrAssignmentExists

	first := rg.doKey(http.MethodPost, "/role-assignments", body, "key-0001")
	require.Equal(t, http.StatusConflict, first.Code, first.Body.String())
	require.Equal(t, authzhttp.AssignmentExists.Name, rhErrCode(t, first))
	require.Empty(t, ahIdemKeys(t, rdb), "respons galat tidak disimpan")

	rg.write.err = nil
	second := rg.doKey(http.MethodPost, "/role-assignments", body, "key-0001")
	require.Equal(t, http.StatusCreated, second.Code, second.Body.String())
	require.Empty(t, second.Header().Get("Idempotent-Replayed"))
	require.Len(t, rg.write.calls, 2)
}

func TestAssignmentCreateScopeIsPartOfTheStoredKey(t *testing.T) {
	rg, rdb := newIdemRig(t, true)
	org := *rg.ident.OrganizationID

	w := rg.doKey(http.MethodPost, "/role-assignments", ahPostBody("organization"), "key-0001")
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	keys := ahIdemKeys(t, rdb)
	require.Len(t, keys, 1)
	require.Equal(t, "idem:"+rg.ident.UserID.String()+":org:"+org.String()+":POST:/role-assignments:key-0001", keys[0])
}
