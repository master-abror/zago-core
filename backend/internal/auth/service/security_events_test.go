package service_test

import (
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/auth/service"
	"github.com/master-abror/zago-core/backend/internal/kernel"
)

// Riwayat event keamanan milik sendiri (docs/05 §14, ADR-0020 butir 46) terhadap PostgreSQL sungguhan.

func eventIDs(list []service.SecurityEventInfo) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(list))
	for _, e := range list {
		out = append(out, e.ID)
	}
	return out
}

func TestListSecurityEventsReturnsOnlyTheCallersEventsNewestFirst(t *testing.T) {
	a := newAuthHarness(t)
	u := a.newUserWithPassword(goodPassword)
	other := a.newUserWithPassword(goodPassword)
	a.ctx = kernel.WithClient(a.ctx, kernel.Client{IP: "203.0.113.9", UserAgent: "agen-uji/1.0"})
	_, ident := a.loginIdentity(u)
	for i := 0; i < 4; i++ {
		_, err := a.login(u.Email, "salah-"+strconv.Itoa(i))
		require.Error(t, err)
	}
	_, otherIdent := a.loginIdentity(other)
	_, err := a.login(other.Email, "salah-orang-lain")
	require.Error(t, err)

	mine, err := a.auth.ListSecurityEvents(a.ctx, ident, 100, nil)
	require.NoError(t, err)
	theirs, err := a.auth.ListSecurityEvents(a.ctx, otherIdent, 100, nil)
	require.NoError(t, err)

	require.Len(t, mine, 5, "1 login sukses + 4 gagal milik pemanggil")
	require.Len(t, theirs, 2)
	seen := map[uuid.UUID]bool{}
	for _, id := range eventIDs(mine) {
		seen[id] = true
	}
	for _, id := range eventIDs(theirs) {
		require.False(t, seen[id], "event pengguna lain tidak boleh muncul di riwayat saya")
	}
	require.Equal(t, service.EventLoginFailed, mine[0].Type, "terbaru dulu")
	require.Equal(t, service.EventLoginSuccess, mine[len(mine)-1].Type)
	for i := 1; i < len(mine); i++ {
		require.False(t, mine[i].CreatedAt.After(mine[i-1].CreatedAt), "urutan menurun menurut waktu")
	}
	require.Equal(t, "203.0.113.9", mine[0].IPAddress)
	require.NotNil(t, mine[0].UserAgent)
	require.Equal(t, "agen-uji/1.0", *mine[0].UserAgent)
	require.Equal(t, "failure", mine[0].Result)
}

func TestListSecurityEventsPagesByKeysetWithoutGapsOrDuplicates(t *testing.T) {
	a := newAuthHarness(t)
	u := a.newUserWithPassword(goodPassword)
	_, ident := a.loginIdentity(u)
	for i := 0; i < 6; i++ {
		_, err := a.login(u.Email, "salah-"+strconv.Itoa(i))
		require.Error(t, err)
	}
	all, err := a.auth.ListSecurityEvents(a.ctx, ident, 100, nil)
	require.NoError(t, err)
	require.Len(t, all, 7)

	var got []uuid.UUID
	var pos *service.EventPosition
	for pages := 0; ; pages++ {
		require.Less(t, pages, 10, "paginasi harus berhenti")
		page, err := a.auth.ListSecurityEvents(a.ctx, ident, 3, pos)
		require.NoError(t, err)
		if len(page) == 0 {
			break
		}
		require.LessOrEqual(t, len(page), 3)
		got = append(got, eventIDs(page)...)
		last := page[len(page)-1]
		pos = &service.EventPosition{CreatedAt: last.CreatedAt, ID: last.ID}
	}

	require.Equal(t, eventIDs(all), got)
}

func TestListSecurityEventsClampsTheLimit(t *testing.T) {
	a := newAuthHarness(t)
	u := a.newUserWithPassword(goodPassword)
	_, ident := a.loginIdentity(u)
	_, err := a.login(u.Email, "salah-sekali")
	require.Error(t, err)

	one, err := a.auth.ListSecurityEvents(a.ctx, ident, 0, nil)

	require.NoError(t, err)
	require.Len(t, one, 1, "limit < 1 dianggap 1")
}
