package app

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/httpserver"
)

// Tabel aksi per rute organisasi (ADR-0023 butir 19). Daftar ini adalah SELURUH rute yang boleh
// dipasang organizationRoutes: rute tambahan atau aksi yang berbeda membuat tes ini gagal dan
// harus disengaja. authzRouteTable gagal bila ada rute yang mencapai handler tanpa Authorize.
func TestOrganizationRoutesMountExactlyTheConfirmedRoutesWithTheirActions(t *testing.T) {
	var buf bytes.Buffer
	log := jsonLogger(&buf)
	s, err := newAuthzStack(nil, nil, log)
	require.NoError(t, err)

	routes, err := s.organizationRoutes(nil, discardAudit{}, newTestKernel(t, log).Responder, log)

	require.NoError(t, err)
	require.False(t, routes == nil)
	got := authzRouteTable(t, []httpserver.AuthzMounter{routes})
	require.Equal(t, []string{
		"DELETE /api/v1/organizations/{id} organization.archive",
		"DELETE /api/v1/organizations/{id}/members/{user_id} organization.member.manage",
		"GET /api/v1/organizations organization.read",
		"GET /api/v1/organizations/{id} organization.read",
		"GET /api/v1/organizations/{id}/members organization.member.manage",
		"GET /api/v1/organizations/{id}/members/{user_id} organization.member.manage",
		"PATCH /api/v1/organizations/{id} organization.update",
		"POST /api/v1/organizations organization.create",
		"POST /api/v1/organizations/{id}/members organization.member.manage",
	}, got)
}

func TestOrganizationRoutesRefuseToAssembleWithoutAuditOrResponder(t *testing.T) {
	var buf bytes.Buffer
	log := jsonLogger(&buf)
	s, err := newAuthzStack(nil, nil, log)
	require.NoError(t, err)
	kern := newTestKernel(t, log)

	noAudit, err := s.organizationRoutes(nil, nil, kern.Responder, log)
	require.Error(t, err)
	require.True(t, noAudit == nil, "tanpa perekam audit tidak boleh ada mounter")

	noResponder, err := s.organizationRoutes(nil, discardAudit{}, nil, log)
	require.Error(t, err)
	require.True(t, noResponder == nil, "tanpa Responder tidak boleh ada mounter")
}
