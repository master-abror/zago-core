package identityhttp_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/identity/domain"
	identityhttp "github.com/master-abror/zago-core/backend/internal/identity/transport/http"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

func strPtr(s string) *string { return &s }

// decodeBody mendekode body seperti handler: lewat httpx.DecodeJSON (field tak dikenal ditolak).
func decodeBody(t *testing.T, body string, dst any) error {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/x", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return httpx.DecodeJSON(httptest.NewRecorder(), req, dst, 0)
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

func TestUpdateRequestRejectsProtectedAndUnknownFieldsFromTheBody(t *testing.T) {
	for _, key := range []string{"email", "username", "status", "id", "organization_id", "last_login_at", "deleted_at", "password", "created_at"} {
		var q identityhttp.UpdateRequest
		f := unknownFieldOf(t, decodeBody(t, fmt.Sprintf(`{%q:"x"}`, key), &q))
		require.Equal(t, key, f.Field)
		require.Equal(t, "unknown_field", f.Code)
	}
}

func TestUpdateRequestValidateRejectsAnEmptyPatch(t *testing.T) {
	for _, body := range []string{`{}`, `{"phone":null}`, `{"display_name":null,"locale":null}`} {
		var q identityhttp.UpdateRequest
		require.NoError(t, decodeBody(t, body, &q), body)
		err := q.Validate()
		require.Error(t, err, "badan %s tidak mengubah apa pun", body)
		var ae *httpx.Error
		require.ErrorAs(t, err, &ae)
		fields := fieldsOf(t, ae)
		require.Len(t, fields, 1)
		require.Equal(t, "empty_patch", fields[0].Code)
	}
}

func TestUpdateRequestValidateAcceptsEachSingleField(t *testing.T) {
	for _, body := range []string{
		`{"display_name":"a"}`, `{"first_name":"a"}`, `{"last_name":"a"}`, `{"phone":"12345"}`,
		`{"avatar_url":"https://cdn.example.org/a.png"}`, `{"locale":"en"}`, `{"timezone":"UTC"}`,
	} {
		var q identityhttp.UpdateRequest
		require.NoError(t, decodeBody(t, body, &q), body)
		require.NoError(t, q.Validate(), body)
	}
}

func TestUpdateRequestEmptyStringsSurviveAsClearValues(t *testing.T) {
	var q identityhttp.UpdateRequest
	require.NoError(t, decodeBody(t, `{"phone":"","first_name":""}`, &q))
	require.NoError(t, q.Validate(), "string kosong adalah perubahan (hapus), bukan patch kosong")
	in := q.Input()
	require.NotNil(t, in.Phone)
	require.Equal(t, "", *in.Phone)
	require.NotNil(t, in.FirstName)
	require.Equal(t, "", *in.FirstName)
	require.Nil(t, in.LastName)
}

func TestUpdateRequestCarriesEveryFieldIntoTheServiceInput(t *testing.T) {
	body := `{"display_name":"DN","first_name":"FN","last_name":"LN","phone":"12345","avatar_url":"https://cdn.example.org/a.png","locale":"id-ID","timezone":"Asia/Jakarta"}`
	var q identityhttp.UpdateRequest
	require.NoError(t, decodeBody(t, body, &q))
	in := q.Input()

	require.Equal(t, "DN", *in.DisplayName)
	require.Equal(t, "FN", *in.FirstName)
	require.Equal(t, "LN", *in.LastName)
	require.Equal(t, "12345", *in.Phone)
	require.Equal(t, "https://cdn.example.org/a.png", *in.AvatarURL)
	require.Equal(t, "id-ID", *in.Locale)
	require.Equal(t, "Asia/Jakarta", *in.Timezone)
}

// docKeys membaca nama kunci JSON dari tag struct UserResponse.
func docKeys(t *testing.T) []string {
	t.Helper()
	typ := reflect.TypeOf(identityhttp.UserResponse{})
	var keys []string
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		require.NotEmpty(t, tag, typ.Field(i).Name)
		keys = append(keys, strings.Split(tag, ",")[0])
	}
	sort.Strings(keys)
	return keys
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func fullRecord() domain.Record {
	rec := domain.Record{}
	rec.ID = id.NewID()
	rec.Username = strPtr("budi.s")
	rec.DisplayName = "Budi Santoso"
	rec.FirstName = strPtr("Budi")
	rec.LastName = strPtr("Santoso")
	rec.Email = "budi@example.org"
	rec.Phone = strPtr("+62 812 3456 7890")
	rec.AvatarURL = strPtr("https://cdn.example.org/a.png")
	rec.Locale = "id-ID"
	rec.Timezone = "Asia/Jakarta"
	rec.Status = "active"
	rec.CreatedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("WIB", 7*3600))
	rec.UpdatedAt = time.Date(2026, 2, 3, 4, 5, 6, 0, time.FixedZone("WIB", 7*3600))
	return rec
}

