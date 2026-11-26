package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/identity/domain"
)

func strPtr(s string) *string { return &s }

func TestNormalizeDisplayName(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr error
	}{
		{"  Budi Santoso  ", "Budi Santoso", nil},
		{strings.Repeat("é", domain.MaxDisplayNameLength), strings.Repeat("é", domain.MaxDisplayNameLength), nil},
		{"", "", domain.ErrInvalidDisplayName},
		{"   ", "", domain.ErrInvalidDisplayName},
		{"Bu\x00di", "", domain.ErrInvalidDisplayName},
		{"Bu\ndi", "", domain.ErrInvalidDisplayName},
		{"\xff\xfe", "", domain.ErrInvalidDisplayName},
		{strings.Repeat("é", domain.MaxDisplayNameLength+1), "", domain.ErrInvalidDisplayName},
	}
	for _, tc := range cases {
		got, err := domain.NormalizeDisplayName(tc.in)
		if tc.wantErr != nil {
			require.ErrorIs(t, err, tc.wantErr, "masukan %q", tc.in)
			continue
		}
		require.NoError(t, err, "masukan %q", tc.in)
		require.Equal(t, tc.want, got)
	}
}

func TestNormalizePhone(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr error
	}{
		{"+62 812-3456-7890", "+62 812-3456-7890", nil},
		{"  (021) 555-1234  ", "(021) 555-1234", nil},
		{"12345", "12345", nil},
		{"+123456789012345", "+123456789012345", nil},
		{"", "", nil},
		{"   ", "", nil},
		{"1234", "", domain.ErrInvalidPhone},
		{"+", "", domain.ErrInvalidPhone},
		{"abc12345", "", domain.ErrInvalidPhone},
		{"+62+81234567", "", domain.ErrInvalidPhone},
		{"+62 812 3456 789 ext", "", domain.ErrInvalidPhone},
		{"+1234567890123456", "", domain.ErrInvalidPhone},
		{"0812 345 678 9012 3456", "", domain.ErrInvalidPhone},
		{"081234567\x00", "", domain.ErrInvalidPhone},
		{strings.Repeat("1", domain.MaxPhoneLength+1), "", domain.ErrInvalidPhone},
	}
	for _, tc := range cases {
		got, err := domain.NormalizePhone(tc.in)
		if tc.wantErr != nil {
			require.ErrorIs(t, err, tc.wantErr, "masukan %q", tc.in)
			continue
		}
		require.NoError(t, err, "masukan %q", tc.in)
		require.Equal(t, tc.want, got)
	}
}

func TestNormalizeAvatarURL(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr error
	}{
		{"https://cdn.example.org/a.png", "https://cdn.example.org/a.png", nil},
		{"  https://cdn.example.org/a.png  ", "https://cdn.example.org/a.png", nil},
		{"https://cdn.example.org:8443/img?id=1#x", "https://cdn.example.org:8443/img?id=1#x", nil},
		{"", "", nil},
		{"   ", "", nil},
		{"http://cdn.example.org/a.png", "", domain.ErrInvalidAvatarURL},
		{"ftp://cdn.example.org/a.png", "", domain.ErrInvalidAvatarURL},
		{"javascript:alert(1)", "", domain.ErrInvalidAvatarURL},
		{"data:image/png;base64,AAAA", "", domain.ErrInvalidAvatarURL},
		{"//cdn.example.org/a.png", "", domain.ErrInvalidAvatarURL},
		{"https://", "", domain.ErrInvalidAvatarURL},
		{"https:///a.png", "", domain.ErrInvalidAvatarURL},
		{"https://:443/a.png", "", domain.ErrInvalidAvatarURL},
		{"https://user:pw@cdn.example.org/a.png", "", domain.ErrInvalidAvatarURL},
		{"https://cdn.example.org/a b.png", "", domain.ErrInvalidAvatarURL},
		{"https://cdn.example.org/\x00", "", domain.ErrInvalidAvatarURL},
		{"https://cdn.example.org/" + strings.Repeat("a", domain.MaxAvatarURLLength), "", domain.ErrInvalidAvatarURL},
	}
	for _, tc := range cases {
		got, err := domain.NormalizeAvatarURL(tc.in)
		if tc.wantErr != nil {
			require.ErrorIs(t, err, tc.wantErr, "masukan %q", tc.in)
			continue
		}
		require.NoError(t, err, "masukan %q", tc.in)
		require.Equal(t, tc.want, got)
	}
}

