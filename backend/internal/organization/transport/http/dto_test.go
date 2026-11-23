package orghttp_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/internal/organization/domain"
	orghttp "github.com/master-abror/zago-core/backend/internal/organization/transport/http"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// decodeBody mendekode body seperti handler: lewat httpx.DecodeJSON (field tak dikenal ditolak).
func decodeBody(t *testing.T, body string, dst any) error {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return httpx.DecodeJSON(httptest.NewRecorder(), req, dst, 0)
}

// jsonKeys menyerialkan v dan mengembalikan kunci tingkat atas objek JSON-nya.
func jsonKeys(t *testing.T, v any) []string {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &m))
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func unknownFieldOf(t *testing.T, err error) httpx.FieldError {
	t.Helper()
	require.Error(t, err)
	require.True(t, httpx.HasCode(err, httpx.ValidationFailed))
	var ae *httpx.Error
	require.ErrorAs(t, err, &ae)
	fields := fieldsOf(t, ae)
	require.Len(t, fields, 1)
	return fields[0]
}

func TestRequestsRejectOrganizationAndStateFromTheBody(t *testing.T) {
	targets := []func() any{
		func() any { return &orghttp.CreateOrganizationRequest{} },
		func() any { return &orghttp.UpdateOrganizationRequest{} },
		func() any { return &orghttp.AddMemberRequest{} },
	}
	for _, key := range []string{"organization_id", "tenant_id", "id", "status", "archived_at"} {
		for _, mk := range targets {
			err := decodeBody(t, fmt.Sprintf(`{%q:"x"}`, key), mk())
			f := unknownFieldOf(t, err)
			require.Equal(t, key, f.Field)
			require.Equal(t, "unknown_field", f.Code)
		}
	}
}

func TestUpdateOrganizationRequestRejectsSlugBecauseSlugIsImmutable(t *testing.T) {
	var q orghttp.UpdateOrganizationRequest
	f := unknownFieldOf(t, decodeBody(t, `{"slug":"baru"}`, &q))
	require.Equal(t, "slug", f.Field)
	require.Equal(t, "unknown_field", f.Code)
}

func TestCreateOrganizationRequestCarriesEveryFieldIntoTheServiceInput(t *testing.T) {
	body := `{"slug":"acme-id","name":"Acme Indonesia","default_locale":"id-ID","default_timezone":"Asia/Jakarta","legal_name":"PT Acme Indonesia"}`
	var q orghttp.CreateOrganizationRequest
	require.NoError(t, decodeBody(t, body, &q))

	in := q.Input()
	require.Equal(t, "acme-id", in.Slug)
	require.Equal(t, "Acme Indonesia", in.Name)
	require.Equal(t, "id-ID", in.DefaultLocale)
	require.Equal(t, "Asia/Jakarta", in.DefaultTimezone)
	require.NotNil(t, in.LegalName)
	require.Equal(t, "PT Acme Indonesia", *in.LegalName)
}

func TestCreateOrganizationRequestOptionalFieldsDefaultToEmpty(t *testing.T) {
	var q orghttp.CreateOrganizationRequest
	require.NoError(t, decodeBody(t, `{"slug":"acme","name":"Acme"}`, &q))

	in := q.Input()
	require.Empty(t, in.DefaultLocale)
	require.Empty(t, in.DefaultTimezone)
	require.Nil(t, in.LegalName)
}

func TestUpdateOrganizationRequestRejectsAnEmptyPatch(t *testing.T) {
	for _, body := range []string{`{}`, `{"name":null}`, `{"name":null,"legal_name":null,"default_locale":null,"default_timezone":null}`} {
		var q orghttp.UpdateOrganizationRequest
		require.NoError(t, decodeBody(t, body, &q), body)
		err := q.Validate()
		require.Error(t, err, body)
		require.True(t, httpx.HasCode(err, httpx.ValidationFailed), body)
		var ae *httpx.Error
		require.ErrorAs(t, err, &ae)
		require.Equal(t, "empty_patch", fieldsOf(t, ae)[0].Code, body)
	}
}

func TestUpdateOrganizationRequestEmptyLegalNameIsAChangeThatClearsIt(t *testing.T) {
	var q orghttp.UpdateOrganizationRequest
	require.NoError(t, decodeBody(t, `{"legal_name":""}`, &q))
	require.NoError(t, q.Validate(), "string kosong adalah perubahan sah (menghapus legal_name)")

	in := q.Input()
	require.NotNil(t, in.LegalName)
	require.Empty(t, *in.LegalName)
	require.Nil(t, in.Name)
	require.Nil(t, in.DefaultLocale)
	require.Nil(t, in.DefaultTimezone)
}

func TestUpdateOrganizationRequestCarriesOnlySentFields(t *testing.T) {
	var q orghttp.UpdateOrganizationRequest
	require.NoError(t, decodeBody(t, `{"name":"Baru","default_timezone":"UTC"}`, &q))
	require.NoError(t, q.Validate())

	in := q.Input()
	require.NotNil(t, in.Name)
	require.Equal(t, "Baru", *in.Name)
	require.NotNil(t, in.DefaultTimezone)
	require.Equal(t, "UTC", *in.DefaultTimezone)
	require.Nil(t, in.LegalName)
	require.Nil(t, in.DefaultLocale)
}