func TestUserFieldsExposesExactlyTheDocumentedKeys(t *testing.T) {
	rec := fullRecord()
	at := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	rec.LastLoginAt = &at

	require.Equal(t, docKeys(t), mapKeys(identityhttp.UserFields(rec, true)), "peta platform/profil = tag UserResponse")

	var tenantWant []string
	for _, k := range docKeys(t) {
		if k != "last_login_at" {
			tenantWant = append(tenantWant, k)
		}
	}
	require.Equal(t, tenantWant, mapKeys(identityhttp.UserFields(rec, false)), "peta tenant = tag UserResponse tanpa last_login_at")
}

func TestUserFieldsLastLoginIsConditional(t *testing.T) {
	rec := fullRecord()
	at := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	rec.LastLoginAt = &at

	tenant := identityhttp.UserFields(rec, false)
	_, present := tenant["last_login_at"]
	require.False(t, present, "tenant tidak melihat aktivitas login global")
	raw, err := json.Marshal(tenant)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "last_login_at")
	require.NotContains(t, string(raw), "2026-03-04")

	platform := identityhttp.UserFields(rec, true)
	require.Equal(t, at, platform["last_login_at"])

	rec.LastLoginAt = nil
	never := identityhttp.UserFields(rec, true)
	v, present := never["last_login_at"]
	require.True(t, present, "kunci ada, nilai null bila belum pernah login")
	require.Nil(t, v)
}

