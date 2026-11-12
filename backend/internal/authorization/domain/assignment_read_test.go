package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

func TestValidAssignmentFilterAcceptsOnlyTheFourDocumentedValues(t *testing.T) {
	for _, ok := range []string{"active", "revoked", "expired", "all"} {
		require.True(t, domain.ValidAssignmentFilter(ok), ok)
	}
	for _, bad := range []string{"", "ACTIVE", "Active", " active", "archived", "pending", "all,active", "*"} {
		require.False(t, domain.ValidAssignmentFilter(bad), "%q", bad)
	}
}

func TestAssignmentStateCoversEveryCombinationOfRevocationAndExpiry(t *testing.T) {
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	cases := []struct {
		name    string
		expires *time.Time
		revoked *time.Time
		want    string
	}{
		{"tanpa masa berlaku, belum dicabut", nil, nil, domain.AssignmentFilterActive},
		{"masa berlaku di depan", &future, nil, domain.AssignmentFilterActive},
		{"masa berlaku tepat sekarang sudah kedaluwarsa", &now, nil, domain.AssignmentFilterExpired},
		{"masa berlaku lewat", &past, nil, domain.AssignmentFilterExpired},
		{"dicabut tanpa masa berlaku", nil, &past, domain.AssignmentFilterRevoked},
		{"dicabut mengalahkan kedaluwarsa", &past, &past, domain.AssignmentFilterRevoked},
		{"dicabut mengalahkan masa berlaku di depan", &future, &past, domain.AssignmentFilterRevoked},
	}
	for _, c := range cases {
		rec := domain.AssignmentRecord{ID: id.NewID(), ExpiresAt: c.expires, RevokedAt: c.revoked}

		got := rec.State(now)

		require.Equal(t, c.want, got, c.name)
		require.Equal(t, got == domain.AssignmentFilterActive, rec.Active(now), c.name+": State aktif harus sama dengan Active")
	}
}

func TestClampAssignmentLimitKeepsRoomForTheHasMoreProbeRow(t *testing.T) {
	require.Equal(t, domain.DefaultPageSize+1, domain.ClampAssignmentLimit(0), "nol memakai bawaan")
	require.Equal(t, domain.DefaultPageSize+1, domain.ClampAssignmentLimit(-5), "negatif memakai bawaan")
	require.Equal(t, 1, domain.ClampAssignmentLimit(1))
	require.Equal(t, 51, domain.ClampAssignmentLimit(51))
	require.Equal(t, domain.MaxPageSize+1, domain.ClampAssignmentLimit(domain.MaxPageSize+1), "batas atas sama dengan satu halaman penuh ditambah satu")
	require.Equal(t, domain.MaxPageSize+1, domain.ClampAssignmentLimit(10_000), "permintaan raksasa dipangkas")
}