func TestUserUpdateInputNormalizeTrimsAndKeepsEmptyAsClear(t *testing.T) {
	in := domain.UpdateInput{}
	in.DisplayName = strPtr("  Budi Santoso ")
	in.FirstName = strPtr("  Budi ")
	in.LastName = strPtr("   ")
	in.Phone = strPtr("  +62 812 3456 7890 ")
	in.AvatarURL = strPtr("  https://cdn.example.org/a.png ")
	in.Locale = strPtr(" id-ID ")
	in.Timezone = strPtr(" Asia/Jakarta ")

	out, err := in.Normalize()
	require.NoError(t, err)
	require.NotNil(t, out.DisplayName)
	require.Equal(t, "Budi Santoso", *out.DisplayName)
	require.NotNil(t, out.FirstName)
	require.Equal(t, "Budi", *out.FirstName)
	require.NotNil(t, out.LastName, "string kosong tetap menjadi pointer non-nil (hapus)")
	require.Equal(t, "", *out.LastName)
	require.NotNil(t, out.Phone)
	require.Equal(t, "+62 812 3456 7890", *out.Phone)
	require.NotNil(t, out.AvatarURL)
	require.Equal(t, "https://cdn.example.org/a.png", *out.AvatarURL)
	require.NotNil(t, out.Locale)
	require.Equal(t, "id-ID", *out.Locale)
	require.NotNil(t, out.Timezone)
	require.Equal(t, "Asia/Jakarta", *out.Timezone)
}

func TestUserUpdateInputNormalizeKeepsUnsetFieldsNil(t *testing.T) {
	out, err := domain.UpdateInput{Phone: strPtr("12345")}.Normalize()
	require.NoError(t, err)
	require.NotNil(t, out.Phone)
	require.Nil(t, out.DisplayName)
	require.Nil(t, out.FirstName)
	require.Nil(t, out.LastName)
	require.Nil(t, out.AvatarURL)
	require.Nil(t, out.Locale)
	require.Nil(t, out.Timezone)
}

func TestUserUpdateInputNormalizeNothingToUpdate(t *testing.T) {
	_, err := domain.UpdateInput{}.Normalize()
	require.ErrorIs(t, err, domain.ErrNothingToUpdate)
}

func TestUserUpdateInputNormalizeDoesNotShareCallerPointers(t *testing.T) {
	s := "Budi"
	in := domain.UpdateInput{DisplayName: &s}
	out, err := in.Normalize()
	require.NoError(t, err)
	require.NotSame(t, in.DisplayName, out.DisplayName)
	*in.DisplayName = "Berubah"
	require.Equal(t, "Budi", *out.DisplayName)
}

func TestUserUpdateInputNormalizeRejectsInvalid(t *testing.T) {
	cases := []struct {
		in      domain.UpdateInput
		wantErr error
	}{
		{domain.UpdateInput{DisplayName: strPtr("")}, domain.ErrInvalidDisplayName},
		{domain.UpdateInput{FirstName: strPtr(strings.Repeat("a", domain.MaxPersonNameLength+1))}, domain.ErrInvalidFirstName},
		{domain.UpdateInput{FirstName: strPtr("a\tb")}, domain.ErrInvalidFirstName},
		{domain.UpdateInput{LastName: strPtr(strings.Repeat("a", domain.MaxPersonNameLength+1))}, domain.ErrInvalidLastName},
		{domain.UpdateInput{LastName: strPtr("\xff")}, domain.ErrInvalidLastName},
		{domain.UpdateInput{Phone: strPtr("abc")}, domain.ErrInvalidPhone},
		{domain.UpdateInput{AvatarURL: strPtr("http://cdn.example.org/a.png")}, domain.ErrInvalidAvatarURL},
		{domain.UpdateInput{Locale: strPtr("")}, domain.ErrInvalidLocale},
		{domain.UpdateInput{Locale: strPtr("EN")}, domain.ErrInvalidLocale},
		{domain.UpdateInput{Locale: strPtr("en_US")}, domain.ErrInvalidLocale},
		{domain.UpdateInput{Locale: strPtr("en-" + strings.Repeat("a", domain.MaxLocaleLength))}, domain.ErrInvalidLocale},
		{domain.UpdateInput{Timezone: strPtr("")}, domain.ErrInvalidTimezone},
		{domain.UpdateInput{Timezone: strPtr("Local")}, domain.ErrInvalidTimezone},
		{domain.UpdateInput{Timezone: strPtr("Mars/Base")}, domain.ErrInvalidTimezone},
		{domain.UpdateInput{Timezone: strPtr("../etc/passwd")}, domain.ErrInvalidTimezone},
	}
	for i, tc := range cases {
		_, err := tc.in.Normalize()
		require.ErrorIs(t, err, tc.wantErr, "kasus %d", i)
	}
}

func TestUserUpdateInputNormalizeAcceptsValidLocalesAndTimezones(t *testing.T) {
	for _, l := range []string{"en", "id-ID", "zh-Hans-CN"} {
		_, err := domain.UpdateInput{Locale: strPtr(l)}.Normalize()
		require.NoError(t, err, "locale %q", l)
	}
	for _, z := range []string{"UTC", "Asia/Jakarta", "America/Argentina/Buenos_Aires"} {
		_, err := domain.UpdateInput{Timezone: strPtr(z)}.Normalize()
		require.NoError(t, err, "timezone %q", z)
	}
}

func TestUserUpdateInputNormalizeReportsFirstInvalidFieldInFixedOrder(t *testing.T) {
	in := domain.UpdateInput{DisplayName: strPtr(""), Phone: strPtr("abc")}
	_, err := in.Normalize()
	require.ErrorIs(t, err, domain.ErrInvalidDisplayName)
}

