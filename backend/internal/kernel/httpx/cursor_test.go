package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

const testSecret = "dev-only-session-secret-0123456789abcdef"

func newCodec(t *testing.T) *httpx.CursorCodec {
	t.Helper()
	c, err := httpx.NewCursorCodec(testSecret)
	require.NoError(t, err)
	return c
}

func samplePos() httpx.Position {
	return httpx.Position{
		CreatedAt: time.Date(2026, 10, 10, 8, 30, 15, 123456000, time.UTC),
		ID:        id.NewID(),
	}
}

func requireCursorCode(t *testing.T, err error, code string) {
	t.Helper()
	require.True(t, httpx.HasCode(err, httpx.ValidationFailed), "%v", err)
	f := fieldErrors(t, err)
	require.Equal(t, "cursor", f[0].Field)
	require.Equal(t, code, f[0].Code)
}

func TestNewCursorCodecRejectsShortSecret(t *testing.T) {
	_, err := httpx.NewCursorCodec(strings.Repeat("x", 31))
	require.Error(t, err)
	_, err = httpx.NewCursorCodec(strings.Repeat("x", 32))
	require.NoError(t, err)
}

func TestCursorRoundTrip(t *testing.T) {
	c := newCodec(t)
	fh := httpx.FilterHash(map[string]string{"action": "invoice.approved"})
	for _, dir := range []httpx.Direction{httpx.Desc, httpx.Asc} {
		pos := samplePos()
		got, err := c.Decode(c.Encode(pos, dir, fh), dir, fh)
		require.NoError(t, err)
		require.True(t, pos.CreatedAt.Equal(got.CreatedAt), "%v != %v", pos.CreatedAt, got.CreatedAt)
		require.Equal(t, pos.ID, got.ID)
	}
	// Tanpa filter aktif juga sah.
	pos := samplePos()
	_, err := c.Decode(c.Encode(pos, httpx.Desc, ""), httpx.Desc, "")
	require.NoError(t, err)
}

func TestCursorTokenIsURLSafe(t *testing.T) {
	c := newCodec(t)
	tok := c.Encode(samplePos(), httpx.Desc, httpx.FilterHash(map[string]string{"a": "b"}))
	require.NotContains(t, tok, "+")
	require.NotContains(t, tok, "/")
	require.NotContains(t, tok, "=")
	require.LessOrEqual(t, len(tok), 512)
}

// Mutasi satu karakter di posisi mana pun harus menghasilkan error: tidak ada token "tetangga" yang sah.
func TestCursorEverySingleCharMutationIsRejected(t *testing.T) {
	c := newCodec(t)
	fh := httpx.FilterHash(map[string]string{"q": "x"})
	tok := c.Encode(samplePos(), httpx.Desc, fh)
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

	for i := 0; i < len(tok); i++ {
		for _, r := range alphabet {
			if byte(r) == tok[i] {
				continue
			}
			mutated := tok[:i] + string(r) + tok[i+1:]
			_, err := c.Decode(mutated, httpx.Desc, fh)
			require.Error(t, err, "mutasi posisi %d → %q lolos", i, string(r))
			require.True(t, httpx.HasCode(err, httpx.ValidationFailed))
		}
	}
}

func TestCursorTruncatedAndGarbageRejected(t *testing.T) {
	c := newCodec(t)
	tok := c.Encode(samplePos(), httpx.Desc, "")
	for i := 0; i < len(tok); i++ {
		_, err := c.Decode(tok[:i], httpx.Desc, "")
		require.Error(t, err, "potongan panjang %d lolos", i)
		requireCursorCode(t, err, "invalid_cursor")
	}
	for _, bad := range []string{
		"", ".", "..", "a.b.c", "!!!.???", "eyJ2IjoxfQ.", ".AAAA",
		strings.Repeat("A", 600), tok + "x", tok + ".", "x" + tok,
	} {
		_, err := c.Decode(bad, httpx.Desc, "")
		requireCursorCode(t, err, "invalid_cursor")
	}
}

func TestCursorFromAnotherSecretRejected(t *testing.T) {
	a := newCodec(t)
	b, err := httpx.NewCursorCodec(strings.Repeat("z", 40))
	require.NoError(t, err)
	_, err = b.Decode(a.Encode(samplePos(), httpx.Desc, ""), httpx.Desc, "")
	requireCursorCode(t, err, "invalid_cursor")
}

func TestCursorReusedWithDifferentFilterOrDirectionIsMismatch(t *testing.T) {
	c := newCodec(t)
	f1 := httpx.FilterHash(map[string]string{"action": "a"})
	f2 := httpx.FilterHash(map[string]string{"action": "b"})
	tok := c.Encode(samplePos(), httpx.Desc, f1)

	_, err := c.Decode(tok, httpx.Desc, f2)
	requireCursorCode(t, err, "cursor_mismatch")
	_, err = c.Decode(tok, httpx.Desc, "")
	requireCursorCode(t, err, "cursor_mismatch")
	_, err = c.Decode(tok, httpx.Asc, f1)
	requireCursorCode(t, err, "cursor_mismatch")
}

func TestFilterHash(t *testing.T) {
	a := httpx.FilterHash(map[string]string{"x": "1", "y": "2"})
	b := httpx.FilterHash(map[string]string{"y": "2", "x": "1"})
	require.Equal(t, a, b, "urutan kunci tidak boleh berpengaruh")
	require.NotEqual(t, a, httpx.FilterHash(map[string]string{"x": "1", "y": "3"}))
	require.NotEqual(t, a, httpx.FilterHash(map[string]string{"x": "1"}))
	require.NotEqual(t,
		httpx.FilterHash(map[string]string{"a": "bc"}),
		httpx.FilterHash(map[string]string{"ab": "c"}), "batas kunci/nilai harus tak ambigu")
	require.Equal(t, httpx.FilterHash(nil), httpx.FilterHash(map[string]string{}))
}