func TestUserFieldsNullableFieldsAreNullNotAbsent(t *testing.T) {
	rec := domain.Record{}
	rec.ID = id.NewID()
	rec.DisplayName = "Tanpa Detail"
	rec.Email = "x@example.org"
	rec.Locale = "en-US"
	rec.Timezone = "UTC"
	rec.Status = "pending"

	m := identityhttp.UserFields(rec, true)
	for _, key := range []string{"username", "first_name", "last_name", "phone", "avatar_url", "last_login_at"} {
		v, present := m[key]
		require.True(t, present, "%s: kunci harus ada", key)
		require.Nil(t, v, "%s: null bila tidak diisi", key)
	}
	raw, err := json.Marshal(m)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"phone":null`)
}

func TestUserFieldsCarryValuesAndConvertTimesToUTC(t *testing.T) {
	rec := fullRecord()
	m := identityhttp.UserFields(rec, false)

	require.Equal(t, rec.ID, m["id"])
	require.Equal(t, "budi.s", m["username"])
	require.Equal(t, "Budi Santoso", m["display_name"])
	require.Equal(t, "Budi", m["first_name"])
	require.Equal(t, "Santoso", m["last_name"])
	require.Equal(t, "budi@example.org", m["email"])
	require.Equal(t, "+62 812 3456 7890", m["phone"])
	require.Equal(t, "https://cdn.example.org/a.png", m["avatar_url"])
	require.Equal(t, "id-ID", m["locale"])
	require.Equal(t, "Asia/Jakarta", m["timezone"])
	require.Equal(t, "active", m["status"])
	require.Equal(t, rec.CreatedAt.UTC(), m["created_at"])
	require.Equal(t, rec.UpdatedAt.UTC(), m["updated_at"])
	created, ok := m["created_at"].(time.Time)
	require.True(t, ok)
	require.Equal(t, time.UTC, created.Location())
}

func TestUserFieldsNeverCarryInternalColumns(t *testing.T) {
	m := identityhttp.UserFields(fullRecord(), true)
	for _, key := range []string{"deleted_at", "password", "password_hash", "organization_id", "mfa_secret"} {
		_, present := m[key]
		require.False(t, present, key)
	}
}

func TestUserFieldsPhoneCanBeDroppedByKeyAndNothingElseDependsOnIt(t *testing.T) {
	m := identityhttp.UserFields(fullRecord(), true)
	delete(m, "phone")
	raw, err := json.Marshal(m)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "phone")
	require.NotContains(t, string(raw), "3456")
	require.Contains(t, string(raw), `"email":"budi@example.org"`)
}

func validationFields(t *testing.T, err error) []httpx.FieldError {
	t.Helper()
	require.Error(t, err)
	var ae *httpx.Error
	require.True(t, errors.As(err, &ae))
	return fieldsOf(t, ae)
}

func TestParseListDefaults(t *testing.T) {
	p, err := identityhttp.ParseList(url.Values{})
	require.NoError(t, err)
	require.Equal(t, 1, p.Page)
	require.Equal(t, domain.DefaultPageSize, p.PerPage)
	require.Equal(t, domain.Window{Offset: 0, Limit: domain.DefaultPageSize}, p.Query.Window)
	require.Equal(t, "", p.Query.Filter)
}

func TestParseListComputesOffsetAndClampsPerPage(t *testing.T) {
	p, err := identityhttp.ParseList(url.Values{"page": {"3"}, "per_page": {"20"}, "status": {"all"}})
	require.NoError(t, err)
	require.Equal(t, 3, p.Page)
	require.Equal(t, 20, p.PerPage)
	require.Equal(t, domain.Window{Offset: 40, Limit: 20}, p.Query.Window)
	require.Equal(t, "all", p.Query.Filter, "status diteruskan apa adanya; domain yang menilai")

	big, err := identityhttp.ParseList(url.Values{"per_page": {"1000"}})
	require.NoError(t, err)
	require.Equal(t, domain.MaxPageSize, big.PerPage)
	require.Equal(t, domain.MaxPageSize, big.Query.Window.Limit)
}

func TestParseListRejectsMalformedPaging(t *testing.T) {
	cases := []struct {
		query url.Values
		field string
	}{
		{url.Values{"page": {"0"}}, "page"},
		{url.Values{"page": {"abc"}}, "page"},
		{url.Values{"page": {"-1"}}, "page"},
		{url.Values{"page": {"10001"}}, "page"},
		{url.Values{"per_page": {"0"}}, "per_page"},
		{url.Values{"per_page": {"-5"}}, "per_page"},
		{url.Values{"per_page": {"x"}}, "per_page"},
	}
	for _, c := range cases {
		_, err := identityhttp.ParseList(c.query)
		fields := validationFields(t, err)
		require.Len(t, fields, 1, "%v", c.query)
		require.Equal(t, c.field, fields[0].Field, "%v", c.query)
		require.Equal(t, "invalid", fields[0].Code)
	}

	_, err := identityhttp.ParseList(url.Values{"page": {"0"}, "per_page": {"x"}})
	require.Len(t, validationFields(t, err), 2, "setiap field yang salah dilaporkan")
}

func TestParseListOffsetCannotOverflow(t *testing.T) {
	p, err := identityhttp.ParseList(url.Values{"page": {"10000"}, "per_page": {"100"}})
	require.NoError(t, err)
	require.Equal(t, 999900, p.Query.Window.Offset)
}

func TestPaginationMetaShape(t *testing.T) {
	raw, err := json.Marshal(identityhttp.PaginationMeta(2, 20, 55))
	require.NoError(t, err)
	require.JSONEq(t, `{"pagination":{"page":2,"per_page":20,"total":55}}`, string(raw))
}
