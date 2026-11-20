package domain_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/organization/domain"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

func strPtr(s string) *string { return &s }

func validRecord(t *testing.T) domain.Record {
	t.Helper()
	rec, err := domain.NewRecord(id.NewID(), domain.CreateInput{Name: "Acme", Slug: "acme"})
	require.NoError(t, err)
	return rec
}

func TestNewRecordAppliesDefaultsAndNormalizes(t *testing.T) {
	oid := id.NewID()
	rec, err := domain.NewRecord(oid, domain.CreateInput{Name: "  Acme Corp  ", Slug: "  Acme-Corp "})
	require.NoError(t, err)
	require.Equal(t, oid, rec.ID)
	require.Equal(t, "Acme Corp", rec.Name)
	require.Equal(t, "acme-corp", rec.Slug)
	require.Nil(t, rec.LegalName)
	require.Equal(t, domain.StatusActive, rec.Status)
	require.Equal(t, domain.DefaultLocale, rec.DefaultLocale)
	require.Equal(t, domain.DefaultTimezone, rec.DefaultTimezone)
	require.NoError(t, rec.Validate())
}

func TestNewRecordKeepsExplicitOptionalFields(t *testing.T) {
	rec, err := domain.NewRecord(id.NewID(), domain.CreateInput{
		Name:            "Acme",
		Slug:            "acme",
		LegalName:       strPtr("  Acme Corporation Ltd. "),
		DefaultLocale:   "id-ID",
		DefaultTimezone: "Asia/Jakarta",
	})
	require.NoError(t, err)
	require.NotNil(t, rec.LegalName)
	require.Equal(t, "Acme Corporation Ltd.", *rec.LegalName)
	require.Equal(t, "id-ID", rec.DefaultLocale)
	require.Equal(t, "Asia/Jakarta", rec.DefaultTimezone)
	require.NoError(t, rec.Validate())
}

func TestNewRecordTreatsBlankLegalNameAsAbsent(t *testing.T) {
	rec, err := domain.NewRecord(id.NewID(), domain.CreateInput{Name: "Acme", Slug: "acme", LegalName: strPtr("   ")})
	require.NoError(t, err)
	require.Nil(t, rec.LegalName)
}

func TestNewRecordCountsRunesNotBytes(t *testing.T) {
	name := strings.Repeat("é", domain.MaxNameLength)
	rec, err := domain.NewRecord(id.NewID(), domain.CreateInput{Name: name, Slug: "acme"})
	require.NoError(t, err, "200 rune multi-byte sah")
	require.Equal(t, name, rec.Name)

	_, err = domain.NewRecord(id.NewID(), domain.CreateInput{Name: name + "é", Slug: "acme"})
	require.ErrorIs(t, err, domain.ErrInvalidName)
}

func TestNewRecordRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name string
		in   domain.CreateInput
		want error
	}{
		{"name kosong", domain.CreateInput{Name: " ", Slug: "acme"}, domain.ErrInvalidName},
		{"name terlalu panjang", domain.CreateInput{Name: strings.Repeat("a", 201), Slug: "acme"}, domain.ErrInvalidName},
		{"name berkarakter kontrol", domain.CreateInput{Name: "Ac\nme", Slug: "acme"}, domain.ErrInvalidName},
		{"name bukan UTF-8", domain.CreateInput{Name: "Ac\xffme", Slug: "acme"}, domain.ErrInvalidName},
		{"slug kosong", domain.CreateInput{Name: "Acme", Slug: ""}, domain.ErrInvalidSlug},
		{"slug terlalu pendek", domain.CreateInput{Name: "Acme", Slug: "a"}, domain.ErrInvalidSlug},
		{"slug diawali tanda hubung", domain.CreateInput{Name: "Acme", Slug: "-acme"}, domain.ErrInvalidSlug},
		{"slug diakhiri tanda hubung", domain.CreateInput{Name: "Acme", Slug: "acme-"}, domain.ErrInvalidSlug},
		{"slug dua tanda hubung", domain.CreateInput{Name: "Acme", Slug: "ac--me"}, domain.ErrInvalidSlug},
		{"slug garis bawah", domain.CreateInput{Name: "Acme", Slug: "ac_me"}, domain.ErrInvalidSlug},
		{"slug berspasi", domain.CreateInput{Name: "Acme", Slug: "ac me"}, domain.ErrInvalidSlug},
		{"slug bertitik", domain.CreateInput{Name: "Acme", Slug: "ac.me"}, domain.ErrInvalidSlug},
		{"slug tanda Kelvin", domain.CreateInput{Name: "Acme", Slug: "\u212acme"}, domain.ErrInvalidSlug},
		{"slug terlalu panjang", domain.CreateInput{Name: "Acme", Slug: strings.Repeat("a", 101)}, domain.ErrInvalidSlug},
		{"legal name terlalu panjang", domain.CreateInput{Name: "Acme", Slug: "acme", LegalName: strPtr(strings.Repeat("a", 256))}, domain.ErrInvalidLegalName},
		{"legal name berkarakter kontrol", domain.CreateInput{Name: "Acme", Slug: "acme", LegalName: strPtr("A\tB")}, domain.ErrInvalidLegalName},
		{"locale garis bawah", domain.CreateInput{Name: "Acme", Slug: "acme", DefaultLocale: "en_US"}, domain.ErrInvalidLocale},
		{"locale kata penuh", domain.CreateInput{Name: "Acme", Slug: "acme", DefaultLocale: "english-US"}, domain.ErrInvalidLocale},
		{"locale diakhiri tanda hubung", domain.CreateInput{Name: "Acme", Slug: "acme", DefaultLocale: "en-"}, domain.ErrInvalidLocale},
		{"timezone tidak ada", domain.CreateInput{Name: "Acme", Slug: "acme", DefaultTimezone: "Mars/Base"}, domain.ErrInvalidTimezone},
		{"timezone Local", domain.CreateInput{Name: "Acme", Slug: "acme", DefaultTimezone: "Local"}, domain.ErrInvalidTimezone},
		{"timezone path traversal", domain.CreateInput{Name: "Acme", Slug: "acme", DefaultTimezone: "../etc/passwd"}, domain.ErrInvalidTimezone},
		{"timezone berkarakter aneh", domain.CreateInput{Name: "Acme", Slug: "acme", DefaultTimezone: "Asia/Jakarta;DROP"}, domain.ErrInvalidTimezone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := domain.NewRecord(id.NewID(), c.in)
			require.ErrorIs(t, err, c.want)
		})
	}
}

func TestNewRecordRejectsNilID(t *testing.T) {
	_, err := domain.NewRecord(uuid.Nil, domain.CreateInput{Name: "Acme", Slug: "acme"})
	require.ErrorIs(t, err, domain.ErrInvalidID)
}

func TestNewRecordAcceptsCommonLocalesAndTimezones(t *testing.T) {
	for _, locale := range []string{"en", "fil", "en-US", "id-ID", "zh-Hant-TW"} {
		_, err := domain.NewRecord(id.NewID(), domain.CreateInput{Name: "Acme", Slug: "acme", DefaultLocale: locale})
		require.NoError(t, err, locale)
	}
	for _, zone := range []string{"UTC", "Asia/Jakarta", "America/Argentina/Buenos_Aires", "Etc/GMT+5"} {
		_, err := domain.NewRecord(id.NewID(), domain.CreateInput{Name: "Acme", Slug: "acme", DefaultTimezone: zone})
		require.NoError(t, err, zone)
	}
}

