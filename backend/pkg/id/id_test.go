package id_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/pkg/id"
)

func TestNewIDIsVersion7(t *testing.T) {
	u := id.NewID()
	require.Equal(t, uuid.Version(7), u.Version())
	require.Equal(t, uuid.RFC4122, u.Variant())
}

func TestNewIDIsStrictlyTimeOrdered(t *testing.T) {
	const n = 10_000
	prev := id.NewID().String()
	for i := 0; i < n; i++ {
		cur := id.NewID().String()
		require.Less(t, prev, cur, "ID harus menaik secara leksikografis (urut waktu), iterasi %d", i)
		prev = cur
	}
}

func TestNewIDEmbedsCurrentTimestamp(t *testing.T) {
	before := time.Now().Add(-2 * time.Second)
	u := id.NewID()
	after := time.Now().Add(2 * time.Second)

	// 48 bit pertama UUIDv7 = milidetik sejak epoch Unix.
	var ms int64
	for _, b := range u[:6] {
		ms = ms<<8 | int64(b)
	}
	ts := time.UnixMilli(ms)
	require.False(t, ts.Before(before), "timestamp %v lebih awal dari %v", ts, before)
	require.False(t, ts.After(after), "timestamp %v lebih lambat dari %v", ts, after)
}

func TestNewIDIsUnique(t *testing.T) {
	seen := make(map[uuid.UUID]struct{}, 5000)
	for i := 0; i < 5000; i++ {
		u := id.NewID()
		_, dup := seen[u]
		require.False(t, dup)
		seen[u] = struct{}{}
	}
}

func TestParseRejectsGarbageWithoutPanic(t *testing.T) {
	for _, in := range []string{"", "not-a-uuid", "123", "zzzzzzzz-zzzz-zzzz-zzzz-zzzzzzzzzzzz", "' OR 1=1 --"} {
		require.NotPanics(t, func() {
			_, err := id.Parse(in)
			require.Error(t, err, "input %q harus ditolak", in)
		})
	}
}

func TestParseRoundTrip(t *testing.T) {
	want := id.NewID()
	got, err := id.Parse(want.String())
	require.NoError(t, err)
	require.Equal(t, want, got)
}