func TestAddMemberRequestRequiresAUserID(t *testing.T) {
	for _, body := range []string{`{}`, `{"user_id":null}`, `{"user_id":"00000000-0000-0000-0000-000000000000"}`} {
		var q orghttp.AddMemberRequest
		require.NoError(t, decodeBody(t, body, &q), body)
		err := q.Validate()
		require.Error(t, err, body)
		var ae *httpx.Error
		require.ErrorAs(t, err, &ae)
		fields := fieldsOf(t, ae)
		require.Equal(t, "user_id", fields[0].Field, body)
		require.Equal(t, "required", fields[0].Code, body)
	}

	var bad orghttp.AddMemberRequest
	err := decodeBody(t, `{"user_id":"bukan-uuid"}`, &bad)
	require.Error(t, err)
	require.True(t, httpx.HasCode(err, httpx.ValidationFailed))
}

func TestAddMemberRequestAcceptsAValidUserID(t *testing.T) {
	uid := id.NewID()
	var q orghttp.AddMemberRequest
	require.NoError(t, decodeBody(t, fmt.Sprintf(`{"user_id":%q}`, uid.String()), &q))
	require.NoError(t, q.Validate())
	require.Equal(t, uid, q.UserID)
}

func TestOrganizationResponseShapeAndUTC(t *testing.T) {
	wib := time.FixedZone("WIB", 7*3600)
	created := time.Date(2026, 11, 20, 9, 30, 0, 0, wib)
	updated := created.Add(time.Hour)
	legal := "PT Acme"

	var rec domain.Record
	rec.ID = id.NewID()
	rec.Name = "Acme"
	rec.Slug = "acme"
	rec.LegalName = &legal
	rec.Status = domain.StatusActive
	rec.DefaultLocale = "id-ID"
	rec.DefaultTimezone = "Asia/Jakarta"
	rec.CreatedAt = created
	rec.UpdatedAt = updated

	got := orghttp.NewOrganizationResponse(rec)

	require.Equal(t, rec.ID, got.ID)
	require.Equal(t, "acme", got.Slug)
	require.Equal(t, &legal, got.LegalName)
	require.Equal(t, time.UTC, got.CreatedAt.Location())
	require.True(t, got.CreatedAt.Equal(created))
	require.True(t, got.UpdatedAt.Equal(updated))
	require.Nil(t, got.ArchivedAt)
	require.ElementsMatch(t,
		[]string{"id", "name", "slug", "legal_name", "status", "default_locale", "default_timezone", "created_at", "updated_at", "archived_at"},
		jsonKeys(t, got))
}

func TestOrganizationResponseArchivedAtIsUTCAndLegalNameNullWhenAbsent(t *testing.T) {
	wib := time.FixedZone("WIB", 7*3600)
	archived := time.Date(2026, 11, 21, 8, 0, 0, 0, wib)

	var rec domain.Record
	rec.ID = id.NewID()
	rec.Status = domain.StatusArchived
	rec.ArchivedAt = &archived

	got := orghttp.NewOrganizationResponse(rec)

	require.NotNil(t, got.ArchivedAt)
	require.Equal(t, time.UTC, got.ArchivedAt.Location())
	require.True(t, got.ArchivedAt.Equal(archived))
	raw, err := json.Marshal(got)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"legal_name":null`)
	require.NotContains(t, string(raw), "organization_id")
}

func TestOrganizationResponsesEmptyListIsAnEmptyArray(t *testing.T) {
	raw, err := json.Marshal(orghttp.NewOrganizationResponses(nil))
	require.NoError(t, err)
	require.JSONEq(t, `[]`, string(raw))

	rows := []domain.Record{{ID: id.NewID()}, {ID: id.NewID()}}
	require.Len(t, orghttp.NewOrganizationResponses(rows), 2)
}

func TestMemberResponseShapeHasNoEmailOrOrganizationID(t *testing.T) {
	wib := time.FixedZone("WIB", 7*3600)
	joined := time.Date(2026, 11, 20, 10, 0, 0, 0, wib)
	inviter := id.NewID()

	var m domain.Member
	m.ID = id.NewID()
	m.OrganizationID = id.NewID()
	m.UserID = id.NewID()
	m.Status = domain.MemberActive
	m.JoinedAt = &joined
	m.InvitedBy = &inviter
	m.CreatedAt = joined
	m.UpdatedAt = joined
	m.DisplayName = "Budi"
	m.UserStatus = "active"

	got := orghttp.NewMemberResponse(m)

	require.Equal(t, m.UserID, got.UserID)
	require.Equal(t, "Budi", got.DisplayName)
	require.NotNil(t, got.JoinedAt)
	require.Equal(t, time.UTC, got.JoinedAt.Location())
	require.Equal(t, &inviter, got.InvitedBy)
	keys := jsonKeys(t, got)
	require.ElementsMatch(t,
		[]string{"id", "user_id", "display_name", "user_status", "status", "joined_at", "invited_by", "created_at", "updated_at"},
		keys)
	require.NotContains(t, keys, "email")
	require.NotContains(t, keys, "organization_id")
}

func TestMemberResponseNullsForNeverActiveMember(t *testing.T) {
	var m domain.Member
	m.ID = id.NewID()
	m.UserID = id.NewID()
	m.Status = domain.MemberInvited

	got := orghttp.NewMemberResponse(m)

	require.Nil(t, got.JoinedAt)
	require.Nil(t, got.InvitedBy)
	raw, err := json.Marshal(got)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"joined_at":null`)
	require.Contains(t, string(raw), `"invited_by":null`)
}

func TestMemberResponsesEmptyListIsAnEmptyArray(t *testing.T) {
	raw, err := json.Marshal(orghttp.NewMemberResponses(nil))
	require.NoError(t, err)
	require.JSONEq(t, `[]`, string(raw))
}
