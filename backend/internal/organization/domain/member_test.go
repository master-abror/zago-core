package domain_test

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/organization/domain"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

func encodeRaw(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

func TestMemberStatusesForFilter(t *testing.T) {
	open := []string{domain.MemberInvited, domain.MemberActive, domain.MemberSuspended}
	all := []string{domain.MemberInvited, domain.MemberActive, domain.MemberSuspended, domain.MemberRemoved}
	cases := []struct {
		filter string
		want   []string
	}{
		{"", open},
		{domain.FilterOpen, open},
		{domain.FilterAll, all},
		{domain.MemberInvited, []string{domain.MemberInvited}},
		{domain.MemberActive, []string{domain.MemberActive}},
		{domain.MemberSuspended, []string{domain.MemberSuspended}},
		{domain.MemberRemoved, []string{domain.MemberRemoved}},
	}
	for _, c := range cases {
		got, err := domain.MemberStatusesForFilter(c.filter)
		require.NoError(t, err, "filter %q", c.filter)
		require.Equal(t, c.want, got, "filter %q", c.filter)
	}
}

func TestMemberStatusesForFilterNeverTreatsUnknownAsAll(t *testing.T) {
	for _, filter := range []string{"ACTIVE", " active", "active ", "OPEN", "everything", "deleted", "*"} {
		got, err := domain.MemberStatusesForFilter(filter)
		require.ErrorIs(t, err, domain.ErrInvalidFilter, "filter %q", filter)
		require.Nil(t, got, "filter %q", filter)
	}
}

func TestValidMemberStatus(t *testing.T) {
	for _, s := range []string{domain.MemberInvited, domain.MemberActive, domain.MemberSuspended, domain.MemberRemoved} {
		require.True(t, domain.ValidMemberStatus(s), "status %q", s)
	}
	for _, s := range []string{"", "ACTIVE", "deleted", "archived", " active"} {
		require.False(t, domain.ValidMemberStatus(s), "status %q", s)
	}
}

func TestClampMemberLimit(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{0, domain.DefaultPageSize},
		{-3, domain.DefaultPageSize},
		{1, 1},
		{50, 50},
		{domain.MaxPageSize, domain.MaxPageSize},
		{domain.MaxPageSize + 1, domain.MaxPageSize},
		{1 << 30, domain.MaxPageSize},
	}
	for _, c := range cases {
		require.Equal(t, c.want, domain.ClampMemberLimit(c.in), "limit %d", c.in)
	}
}

func TestMemberCursorRoundTrip(t *testing.T) {
	c := domain.MemberCursor{CreatedAt: time.UnixMicro(1_700_000_000_123_456).UTC(), ID: id.NewID()}
	got, err := domain.DecodeMemberCursor(c.Encode())
	require.NoError(t, err)
	require.NotNil(t, got)
	require.True(t, got.CreatedAt.Equal(c.CreatedAt), "waktu harus kembali persis")
	require.Equal(t, c.ID, got.ID)
}

func TestMemberCursorKeepsMicrosecondPrecisionOnly(t *testing.T) {
	withNanos := time.Unix(1_700_000_000, 123_456_789).UTC()
	c := domain.MemberCursor{CreatedAt: withNanos, ID: id.NewID()}
	got, err := domain.DecodeMemberCursor(c.Encode())
	require.NoError(t, err)
	require.True(t, got.CreatedAt.Equal(withNanos.Truncate(time.Microsecond)))
}

func TestMemberCursorIsURLSafe(t *testing.T) {
	c := domain.MemberCursor{CreatedAt: time.UnixMicro(1_700_000_000_123_456).UTC(), ID: id.NewID()}
	enc := c.Encode()
	require.NotEmpty(t, enc)
	require.LessOrEqual(t, len(enc), domain.MaxCursorLength)
	require.False(t, strings.ContainsAny(enc, "+/="), "cursor %q", enc)
}

func TestDecodeMemberCursorEmptyMeansFirstPage(t *testing.T) {
	got, err := domain.DecodeMemberCursor("")
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestDecodeMemberCursorRejectsMalformedInput(t *testing.T) {
	good := "0194f1b2-7c3d-7a1e-8b2f-3c4d5e6f7a8b"
	// Kontrol: bentuk yang sah memang diterima, supaya penolakan di bawah tidak hampa.
	ok, err := domain.DecodeMemberCursor(encodeRaw("12." + good))
	require.NoError(t, err)
	require.NotNil(t, ok)

	bad := []string{
		"!!!bukan-base64!!!",
		encodeRaw("tanpa-titik"),
		encodeRaw("abc." + good),
		encodeRaw("-1." + good),
		encodeRaw("+12." + good),
		encodeRaw("012." + good),
		encodeRaw("12.bukan-uuid"),
		encodeRaw("12." + uuid.Nil.String()),
		encodeRaw("12." + strings.ToUpper(good)),
		base64.URLEncoding.EncodeToString([]byte("1." + good)),
		strings.Repeat("A", domain.MaxCursorLength+1),
	}
	for _, s := range bad {
		got, err := domain.DecodeMemberCursor(s)
		require.ErrorIs(t, err, domain.ErrInvalidCursor, "cursor %q", s)
		require.Nil(t, got, "cursor %q", s)
	}
}

func TestMemberNextCursorUsesRowPosition(t *testing.T) {
	m := domain.Member{ID: id.NewID(), CreatedAt: time.UnixMicro(1_700_000_000_000_001).UTC()}
	c := m.NextCursor()
	require.Equal(t, m.ID, c.ID)
	require.True(t, c.CreatedAt.Equal(m.CreatedAt))
}