func TestRecordValidateRejectsNonNormalForm(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*domain.Record)
		want   error
	}{
		{"id kosong", func(r *domain.Record) { r.ID = uuid.Nil }, domain.ErrInvalidID},
		{"name berspasi tepi", func(r *domain.Record) { r.Name = " Acme" }, domain.ErrInvalidName},
		{"slug huruf besar", func(r *domain.Record) { r.Slug = "Acme" }, domain.ErrInvalidSlug},
		{"legal name kosong", func(r *domain.Record) { r.LegalName = strPtr("") }, domain.ErrInvalidLegalName},
		{"locale kosong", func(r *domain.Record) { r.DefaultLocale = "" }, domain.ErrInvalidLocale},
		{"timezone tidak ada", func(r *domain.Record) { r.DefaultTimezone = "Mars/Base" }, domain.ErrInvalidTimezone},
		{"status tidak dikenal", func(r *domain.Record) { r.Status = "bogus" }, domain.ErrInvalidStatus},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := validRecord(t)
			c.mutate(&rec)
			require.ErrorIs(t, rec.Validate(), c.want)
		})
	}
}

func TestUpdateInputNormalize(t *testing.T) {
	t.Run("tanpa bidang", func(t *testing.T) {
		_, err := domain.UpdateInput{}.Normalize()
		require.ErrorIs(t, err, domain.ErrNothingToUpdate)
	})
	t.Run("menormalkan dan menyalin pointer", func(t *testing.T) {
		name := "  New Name  "
		in := domain.UpdateInput{Name: &name}
		out, err := in.Normalize()
		require.NoError(t, err)
		require.NotNil(t, out.Name)
		require.Equal(t, "New Name", *out.Name)
		require.NotSame(t, in.Name, out.Name)
		require.Nil(t, out.LegalName)
		require.Nil(t, out.DefaultLocale)
		require.Nil(t, out.DefaultTimezone)
	})
	t.Run("legal name kosong berarti hapus", func(t *testing.T) {
		out, err := domain.UpdateInput{LegalName: strPtr("  ")}.Normalize()
		require.NoError(t, err)
		require.NotNil(t, out.LegalName)
		require.Equal(t, "", *out.LegalName)
	})
	t.Run("name kosong ditolak", func(t *testing.T) {
		_, err := domain.UpdateInput{Name: strPtr(" ")}.Normalize()
		require.ErrorIs(t, err, domain.ErrInvalidName)
	})
	t.Run("locale kosong ditolak", func(t *testing.T) {
		_, err := domain.UpdateInput{DefaultLocale: strPtr("")}.Normalize()
		require.ErrorIs(t, err, domain.ErrInvalidLocale)
	})
	t.Run("timezone kosong ditolak", func(t *testing.T) {
		_, err := domain.UpdateInput{DefaultTimezone: strPtr("")}.Normalize()
		require.ErrorIs(t, err, domain.ErrInvalidTimezone)
	})
	t.Run("timezone tidak ada ditolak", func(t *testing.T) {
		_, err := domain.UpdateInput{DefaultTimezone: strPtr("Mars/Base")}.Normalize()
		require.ErrorIs(t, err, domain.ErrInvalidTimezone)
	})
	t.Run("semua bidang sah", func(t *testing.T) {
		out, err := domain.UpdateInput{
			Name:            strPtr("Acme"),
			LegalName:       strPtr("Acme Ltd"),
			DefaultLocale:   strPtr("id-ID"),
			DefaultTimezone: strPtr("Asia/Jakarta"),
		}.Normalize()
		require.NoError(t, err)
		require.Equal(t, "Acme", *out.Name)
		require.Equal(t, "Acme Ltd", *out.LegalName)
		require.Equal(t, "id-ID", *out.DefaultLocale)
		require.Equal(t, "Asia/Jakarta", *out.DefaultTimezone)
	})
}

func TestPredecessorStatuses(t *testing.T) {
	from, err := domain.PredecessorStatuses(domain.StatusActive)
	require.NoError(t, err)
	require.Equal(t, []string{domain.StatusSuspended}, from)

	from, err = domain.PredecessorStatuses(domain.StatusSuspended)
	require.NoError(t, err)
	require.Equal(t, []string{domain.StatusActive}, from)

	from, err = domain.PredecessorStatuses(domain.StatusArchived)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{domain.StatusActive, domain.StatusSuspended}, from)

	_, err = domain.PredecessorStatuses("bogus")
	require.ErrorIs(t, err, domain.ErrInvalidStatus)
}

