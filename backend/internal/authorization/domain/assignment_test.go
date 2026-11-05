package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/role"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

var assignNow = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

func sysRole(slug string) domain.RoleRef {
	return domain.RoleRef{Slug: slug, Type: domain.RoleTypeSystem}
}

func orgRole(org uuid.UUID) domain.RoleRef {
	return domain.RoleRef{Slug: "finance", Type: domain.RoleTypeOrganization, OrganizationID: ptr(org)}
}

func groupRole(org, grp uuid.UUID) domain.RoleRef {
	return domain.RoleRef{Slug: "finance-staff", Type: domain.RoleTypeGroup, OrganizationID: ptr(org), GroupID: ptr(grp)}
}

func customRole(org uuid.UUID, grp *uuid.UUID) domain.RoleRef {
	return domain.RoleRef{Slug: "auditor", Type: domain.RoleTypeCustom, OrganizationID: ptr(org), GroupID: grp}
}

func userDraft(r domain.RoleRef, scope domain.GrantTarget) domain.AssignmentDraft {
	return domain.AssignmentDraft{Role: r, SubjectType: domain.SubjectUser, SubjectID: id.NewID(), Scope: scope}
}

func wantErr(t *testing.T, want, got error, name string) {
	t.Helper()
	if want == nil {
		require.NoError(t, got, name)
		return
	}
	require.ErrorIs(t, got, want, name)
}

func TestValidateAssignmentSystemTemplatesHaveExactlyOneAllowedScope(t *testing.T) {
	org, grp := id.NewID(), id.NewID()
	platform, orgScope, grpScope := domain.PlatformTarget(), domain.OrganizationTarget(org), domain.GroupTarget(org, grp)
	cases := []struct {
		name  string
		role  domain.RoleRef
		scope domain.GrantTarget
		want  error
	}{
		{name: "super-admin di platform", role: sysRole(domain.RoleSlugSuperAdmin), scope: platform},
		{name: "super-admin di organisasi", role: sysRole(domain.RoleSlugSuperAdmin), scope: orgScope, want: domain.ErrAssignmentScope},
		{name: "super-admin di grup", role: sysRole(domain.RoleSlugSuperAdmin), scope: grpScope, want: domain.ErrAssignmentScope},
		{name: "organization-admin di organisasi", role: sysRole(domain.RoleSlugOrgAdmin), scope: orgScope},
		{name: "organization-admin di platform", role: sysRole(domain.RoleSlugOrgAdmin), scope: platform, want: domain.ErrAssignmentScope},
		{name: "organization-admin di grup", role: sysRole(domain.RoleSlugOrgAdmin), scope: grpScope, want: domain.ErrAssignmentScope},
		{name: "group-admin di grup", role: sysRole(domain.RoleSlugGroupAdmin), scope: grpScope},
		{name: "group-admin di organisasi", role: sysRole(domain.RoleSlugGroupAdmin), scope: orgScope, want: domain.ErrAssignmentScope},
		{name: "group-admin di platform", role: sysRole(domain.RoleSlugGroupAdmin), scope: platform, want: domain.ErrAssignmentScope},
		{name: "member di organisasi", role: sysRole(domain.RoleSlugMember), scope: orgScope},
		{name: "member di grup", role: sysRole(domain.RoleSlugMember), scope: grpScope, want: domain.ErrAssignmentScope},
		{name: "member di platform", role: sysRole(domain.RoleSlugMember), scope: platform, want: domain.ErrAssignmentScope},
		{name: "role sistem tak dikenal ditolak di mana pun (gagal-tertutup)", role: sysRole("billing-admin"), scope: orgScope, want: domain.ErrAssignmentScope},
		{name: "role sistem tak dikenal di platform", role: sysRole("billing-admin"), scope: platform, want: domain.ErrAssignmentScope},
	}
	for _, c := range cases {
		wantErr(t, c.want, domain.ValidateAssignment(userDraft(c.role, c.scope), assignNow), c.name)
	}
}

