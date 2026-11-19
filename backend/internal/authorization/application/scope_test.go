package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

type checkCall struct{ user, org, group uuid.UUID }

type fakeChecker struct {
	member bool
	err    error
	calls  []checkCall
}

func (f *fakeChecker) IsActiveGroupMember(_ context.Context, userID, orgID, groupID uuid.UUID) (bool, error) {
	f.calls = append(f.calls, checkCall{user: userID, org: orgID, group: groupID})
	return f.member, f.err
}

func newResolver(t *testing.T, c application.GroupMembershipChecker) *application.ScopeResolver {
	t.Helper()
	r, err := application.NewScopeResolver(c, nil)
	require.NoError(t, err)
	return r
}

func tenantIdentity() authdomain.Identity {
	org := id.NewID()
	return authdomain.Identity{UserID: id.NewID(), OrganizationID: &org, SessionID: id.NewID(), AuthLevel: authdomain.AuthLevelPassword}
}

func TestNewScopeResolverRequiresChecker(t *testing.T) {
	_, err := application.NewScopeResolver(nil, nil)
	require.Error(t, err)
}

func TestResolveWithoutHeaderYieldsOrganizationOnlyAndNeverQueries(t *testing.T) {
	chk := &fakeChecker{member: true}
	ident := tenantIdentity()

	scope, err := newResolver(t, chk).Resolve(context.Background(), ident, nil)

	require.NoError(t, err)
	org, ok := scope.Tenant()
	require.True(t, ok)
	require.Equal(t, *ident.OrganizationID, org)
	_, hasGroup := scope.Group()
	require.False(t, hasGroup)
	require.Empty(t, chk.calls, "tanpa header tidak ada kueri verifikasi")
}

func TestResolveSetsActiveGroupOnlyAfterVerificationWithSessionOrganization(t *testing.T) {
	chk := &fakeChecker{member: true}
	ident := tenantIdentity()
	group := id.NewID()

	scope, err := newResolver(t, chk).Resolve(context.Background(), ident, []string{group.String()})

	require.NoError(t, err)
	got, ok := scope.Group()
	require.True(t, ok)
	require.Equal(t, group, got)
	require.Equal(t, []checkCall{{user: ident.UserID, org: *ident.OrganizationID, group: group}}, chk.calls,
		"verifikasi memakai user dan organisasi dari Identity, bukan dari request")
}

func TestResolveAcceptsUppercaseCanonicalUUID(t *testing.T) {
	chk := &fakeChecker{member: true}
	group := id.NewID()

	scope, err := newResolver(t, chk).Resolve(context.Background(), tenantIdentity(), []string{strings.ToUpper(group.String())})

	require.NoError(t, err)
	got, ok := scope.Group()
	require.True(t, ok)
	require.Equal(t, group, got)
}

func TestResolveDiscardsHeaderWhenVerificationSaysNotAMember(t *testing.T) {
	chk := &fakeChecker{member: false}
	ident := tenantIdentity()

	scope, err := newResolver(t, chk).Resolve(context.Background(), ident, []string{id.NewID().String()})

	require.NoError(t, err, "header yang gagal verifikasi dibuang, bukan galat")
	_, hasGroup := scope.Group()
	require.False(t, hasGroup)
	org, _ := scope.Tenant()
	require.Equal(t, *ident.OrganizationID, org, "organisasi sesi tetap")
	require.Len(t, chk.calls, 1)
}

func TestResolveDiscardsMalformedHeadersWithoutQueryingTheDatabase(t *testing.T) {
	group := id.NewID().String()
	compact := strings.ReplaceAll(group, "-", "")
	cases := []struct {
		name   string
		values []string
	}{
		{name: "nilai kosong", values: []string{""}},
		{name: "bukan UUID", values: []string{"bukan-uuid"}},
		{name: "UUID nol", values: []string{uuid.Nil.String()}},
		{name: "tanpa strip", values: []string{compact}},
		{name: "bentuk urn", values: []string{"urn:uuid:" + group}},
		{name: "dalam kurung kurawal", values: []string{"{" + group + "}"}},
		{name: "spasi di ujung", values: []string{group + " "}},
		{name: "36 karakter tapi bukan heksadesimal", values: []string{strings.Repeat("z", 36)}},
		{name: "header ganda sama", values: []string{group, group}},
		{name: "header ganda berbeda", values: []string{group, id.NewID().String()}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			chk := &fakeChecker{member: true}

			scope, err := newResolver(t, chk).Resolve(context.Background(), tenantIdentity(), c.values)

			require.NoError(t, err)
			_, hasGroup := scope.Group()
			require.False(t, hasGroup)
			require.Empty(t, chk.calls, "header cacat tidak boleh sampai ke database")
		})
	}
}

func TestResolveIgnoresHeaderForPlatformIdentity(t *testing.T) {
	chk := &fakeChecker{member: true}
	platform := authdomain.Identity{UserID: id.NewID(), SessionID: id.NewID(), AuthLevel: authdomain.AuthLevelPassword}

	scope, err := newResolver(t, chk).Resolve(context.Background(), platform, []string{id.NewID().String()})

	require.NoError(t, err)
	require.True(t, scope.IsPlatform())
	_, hasGroup := scope.Group()
	require.False(t, hasGroup)
	require.Empty(t, chk.calls, "identitas platform tidak punya organisasi untuk dicocokkan")
}

func TestResolveFailsClosedWhenVerificationCannotRun(t *testing.T) {
	boom := errors.New("koneksi putus")
	chk := &fakeChecker{err: boom}

	_, err := newResolver(t, chk).Resolve(context.Background(), tenantIdentity(), []string{id.NewID().String()})

	require.ErrorIs(t, err, boom)
}

func TestResolveRejectsInvalidIdentityOrganization(t *testing.T) {
	zero := uuid.Nil
	ident := authdomain.Identity{UserID: id.NewID(), OrganizationID: &zero, SessionID: id.NewID(), AuthLevel: authdomain.AuthLevelPassword}

	_, err := newResolver(t, &fakeChecker{}).Resolve(context.Background(), ident, nil)

	require.Error(t, err)
}
