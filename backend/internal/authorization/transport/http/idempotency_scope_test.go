package authzhttp_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	authzhttp "github.com/master-abror/zago-core/backend/internal/authorization/transport/http"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

func idemScopeOf(s domain.ScopeContext) (string, bool) {
	r := httptest.NewRequest(http.MethodPost, "/x", nil)
	r = r.WithContext(domain.WithScope(r.Context(), s))
	return authzhttp.IdempotencyScope(r)
}

func idemScopeFor(t *testing.T, org, group *uuid.UUID) domain.ScopeContext {
	t.Helper()
	s, err := domain.NewScope(org, group)
	require.NoError(t, err)
	return s
}

func TestIdempotencyScopeValues(t *testing.T) {
	org, other, grp := id.NewID(), id.NewID(), id.NewID()

	platform, ok := idemScopeOf(idemScopeFor(t, nil, nil))
	require.True(t, ok)
	require.Equal(t, "platform", platform)

	inOrg, ok := idemScopeOf(idemScopeFor(t, &org, nil))
	require.True(t, ok)
	require.Equal(t, "org:"+org.String(), inOrg)

	inGroup, ok := idemScopeOf(idemScopeFor(t, &org, &grp))
	require.True(t, ok)
	require.Equal(t, "grp:"+org.String()+"."+grp.String(), inGroup)

	inOther, ok := idemScopeOf(idemScopeFor(t, &other, nil))
	require.True(t, ok)

	seen := map[string]bool{}
	for _, v := range []string{platform, inOrg, inGroup, inOther} {
		require.False(t, seen[v], "scope berbeda harus menghasilkan penanda berbeda: %s", v)
		seen[v] = true
	}
}

func TestIdempotencyScopeNeedsTheScopeMiddleware(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/x", nil)
	v, ok := authzhttp.IdempotencyScope(r)
	require.False(t, ok)
	require.Empty(t, v)
}

// ScopeContext yang dirakit tangan dan tidak konsisten (grup aktif tanpa organisasi) tidak boleh
// berubah diam-diam menjadi scope platform.
func TestIdempotencyScopeRejectsGroupWithoutOrganization(t *testing.T) {
	grp := id.NewID()
	v, ok := idemScopeOf(domain.ScopeContext{ActiveGroupID: &grp})
	require.False(t, ok)
	require.Empty(t, v)
}

// Header dan body tidak pernah memengaruhi penanda scope.
func TestIdempotencyScopeIgnoresClientInput(t *testing.T) {
	org, forged := id.NewID(), id.NewID()
	r := httptest.NewRequest(http.MethodPost, "/x?organization_id="+forged.String(), nil)
	r.Header.Set("X-Organization-ID", forged.String())
	r.Header.Set("X-Active-Group-ID", forged.String())
	r = r.WithContext(domain.WithScope(r.Context(), idemScopeFor(t, &org, nil)))
	v, ok := authzhttp.IdempotencyScope(r)
	require.True(t, ok)
	require.Equal(t, "org:"+org.String(), v)
}