func TestUserStatusesForFilter(t *testing.T) {
	for _, f := range []string{"", domain.FilterOpen} {
		got, err := domain.StatusesForFilter(f)
		require.NoError(t, err, "filter %q", f)
		require.ElementsMatch(t, []string{domain.StatusPending, domain.StatusActive, domain.StatusSuspended, domain.StatusLocked}, got)
		require.NotContains(t, got, domain.StatusDeactivated)
		require.NotContains(t, got, domain.StatusDeleted)
	}

	all, err := domain.StatusesForFilter(domain.FilterAll)
	require.NoError(t, err)
	require.Len(t, all, 6)
	require.Contains(t, all, domain.StatusDeactivated)
	require.Contains(t, all, domain.StatusDeleted)

	for _, s := range []string{
		domain.StatusPending, domain.StatusActive, domain.StatusSuspended,
		domain.StatusLocked, domain.StatusDeactivated, domain.StatusDeleted,
	} {
		got, err := domain.StatusesForFilter(s)
		require.NoError(t, err, "status %q", s)
		require.Equal(t, []string{s}, got)
	}
}

func TestUserStatusesForFilterRejectsUnknownValues(t *testing.T) {
	for _, f := range []string{"bogus", "ALL", " open", "archived", "open,all"} {
		got, err := domain.StatusesForFilter(f)
		require.ErrorIs(t, err, domain.ErrInvalidFilter, "filter %q", f)
		require.Nil(t, got, "nilai tak dikenal tidak pernah menjadi semua status")
	}
}

func TestUserClampWindow(t *testing.T) {
	cases := []struct {
		in   domain.Window
		want domain.Window
	}{
		{domain.Window{Offset: -5, Limit: -1}, domain.Window{Offset: 0, Limit: domain.DefaultPageSize}},
		{domain.Window{Offset: 10, Limit: 0}, domain.Window{Offset: 10, Limit: domain.DefaultPageSize}},
		{domain.Window{Offset: 0, Limit: domain.MaxPageSize}, domain.Window{Offset: 0, Limit: domain.MaxPageSize}},
		{domain.Window{Offset: 0, Limit: domain.MaxPageSize + 1}, domain.Window{Offset: 0, Limit: domain.MaxPageSize}},
		{domain.Window{Offset: 3, Limit: 7}, domain.Window{Offset: 3, Limit: 7}},
	}
	for i, tc := range cases {
		require.Equal(t, tc.want, domain.ClampWindow(tc.in), "kasus %d", i)
	}
}

func baseUserRecord() domain.Record {
	rec := domain.Record{}
	rec.DisplayName = "Budi"
	rec.FirstName = strPtr("Budi")
	rec.Phone = strPtr("+6281")
	rec.Locale = "id-ID"
	rec.Timezone = "Asia/Jakarta"
	return rec
}

func TestUserChangedFieldsIsEmptyWhenNothingDiffers(t *testing.T) {
	in := domain.UpdateInput{}
	in.DisplayName = strPtr("Budi")
	in.FirstName = strPtr("Budi")
	in.LastName = strPtr("")
	in.Phone = strPtr("+6281")
	in.AvatarURL = strPtr("")
	in.Locale = strPtr("id-ID")
	in.Timezone = strPtr("Asia/Jakarta")

	got := baseUserRecord().ChangedFields(in)
	require.NotNil(t, got, "tidak pernah nil")
	require.Empty(t, got, "menghapus nilai yang memang kosong bukan perubahan")
}

func TestUserChangedFieldsListsDifferingFieldsSorted(t *testing.T) {
	in := domain.UpdateInput{}
	in.DisplayName = strPtr("Andi")
	in.FirstName = strPtr("")
	in.LastName = strPtr("Santoso")
	in.Phone = strPtr("")
	in.AvatarURL = strPtr("https://cdn.example.org/a.png")
	in.Locale = strPtr("en")
	in.Timezone = strPtr("UTC")

	got := baseUserRecord().ChangedFields(in)
	require.Equal(t, []string{
		domain.FieldAvatarURL, domain.FieldDisplayName, domain.FieldFirstName, domain.FieldLastName,
		domain.FieldLocale, domain.FieldPhone, domain.FieldTimezone,
	}, got)
}

func TestUserChangedFieldsIgnoresUnsetInput(t *testing.T) {
	got := baseUserRecord().ChangedFields(domain.UpdateInput{})
	require.NotNil(t, got)
	require.Empty(t, got)
}

func TestUserChangedFieldsNeverCarriesValues(t *testing.T) {
	in := domain.UpdateInput{}
	in.Phone = strPtr("+62 812 0000 1111")
	got := baseUserRecord().ChangedFields(in)
	require.Equal(t, []string{domain.FieldPhone}, got)
	for _, name := range got {
		require.NotContains(t, name, "0000", "hanya nama bidang, bukan nilai (data pribadi)")
	}
}