func TestValidateAssignmentNonSystemRolesNeverGoToThePlatform(t *testing.T) {
	org, grp := id.NewID(), id.NewID()
	roles := []domain.RoleRef{orgRole(org), groupRole(org, grp), customRole(org, nil), customRole(org, &grp)}

	for _, r := range roles {
		err := domain.ValidateAssignment(userDraft(r, domain.PlatformTarget()), assignNow)
		require.ErrorIs(t, err, domain.ErrAssignmentScope, r.Type)
	}
}

func TestValidateAssignmentNonSystemRolesStayInsideTheirBoundary(t *testing.T) {
	org, other, grp, otherGrp := id.NewID(), id.NewID(), id.NewID(), id.NewID()
	cases := []struct {
		name  string
		role  domain.RoleRef
		scope domain.GrantTarget
		want  error
	}{
		{name: "role organisasi di organisasinya", role: orgRole(org), scope: domain.OrganizationTarget(org)},
		{name: "role organisasi di grup dalam organisasinya", role: orgRole(org), scope: domain.GroupTarget(org, grp)},
		{name: "role organisasi di organisasi lain", role: orgRole(org), scope: domain.OrganizationTarget(other), want: domain.ErrAssignmentScope},
		{name: "role organisasi di grup organisasi lain", role: orgRole(org), scope: domain.GroupTarget(other, grp), want: domain.ErrAssignmentScope},
		{name: "role grup di grupnya", role: groupRole(org, grp), scope: domain.GroupTarget(org, grp)},
		{name: "role grup di grup lain", role: groupRole(org, grp), scope: domain.GroupTarget(org, otherGrp), want: domain.ErrAssignmentScope},
		{name: "role grup di organisasi", role: groupRole(org, grp), scope: domain.OrganizationTarget(org), want: domain.ErrAssignmentScope},
		{name: "role grup di grup dengan id sama tetapi organisasi lain", role: groupRole(org, grp), scope: domain.GroupTarget(other, grp), want: domain.ErrAssignmentScope},
		{name: "custom tanpa grup di organisasinya", role: customRole(org, nil), scope: domain.OrganizationTarget(org)},
		{name: "custom tanpa grup di grup dalam organisasinya", role: customRole(org, nil), scope: domain.GroupTarget(org, grp)},
		{name: "custom dengan grup di grupnya", role: customRole(org, &grp), scope: domain.GroupTarget(org, grp)},
		{name: "custom dengan grup di grup lain", role: customRole(org, &grp), scope: domain.GroupTarget(org, otherGrp), want: domain.ErrAssignmentScope},
		{name: "custom dengan grup di organisasi", role: customRole(org, &grp), scope: domain.OrganizationTarget(org), want: domain.ErrAssignmentScope},
		{name: "custom di organisasi lain", role: customRole(org, nil), scope: domain.OrganizationTarget(other), want: domain.ErrAssignmentScope},
	}
	for _, c := range cases {
		wantErr(t, c.want, domain.ValidateAssignment(userDraft(c.role, c.scope), assignNow), c.name)
	}
}