func TestArchivedHasNoWayOutAndNoSelfTransition(t *testing.T) {
	for _, to := range []string{domain.StatusActive, domain.StatusSuspended, domain.StatusArchived} {
		from, err := domain.PredecessorStatuses(to)
		require.NoError(t, err)
		require.NotContains(t, from, domain.StatusArchived, "archived tidak boleh menjadi status asal ke "+to)
		require.NotContains(t, from, to, "perpindahan ke status yang sama tidak diizinkan: "+to)
	}
}

func TestValidStatus(t *testing.T) {
	require.True(t, domain.ValidStatus(domain.StatusActive))
	require.True(t, domain.ValidStatus(domain.StatusSuspended))
	require.True(t, domain.ValidStatus(domain.StatusArchived))
	require.False(t, domain.ValidStatus(""))
	require.False(t, domain.ValidStatus("deleted"))
}

func TestStatusesForFilter(t *testing.T) {
	open := []string{domain.StatusActive, domain.StatusSuspended}
	got, err := domain.StatusesForFilter("")
	require.NoError(t, err)
	require.Equal(t, open, got, "bawaan = open")
	got, err = domain.StatusesForFilter(domain.FilterOpen)
	require.NoError(t, err)
	require.Equal(t, open, got)
	got, err = domain.StatusesForFilter(domain.FilterAll)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{domain.StatusActive, domain.StatusSuspended, domain.StatusArchived}, got)
	got, err = domain.StatusesForFilter(domain.StatusArchived)
	require.NoError(t, err)
	require.Equal(t, []string{domain.StatusArchived}, got)

	_, err = domain.StatusesForFilter("bogus")
	require.ErrorIs(t, err, domain.ErrInvalidFilter)
}

func TestAccessPlatform(t *testing.T) {
	a := domain.PlatformAccess()
	require.True(t, a.Valid())
	require.True(t, a.IsPlatform())
	_, ok := a.Tenant()
	require.False(t, ok)
	filter, err := a.Filter()
	require.NoError(t, err)
	require.True(t, filter == nil, "akses platform tanpa pembatas tenant")
}

func TestAccessTenant(t *testing.T) {
	oid := id.NewID()
	a, err := domain.TenantAccess(oid)
	require.NoError(t, err)
	require.True(t, a.Valid())
	require.False(t, a.IsPlatform())
	got, ok := a.Tenant()
	require.True(t, ok)
	require.Equal(t, oid, got)
	filter, err := a.Filter()
	require.NoError(t, err)
	require.NotNil(t, filter)
	require.Equal(t, oid, *filter)
}

func TestAccessTenantRejectsNilOrganization(t *testing.T) {
	_, err := domain.TenantAccess(uuid.Nil)
	require.ErrorIs(t, err, domain.ErrInvalidAccess)
}

func TestZeroAccessIsNeverPlatform(t *testing.T) {
	var a domain.Access
	require.False(t, a.Valid())
	require.False(t, a.IsPlatform())
	_, ok := a.Tenant()
	require.False(t, ok)
	_, err := a.Filter()
	require.ErrorIs(t, err, domain.ErrInvalidAccess)
}

func TestClampWindow(t *testing.T) {
	require.Equal(t, domain.Window{Offset: 0, Limit: domain.DefaultPageSize}, domain.ClampWindow(domain.Window{Offset: -5, Limit: 0}))
	require.Equal(t, domain.Window{Offset: 5, Limit: domain.MaxPageSize}, domain.ClampWindow(domain.Window{Offset: 5, Limit: 1000}))
	require.Equal(t, domain.Window{Offset: 0, Limit: 10}, domain.ClampWindow(domain.Window{Offset: 0, Limit: 10}))
	require.Equal(t, 50, domain.DefaultPageSize)
	require.Equal(t, 100, domain.MaxPageSize)
}
