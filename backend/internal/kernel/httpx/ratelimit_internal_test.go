package httpx

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMemoryLimiterLimitIsHalfOfRuleMinimumOne(t *testing.T) {
	m := newMemoryLimiter(10)
	now := time.Now()

	r := m.check(Rule{Name: "r", Limit: 10, Window: time.Minute}, "a", now)
	require.Equal(t, 5, r.Limit)
	require.Equal(t, 4, r.Remaining)

	one := Rule{Name: "one", Limit: 1, Window: time.Minute}
	require.True(t, m.check(one, "a", now).Allowed)
	require.False(t, m.check(one, "a", now).Allowed, "Limit 1 → batas fallback tetap 1")
}

func TestMemoryLimiterWindowResetsWithInjectedClock(t *testing.T) {
	m := newMemoryLimiter(10)
	rule := Rule{Name: "r", Limit: 2, Window: time.Minute}
	t0 := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)

	require.True(t, m.check(rule, "a", t0).Allowed)
	denied := m.check(rule, "a", t0.Add(10*time.Second))
	require.False(t, denied.Allowed)
	require.Equal(t, 50*time.Second, denied.RetryAfter)

	require.True(t, m.check(rule, "a", t0.Add(time.Minute)).Allowed, "jendela baru")
}

func TestMemoryLimiterIsBoundedAndFailsClosedForNewIdentitiesWhenFull(t *testing.T) {
	m := newMemoryLimiter(3)
	rule := Rule{Name: "r", Limit: 10, Window: time.Minute}
	t0 := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)

	for _, id := range []string{"a", "b", "c"} {
		require.True(t, m.check(rule, id, t0).Allowed)
	}
	require.False(t, m.check(rule, "d", t0).Allowed, "penuh oleh jendela aktif: identitas baru ditolak")
	require.True(t, m.check(rule, "a", t0).Allowed, "identitas yang sudah ada tetap dilayani")
	require.LessOrEqual(t, len(m.entries), 3)

	require.True(t, m.check(rule, "d", t0.Add(2*time.Minute)).Allowed, "entri kedaluwarsa dibersihkan")
	require.LessOrEqual(t, len(m.entries), 3)
}