func TestValidateAssignmentRejectsMalformedRolesAndScopes(t *testing.T) {
	org, grp := id.NewID(), id.NewID()
	orgScope := domain.OrganizationTarget(org)
	cases := []struct {
		name string
		role domain.RoleRef
		want error
	}{
		{name: "slug kosong", role: domain.RoleRef{Type: domain.RoleTypeSystem}, want: domain.ErrInvalidRole},
		{name: "tipe asing", role: domain.RoleRef{Slug: "x", Type: "wizard", OrganizationID: ptr(org)}, want: domain.ErrInvalidRole},
		{name: "sistem dengan organisasi", role: domain.RoleRef{Slug: "member", Type: domain.RoleTypeSystem, OrganizationID: ptr(org)}, want: domain.ErrInvalidRole},
		{name: "sistem dengan grup", role: domain.RoleRef{Slug: "member", Type: domain.RoleTypeSystem, GroupID: ptr(grp)}, want: domain.ErrInvalidRole},
		{name: "organisasi tanpa organisasi", role: domain.RoleRef{Slug: "x", Type: domain.RoleTypeOrganization}, want: domain.ErrInvalidRole},
		{name: "organisasi dengan grup", role: domain.RoleRef{Slug: "x", Type: domain.RoleTypeOrganization, OrganizationID: ptr(org), GroupID: ptr(grp)}, want: domain.ErrInvalidRole},
		{name: "grup tanpa grup", role: domain.RoleRef{Slug: "x", Type: domain.RoleTypeGroup, OrganizationID: ptr(org)}, want: domain.ErrInvalidRole},
		{name: "custom tanpa organisasi", role: domain.RoleRef{Slug: "x", Type: domain.RoleTypeCustom}, want: domain.ErrInvalidRole},
		{name: "custom organisasi id nol", role: domain.RoleRef{Slug: "x", Type: domain.RoleTypeCustom, OrganizationID: ptr(uuid.Nil)}, want: domain.ErrInvalidRole},
		{name: "custom grup id nol", role: domain.RoleRef{Slug: "x", Type: domain.RoleTypeCustom, OrganizationID: ptr(org), GroupID: ptr(uuid.Nil)}, want: domain.ErrInvalidRole},
	}
	for _, c := range cases {
		wantErr(t, c.want, domain.ValidateAssignment(userDraft(c.role, orgScope), assignNow), c.name)
	}

	err := domain.ValidateAssignment(userDraft(sysRole(domain.RoleSlugMember), domain.GrantTarget{}), assignNow)
	require.ErrorIs(t, err, domain.ErrInvalidGrantTarget, "scope nilai nol")
}

func TestValidateAssignmentRejectsArchivedRoles(t *testing.T) {
	org := id.NewID()
	r := customRole(org, nil)
	r.Archived = true

	err := domain.ValidateAssignment(userDraft(r, domain.OrganizationTarget(org)), assignNow)

	require.ErrorIs(t, err, domain.ErrRoleArchived)
}

func TestValidateAssignmentSubjects(t *testing.T) {
	org, other := id.NewID(), id.NewID()
	member := sysRole(domain.RoleSlugMember)
	superAdmin := sysRole(domain.RoleSlugSuperAdmin)
	orgScope := domain.OrganizationTarget(org)
	subject := func(r domain.RoleRef, scope domain.GrantTarget, typ string, subjectID uuid.UUID) domain.AssignmentDraft {
		return domain.AssignmentDraft{Role: r, SubjectType: typ, SubjectID: subjectID, Scope: scope}
	}
	cases := []struct {
		name string
		d    domain.AssignmentDraft
		want error
	}{
		{name: "user", d: subject(member, orgScope, domain.SubjectUser, id.NewID())},
		{name: "grup", d: subject(member, orgScope, domain.SubjectGroup, id.NewID())},
		{name: "organisasi itu sendiri", d: subject(member, orgScope, domain.SubjectOrganization, org)},
		{name: "organisasi lain", d: subject(member, orgScope, domain.SubjectOrganization, other), want: domain.ErrAssignmentSubject},
		{name: "service belum didukung", d: subject(member, orgScope, domain.SubjectService, id.NewID()), want: domain.ErrAssignmentSubject},
		{name: "tipe asing", d: subject(member, orgScope, "robot", id.NewID()), want: domain.ErrAssignmentSubject},
		{name: "tipe kosong", d: subject(member, orgScope, "", id.NewID()), want: domain.ErrAssignmentSubject},
		{name: "id nol", d: subject(member, orgScope, domain.SubjectUser, uuid.Nil), want: domain.ErrAssignmentSubject},
		{name: "platform untuk user", d: subject(superAdmin, domain.PlatformTarget(), domain.SubjectUser, id.NewID())},
		{name: "platform untuk grup", d: subject(superAdmin, domain.PlatformTarget(), domain.SubjectGroup, id.NewID()), want: domain.ErrAssignmentSubject},
		{name: "platform untuk organisasi", d: subject(superAdmin, domain.PlatformTarget(), domain.SubjectOrganization, org), want: domain.ErrAssignmentSubject},
	}
	for _, c := range cases {
		wantErr(t, c.want, domain.ValidateAssignment(c.d, assignNow), c.name)
	}
}

