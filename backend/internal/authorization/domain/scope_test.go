package domain_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

func ptr(u uuid.UUID) *uuid.UUID { return &u }

func TestNewScopeRejectsInvariantViolations(t *testing.T) {
	org, grp := id.NewID(), id.NewID()
	cases := []struct {
		name     string
		org, grp *uuid.UUID
	}{
		{name: "organisasi UUID nol", org: ptr(uuid.Nil)},
		{name: "grup UUID nol", org: ptr(org), grp: ptr(uuid.Nil)},
		{name: "grup tanpa organisasi", grp: ptr(grp)},
		{name: "grup nol tanpa organisasi", grp: ptr(uuid.Nil)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := domain.NewScope(c.org, c.grp)
			require.ErrorIs(t, err, domain.ErrInvalidScope)
		})
	}
}

func TestNewScopeAcceptsPlatformTenantAndGroup(t *testing.T) {
	org, grp := id.NewID(), id.NewID()

	platform, err := domain.NewScope(nil, nil)
	require.NoError(t, err)
	require.True(t, platform.IsPlatform())
	_, ok := platform.Tenant()
	require.False(t, ok)
	_, ok = platform.Group()
	require.False(t, ok)

	tenant, err := domain.NewScope(ptr(org), nil)
	require.NoError(t, err)
	require.False(t, tenant.IsPlatform())
	got, ok := tenant.Tenant()
	require.True(t, ok)
	require.Equal(t, org, got)
	_, ok = tenant.Group()
	require.False(t, ok)

	withGroup, err := domain.NewScope(ptr(org), ptr(grp))
	require.NoError(t, err)
	gotGrp, ok := withGroup.Group()
	require.True(t, ok)
	require.Equal(t, grp, gotGrp)
}

func TestNewScopeCopiesPointedValues(t *testing.T) {
	org := id.NewID()
	want := org

	s, err := domain.NewScope(&org, nil)
	require.NoError(t, err)
	org = id.NewID() // pemanggil mengubah nilai yang ditunjuk

	got, _ := s.Tenant()
	require.Equal(t, want, got, "scope tidak boleh berubah lewat pointer milik pemanggil")
}

func TestScopeFromContextIsMissingUntilMiddlewareSetsIt(t *testing.T) {
	_, ok := domain.ScopeFromContext(context.Background())
	require.False(t, ok)
}

func TestScopeInContextIsACopyBothWays(t *testing.T) {
	org, grp := id.NewID(), id.NewID()
	in, err := domain.NewScope(ptr(org), ptr(grp))
	require.NoError(t, err)

	ctx := domain.WithScope(context.Background(), in)
	*in.OrganizationID = id.NewID() // ubah sumber sesudah disimpan
	first, ok := domain.ScopeFromContext(ctx)
	require.True(t, ok)
	*first.ActiveGroupID = id.NewID() // ubah hasil bacaan
	second, ok := domain.ScopeFromContext(ctx)
	require.True(t, ok)

	gotOrg, _ := second.Tenant()
	gotGrp, _ := second.Group()
	require.Equal(t, org, gotOrg)
	require.Equal(t, grp, gotGrp)
}