func pageReq(rawQuery string) *http.Request {
	return httptest.NewRequest(http.MethodGet, "/x?"+rawQuery, nil)
}

func TestParsePage(t *testing.T) {
	p, err := httpx.ParsePage(pageReq(""), httpx.PageOptions{})
	require.NoError(t, err)
	require.Equal(t, 50, p.Limit)
	require.Equal(t, 51, p.FetchLimit())
	require.Empty(t, p.Cursor)

	p, err = httpx.ParsePage(pageReq("limit=20&cursor=abc.def"), httpx.PageOptions{})
	require.NoError(t, err)
	require.Equal(t, 20, p.Limit)
	require.Equal(t, "abc.def", p.Cursor)

	// Di atas maksimum dipotong, bukan ditolak.
	p, err = httpx.ParsePage(pageReq("limit=100000"), httpx.PageOptions{})
	require.NoError(t, err)
	require.Equal(t, 100, p.Limit)

	// Opsi per endpoint; default tidak boleh melebihi maksimum.
	p, err = httpx.ParsePage(pageReq(""), httpx.PageOptions{DefaultLimit: 500, MaxLimit: 30})
	require.NoError(t, err)
	require.Equal(t, 30, p.Limit)
}

func TestParsePageRejectsBadLimit(t *testing.T) {
	for _, q := range []string{"limit=abc", "limit=0", "limit=-5", "limit=1.5", "limit=%20"} {
		_, err := httpx.ParsePage(pageReq(q), httpx.PageOptions{})
		require.True(t, httpx.HasCode(err, httpx.ValidationFailed), q)
		f := fieldErrors(t, err)
		require.Equal(t, "limit", f[0].Field, q)
	}
}

type row struct {
	ID uuid.UUID
	At time.Time
}

func rowPos(r row) httpx.Position { return httpx.Position{CreatedAt: r.At, ID: r.ID} }

func makeRows(n int) []row {
	base := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)
	rows := make([]row, n)
	for i := range rows {
		rows[i] = row{ID: id.NewID(), At: base.Add(-time.Duration(i) * time.Second)}
	}
	return rows
}

func paginationMeta(t *testing.T, m httpx.Meta) (next any, hasMore bool) {
	t.Helper()
	p, ok := m["pagination"].(map[string]any)
	require.True(t, ok)
	hm, ok := p["has_more"].(bool)
	require.True(t, ok)
	return p["next_cursor"], hm
}

func TestPaginateHasMore(t *testing.T) {
	c := newCodec(t)
	rows := makeRows(4) // limit 3 → 4 baris diambil
	fh := httpx.FilterHash(map[string]string{"k": "v"})

	items, meta := httpx.Paginate(rows, 3, c, httpx.Desc, fh, rowPos)
	require.Len(t, items, 3)
	next, hasMore := paginationMeta(t, meta)
	require.True(t, hasMore)
	tok, ok := next.(string)
	require.True(t, ok)

	pos, err := c.Decode(tok, httpx.Desc, fh)
	require.NoError(t, err)
	require.Equal(t, rows[2].ID, pos.ID, "cursor menunjuk baris terakhir yang DITAMPILKAN, bukan baris ekstra")
	require.True(t, rows[2].At.Equal(pos.CreatedAt))
}

func TestPaginateLastPage(t *testing.T) {
	c := newCodec(t)
	for _, n := range []int{0, 1, 3} {
		items, meta := httpx.Paginate(makeRows(n), 3, c, httpx.Desc, "", rowPos)
		require.Len(t, items, n)
		next, hasMore := paginationMeta(t, meta)
		require.False(t, hasMore, "n=%d", n)
		require.Nil(t, next, "n=%d", n)
	}
}

func TestPaginateNilRowsGivesEmptyArray(t *testing.T) {
	c := newCodec(t)
	var none []row
	items, _ := httpx.Paginate(none, 3, c, httpx.Desc, "", rowPos)
	require.NotNil(t, items)
	require.Empty(t, items)
}

// Berjalan penuh melalui semua halaman dengan cursor: tidak ada baris hilang atau ganda.
func TestPaginateWalksAllRowsExactlyOnce(t *testing.T) {
	c := newCodec(t)
	all := makeRows(23)
	const limit = 5

	var seen []uuid.UUID
	var cursor *httpx.Position
	for page := 0; page < 20; page++ {
		// Simulasi query keyset: baris dengan (At, ID) di bawah cursor (urut menurun), maksimum limit+1.
		var rows []row
		for _, r := range all {
			if cursor == nil || r.At.Before(cursor.CreatedAt) {
				rows = append(rows, r)
			}
			if len(rows) == limit+1 {
				break
			}
		}
		items, meta := httpx.Paginate(rows, limit, c, httpx.Desc, "", rowPos)
		for _, it := range items {
			seen = append(seen, it.ID)
		}
		next, hasMore := paginationMeta(t, meta)
		if !hasMore {
			break
		}
		pos, err := c.Decode(next.(string), httpx.Desc, "")
		require.NoError(t, err)
		cursor = &pos
	}
	require.Len(t, seen, len(all))
	for i, r := range all {
		require.Equal(t, r.ID, seen[i])
	}
}
