package authzhttp_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	authzhttp "github.com/master-abror/zago-core/backend/internal/authorization/transport/http"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

func asgScope(t *testing.T, org *uuid.UUID) domain.ScopeContext {
	t.Helper()
	s, err := domain.NewScope(org, nil)
	require.NoError(t, err)
	return s
}

func asgFieldNames(fields []httpx.FieldError) []string {
	names := make([]string, 0, len(fields))
	for _, f := range fields {
		names = append(names, f.Field)
	}
	return names
}

func validAssignment() authzhttp.CreateAssignmentRequest {
	return authzhttp.CreateAssignmentRequest{RoleID: id.NewID(), SubjectType: "user", SubjectID: id.NewID(), ScopeType: "organization"}
}

func TestAssignmentRequestRejectsOrganizationFromTheBody(t *testing.T) {
	for _, key := range []string{"organization_id", "scope_id", "tenant_id", "assigned_by", "status", "id"} {
		err := decodeBody(t, fmt.Sprintf(`{%q:"x"}`, key), &authzhttp.CreateAssignmentRequest{})
		require.Error(t, err, key)
		require.True(t, httpx.HasCode(err, httpx.ValidationFailed), key)
		fields := fieldsOf(t, err)
		require.Len(t, fields, 1, key)
		require.Equal(t, key, fields[0].Field)
		require.Equal(t, "unknown_field", fields[0].Code)
	}
}

func TestCreateAssignmentRequestCarriesEveryFieldIntoTheWriterInput(t *testing.T) {
	roleID, subjectID, groupID, org := id.NewID(), id.NewID(), id.NewID(), id.NewID()
	body := fmt.Sprintf(`{"role_id":%q,"subject_type":"user","subject_id":%q,"scope_type":"group","group_id":%q,"expires_at":"2030-01-02T03:04:05Z"}`,
		roleID.String(), subjectID.String(), groupID.String())
	var q authzhttp.CreateAssignmentRequest
	require.NoError(t, decodeBody(t, body, &q))
	require.NoError(t, q.Validate())

	target, err := q.Target(asgScope(t, &org))
	require.NoError(t, err)
	require.Equal(t, domain.GroupTarget(org, groupID), target)

	in := q.Input(target)
	require.Equal(t, roleID, in.RoleID)
	require.Equal(t, "user", in.SubjectType)
	require.Equal(t, subjectID, in.SubjectID)
	require.Equal(t, target, in.Scope)
	require.NotNil(t, in.ExpiresAt)
	require.True(t, in.ExpiresAt.Equal(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)))
}

func TestCreateAssignmentRequestMalformedValuesAreRejectedAtDecode(t *testing.T) {
	for _, body := range []string{
		`{"role_id":"bukan-uuid"}`,
		`{"subject_id":"123"}`,
		`{"group_id":"x"}`,
		`{"expires_at":"besok"}`,
		`{"expires_at":"2030-01-02"}`,
		`{"role_id":5}`,
	} {
		err := decodeBody(t, body, &authzhttp.CreateAssignmentRequest{})
		require.Error(t, err, body)
		require.True(t, httpx.HasCode(err, httpx.ValidationFailed), body)
	}
}

func TestCreateAssignmentRequestValidateReportsEveryFieldAtOnce(t *testing.T) {
	err := (authzhttp.CreateAssignmentRequest{}).Validate()
	require.Error(t, err)
	require.True(t, httpx.HasCode(err, httpx.ValidationFailed))
	fields := fieldsOf(t, err)
	require.Equal(t, []string{"role_id", "subject_type", "subject_id", "scope_type"}, asgFieldNames(fields))
	for _, f := range fields {
		require.Equal(t, "required", f.Code, f.Field)
	}

	require.NoError(t, validAssignment().Validate())
}

func TestCreateAssignmentRequestScopeTypeMustBeOneOfThree(t *testing.T) {
	for _, st := range []string{"resource", "own", "Platform", "system", " organization"} {
		q := validAssignment()
		q.ScopeType = st
		fields := fieldsOf(t, q.Validate())
		require.Equal(t, []string{"scope_type"}, asgFieldNames(fields), st)
		require.Equal(t, "invalid", fields[0].Code, st)
	}
	q := validAssignment()
	q.ScopeType = "platform"
	require.NoError(t, q.Validate())
}

func TestCreateAssignmentRequestGroupIDOnlyForGroupScope(t *testing.T) {
	gid, nilID := id.NewID(), uuid.Nil

	q := validAssignment()
	q.ScopeType = "group"
	fields := fieldsOf(t, q.Validate())
	require.Equal(t, []string{"group_id"}, asgFieldNames(fields))
	require.Equal(t, "required", fields[0].Code)

	q.GroupID = &nilID
	fields = fieldsOf(t, q.Validate())
	require.Equal(t, []string{"group_id"}, asgFieldNames(fields))
	require.Equal(t, "required", fields[0].Code)

	q.GroupID = &gid
	require.NoError(t, q.Validate())

	for _, st := range []string{"organization", "platform"} {
		q := validAssignment()
		q.ScopeType = st
		q.GroupID = &gid
		fields := fieldsOf(t, q.Validate())
		require.Equal(t, []string{"group_id"}, asgFieldNames(fields), st)
		require.Equal(t, "invalid", fields[0].Code, st)
	}
}