func TestValidateAssignmentExpiry(t *testing.T) {
	org := id.NewID()
	future := assignNow.Add(time.Hour)
	past := assignNow.Add(-time.Hour)
	now := assignNow
	with := func(r domain.RoleRef, scope domain.GrantTarget, exp *time.Time) domain.AssignmentDraft {
		d := userDraft(r, scope)
		d.ExpiresAt = exp
		return d
	}
	member := sysRole(domain.RoleSlugMember)
	superAdmin := sysRole(domain.RoleSlugSuperAdmin)

	require.NoError(t, domain.ValidateAssignment(with(member, domain.OrganizationTarget(org), &future), assignNow))
	require.NoError(t, domain.ValidateAssignment(with(member, domain.OrganizationTarget(org), nil), assignNow))
	require.ErrorIs(t, domain.ValidateAssignment(with(member, domain.OrganizationTarget(org), &past), assignNow), domain.ErrAssignmentExpiry)
	require.ErrorIs(t, domain.ValidateAssignment(with(member, domain.OrganizationTarget(org), &now), assignNow), domain.ErrAssignmentExpiry, "tepat sekarang sudah kedaluwarsa")

	require.NoError(t, domain.ValidateAssignment(with(superAdmin, domain.PlatformTarget(), nil), assignNow))
	require.ErrorIs(t, domain.ValidateAssignment(with(superAdmin, domain.PlatformTarget(), &future), assignNow), domain.ErrAssignmentExpiry,
		"super-admin platform tidak boleh kedaluwarsa diam-diam")
}

func TestCheckRevocationProtectsTheLastActivePlatformSuperAdmin(t *testing.T) {
	org := id.NewID()
	superAdmin := func(active bool) domain.AssignmentRef {
		return domain.AssignmentRef{Role: sysRole(domain.RoleSlugSuperAdmin), Scope: domain.PlatformTarget(), Active: active}
	}

	require.ErrorIs(t, domain.CheckRevocation(superAdmin(true), 1), domain.ErrLastSuperAdmin)
	require.ErrorIs(t, domain.CheckRevocation(superAdmin(true), 0), domain.ErrLastSuperAdmin, "hitungan 0 pada assignment aktif tidak masuk akal: gagal-tertutup")
	require.NoError(t, domain.CheckRevocation(superAdmin(true), 2))
	require.NoError(t, domain.CheckRevocation(superAdmin(false), 1), "assignment yang sudah tidak aktif tidak mengurangi yang aktif")

	orgAdmin := domain.AssignmentRef{Role: sysRole(domain.RoleSlugOrgAdmin), Scope: domain.OrganizationTarget(org), Active: true}
	require.NoError(t, domain.CheckRevocation(orgAdmin, 1), "hanya super-admin platform yang dilindungi")

	impostor := domain.AssignmentRef{Role: customRole(org, nil), Scope: domain.OrganizationTarget(org), Active: true}
	impostor.Role.Slug = domain.RoleSlugSuperAdmin
	require.NoError(t, domain.CheckRevocation(impostor, 1), "slug saja tidak cukup: role custom berslug super-admin bukan super-admin sistem")

	require.ErrorIs(t, domain.CheckRevocation(superAdmin(true), -1), domain.ErrInvalidAssignment)
	bad := superAdmin(true)
	bad.Role.Slug = ""
	require.ErrorIs(t, domain.CheckRevocation(bad, 5), domain.ErrInvalidRole)
}

func TestCheckRoleMutableRejectsEverySystemRole(t *testing.T) {
	org, grp := id.NewID(), id.NewID()

	for _, slug := range []string{domain.RoleSlugSuperAdmin, domain.RoleSlugOrgAdmin, domain.RoleSlugGroupAdmin, domain.RoleSlugMember, "billing-admin"} {
		require.ErrorIs(t, domain.CheckRoleMutable(sysRole(slug)), domain.ErrSystemRoleImmutable, slug)
	}
	for _, r := range []domain.RoleRef{orgRole(org), groupRole(org, grp), customRole(org, nil), customRole(org, &grp)} {
		require.NoError(t, domain.CheckRoleMutable(r), r.Type)
	}
	require.ErrorIs(t, domain.CheckRoleMutable(domain.RoleRef{Type: domain.RoleTypeCustom}), domain.ErrInvalidRole)
}