func TestCreateAssignmentRequestTargetAlwaysUsesTheCallerOrganization(t *testing.T) {
	org, gid := id.NewID(), id.NewID()
	tenant := asgScope(t, &org)
	platform := asgScope(t, nil)

	q := validAssignment()
	q.ScopeType = "organization"
	got, err := q.Target(tenant)
	require.NoError(t, err)
	require.Equal(t, domain.OrganizationTarget(org), got)
	_, err = q.Target(platform)
	require.ErrorIs(t, err, domain.ErrTargetOutsideScope)

	q.ScopeType = "group"
	q.GroupID = &gid
	got, err = q.Target(tenant)
	require.NoError(t, err)
	require.Equal(t, domain.GroupTarget(org, gid), got)
	_, err = q.Target(platform)
	require.ErrorIs(t, err, domain.ErrTargetOutsideScope)

	// Scope platform dari pemanggil tenant diteruskan; penulis yang menolak dan mengauditnya.
	q.ScopeType = "platform"
	q.GroupID = nil
	got, err = q.Target(tenant)
	require.NoError(t, err)
	require.Equal(t, domain.PlatformTarget(), got)
	got, err = q.Target(platform)
	require.NoError(t, err)
	require.Equal(t, domain.PlatformTarget(), got)
}

// Target tanpa Validate tidak pernah menghasilkan GroupTarget dengan grup kosong: itu galat
// pemrograman (500), bukan target yang diteruskan ke penulis.
func TestCreateAssignmentRequestTargetRefusesShapesValidateWouldReject(t *testing.T) {
	org := id.NewID()
	tenant := asgScope(t, &org)

	q := validAssignment()
	q.ScopeType = "group"
	_, err := q.Target(tenant)
	require.ErrorIs(t, err, domain.ErrInvalidGrantTarget)

	q.ScopeType = "resource"
	_, err = q.Target(tenant)
	require.ErrorIs(t, err, domain.ErrInvalidGrantTarget)
}

func TestAssignmentResponseShapeStatusAndIndependence(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	org, grp, by := id.NewID(), id.NewID(), id.NewID()
	rec := domain.AssignmentRecord{ID: id.NewID(), RoleID: id.NewID(), SubjectType: "user", SubjectID: id.NewID(), Scope: domain.GroupTarget(org, grp), AssignedBy: &by, CreatedAt: now.Add(-time.Hour)}
	view := domain.AssignmentView{AssignmentRecord: rec, RoleName: "Group Admin", RoleSlug: "group-admin", RoleStatus: "active"}

	resp := authzhttp.NewAssignmentResponse(view, now)
	require.Equal(t, "group", resp.ScopeType)
	require.NotNil(t, resp.GroupID)
	require.Equal(t, grp, *resp.GroupID)
	require.Equal(t, "active", resp.Status)
	require.Equal(t, "Group Admin", resp.RoleName)
	require.Equal(t, "group-admin", resp.RoleSlug)
	require.Equal(t, "active", resp.RoleStatus)

	raw, err := json.Marshal(resp)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	require.NotContains(t, m, "organization_id")
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	require.ElementsMatch(t, []string{"id", "role_id", "role_name", "role_slug", "role_status", "subject_type", "subject_id", "scope_type", "group_id", "assigned_by", "expires_at", "revoked_at", "created_at", "status"}, keys)
	require.Nil(t, m["expires_at"])
	require.Nil(t, m["revoked_at"])

	*resp.GroupID = id.NewID()
	require.Equal(t, grp, *rec.Scope.GroupID, "respons tidak boleh berbagi pointer dengan record")

	expiredAt := now.Add(-time.Minute)
	rec.ExpiresAt = &expiredAt
	require.Equal(t, "expired", authzhttp.NewAssignmentResponse(domain.AssignmentView{AssignmentRecord: rec}, now).Status)

	revokedAt := now.Add(-time.Second)
	rec.RevokedAt = &revokedAt
	require.Equal(t, "revoked", authzhttp.NewAssignmentResponse(domain.AssignmentView{AssignmentRecord: rec}, now).Status)
}

func TestAssignmentResultResponseCarriesRoleSummaryAndPlatformScope(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	rec := domain.AssignmentRecord{ID: id.NewID(), RoleID: id.NewID(), SubjectType: "user", SubjectID: id.NewID(), Scope: domain.PlatformTarget(), CreatedAt: now}
	res := application.AssignmentResult{Assignment: rec, Role: domain.RoleRecord{Name: "Super Admin", Slug: "super-admin", Status: "active"}}

	resp := authzhttp.NewAssignmentResultResponse(res, now)
	require.Equal(t, "platform", resp.ScopeType)
	require.Nil(t, resp.GroupID)
	require.Nil(t, resp.AssignedBy)
	require.Equal(t, "Super Admin", resp.RoleName)
	require.Equal(t, "super-admin", resp.RoleSlug)
	require.Equal(t, "active", resp.RoleStatus)
	require.Equal(t, "active", resp.Status)
}