func TestValidateRoleBoundary(t *testing.T) {
	org, grp := id.NewID(), id.NewID()

	got, err := domain.ValidateRoleBoundary(domain.RoleTypeOrganization, &org, nil)
	require.NoError(t, err)
	require.Equal(t, domain.OrganizationTarget(org), got)

	got, err = domain.ValidateRoleBoundary(domain.RoleTypeGroup, &org, &grp)
	require.NoError(t, err)
	require.Equal(t, domain.GroupTarget(org, grp), got)

	got, err = domain.ValidateRoleBoundary(domain.RoleTypeCustom, &org, nil)
	require.NoError(t, err)
	require.Equal(t, domain.OrganizationTarget(org), got)

	got, err = domain.ValidateRoleBoundary(domain.RoleTypeCustom, &org, &grp)
	require.NoError(t, err)
	require.Equal(t, domain.GroupTarget(org, grp), got, "role custom dengan grup dibatasi ke grup itu")

	for _, bad := range []struct {
		name string
		typ  string
		org  *uuid.UUID
		grp  *uuid.UUID
	}{
		{name: "organisasi dengan grup", typ: domain.RoleTypeOrganization, org: &org, grp: &grp},
		{name: "organisasi tanpa organisasi", typ: domain.RoleTypeOrganization},
		{name: "grup tanpa grup", typ: domain.RoleTypeGroup, org: &org},
		{name: "custom tanpa organisasi", typ: domain.RoleTypeCustom, grp: &grp},
		{name: "organisasi id nol", typ: domain.RoleTypeCustom, org: ptr(uuid.Nil)},
		{name: "tipe asing", typ: "wizard", org: &org},
		{name: "tipe kosong", typ: "", org: &org},
	} {
		_, err := domain.ValidateRoleBoundary(bad.typ, bad.org, bad.grp)
		require.ErrorIs(t, err, domain.ErrInvalidRole, bad.name)
	}
}

func TestValidateRoleBoundaryNeverCreatesSystemRoles(t *testing.T) {
	org := id.NewID()

	for _, o := range []*uuid.UUID{nil, &org} {
		_, err := domain.ValidateRoleBoundary(domain.RoleTypeSystem, o, nil)
		require.ErrorIs(t, err, domain.ErrRoleTypeForbidden)
	}
}

func TestSystemTemplateTableMatchesTheRoleCatalog(t *testing.T) {
	require.Equal(t, role.SlugSuperAdmin, domain.RoleSlugSuperAdmin)
	require.Equal(t, role.SlugOrgAdmin, domain.RoleSlugOrgAdmin)
	require.Equal(t, role.SlugGroupAdmin, domain.RoleSlugGroupAdmin)
	require.Equal(t, role.SlugMember, domain.RoleSlugMember)

	org, grp := id.NewID(), id.NewID()
	scopes := []domain.GrantTarget{domain.PlatformTarget(), domain.OrganizationTarget(org), domain.GroupTarget(org, grp)}
	for _, sr := range role.SystemRoles() {
		allowed := 0
		for _, s := range scopes {
			if domain.ValidateAssignment(userDraft(sysRole(sr.Slug), s), assignNow) == nil {
				allowed++
			}
		}
		require.Equal(t, 1, allowed, "role sistem %q harus punya tepat satu scope (tambahkan ke systemTemplateLevels)", sr.Slug)
	}
}

func TestOnlySuperAdminCanBeAssignedOnThePlatformAmongAllSystemRoles(t *testing.T) {
	for _, sr := range role.SystemRoles() {
		err := domain.ValidateAssignment(userDraft(sysRole(sr.Slug), domain.PlatformTarget()), assignNow)
		if sr.Slug == role.SlugSuperAdmin {
			require.NoError(t, err)
			continue
		}
		require.ErrorIs(t, err, domain.ErrAssignmentScope, sr.Slug)
	}
}
