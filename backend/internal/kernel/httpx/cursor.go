package httpx

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Direction adalah arah urut keyset. Urutan selalu (created_at, id) (docs/08 §4).
type Direction string

const (
	Desc Direction = "desc" // terbaru dulu: created_at DESC, id DESC; halaman berikut: (created_at, id) < cursor
	Asc  Direction = "asc"  // terlama dulu: created_at ASC, id ASC;  halaman berikut: (created_at, id) > cursor
)

func (d Direction) valid() bool { return d == Desc || d == Asc }

// Position adalah baris terakhir pada satu halaman: kunci keyset (created_at, id).
type Position struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

const (
	cursorVersion      = 1
	cursorKeyLabel     = "zago/cursor/v1"
	minCursorSecretLen = 32
	maxCursorTokenLen  = 512
)

var (
	cursorEnc = base64.RawURLEncoding
	// Strict menolak bit sisa non-nol sehingga token tidak bisa dimutasi tanpa mengubah MAC-nya.
	cursorDec = base64.RawURLEncoding.Strict()
)

// CursorCodec menandatangani dan memverifikasi cursor paginasi (docs/08 §4). Kuncinya diturunkan
// dari SESSION_SECRET dengan label tetap (ADR-0013), sehingga berbeda dari kunci turunan lain.
type CursorCodec struct {
	key []byte
}

// NewCursorCodec menurunkan kunci cursor = HMAC-SHA256(secret, "zago/cursor/v1").
func NewCursorCodec(sessionSecret string) (*CursorCodec, error) {
	if len(sessionSecret) < minCursorSecretLen {
		return nil, errors.New("httpx: rahasia cursor minimal 32 byte")
	}
	m := hmac.New(sha256.New, []byte(sessionSecret))
	m.Write([]byte(cursorKeyLabel))
	return &CursorCodec{key: m.Sum(nil)}, nil
}

type cursorPayload struct {
	V int    `json:"v"`
	T int64  `json:"t"` // created_at, UnixNano UTC
	I string `json:"i"` // id
	D string `json:"d"` // arah
	F string `json:"f"` // hash filter aktif
}

func (c *CursorCodec) mac(msg []byte) []byte {
	m := hmac.New(sha256.New, c.key)
	m.Write(msg)
	return m.Sum(nil)
}

func (c *CursorCodec) seal(p cursorPayload) string {
	raw, _ := json.Marshal(p) // struct sederhana: tidak mungkin gagal
	return cursorEnc.EncodeToString(raw) + "." + cursorEnc.EncodeToString(c.mac(raw))
}

// Encode membuat cursor opak untuk halaman berikut setelah posisi pos, dengan arah dan hash filter
// yang sedang aktif.
func (c *CursorCodec) Encode(pos Position, dir Direction, filterHash string) string {
	return c.seal(cursorPayload{
		V: cursorVersion,
		T: pos.CreatedAt.UTC().UnixNano(),
		I: pos.ID.String(),
		D: string(dir),
		F: filterHash,
	})
}

func invalidCursor() *Error {
	return Validation(FieldError{Field: "cursor", Code: "invalid_cursor", Message: "cursor is invalid"})
}

// Decode memverifikasi token. Token rusak/dipalsukan/terpotong → validation_failed (invalid_cursor);
// token sah tetapi dipakai dengan arah atau filter berbeda → validation_failed (cursor_mismatch).
// Tidak pernah mengembalikan halaman yang diam-diam salah (docs/08 §4).
func (c *CursorCodec) Decode(token string, dir Direction, filterHash string) (Position, error) {
	if token == "" || len(token) > maxCursorTokenLen || strings.Count(token, ".") != 1 {
		return Position{}, invalidCursor()
	}
	left, right, _ := strings.Cut(token, ".")
	raw, err := cursorDec.DecodeString(left)
	if err != nil {
		return Position{}, invalidCursor()
	}
	sig, err := cursorDec.DecodeString(right)
	if err != nil || !hmac.Equal(sig, c.mac(raw)) {
		return Position{}, invalidCursor()
	}

	var p cursorPayload
	if err := json.Unmarshal(raw, &p); err != nil || p.V != cursorVersion || !Direction(p.D).valid() {
		return Position{}, invalidCursor()
	}
	id, err := uuid.Parse(p.I)
	if err != nil {
		return Position{}, invalidCursor()
	}
	if p.D != string(dir) || p.F != filterHash {
		return Position{}, Validation(FieldError{
			Field: "cursor", Code: "cursor_mismatch",
			Message: "cursor was issued for a different sort order or filter set",
		})
	}
	return Position{CreatedAt: time.Unix(0, p.T).UTC(), ID: id}, nil
}

// FilterHash membuat sidik jari filter aktif untuk disematkan di cursor. Hanya sertakan filter
// yang benar-benar aktif, dengan nilai ternormalisasi (mis. waktu sudah UTC RFC3339). Urutan
// kunci tidak berpengaruh; panjang diberi prefiks sehingga {"a":"bc"} ≠ {"ab":"c"}.
func FilterHash(filters map[string]string) string {
	keys := make([]string, 0, len(filters))
	for k := range filters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	var n [4]byte
	write := func(s string) {
		binary.BigEndian.PutUint32(n[:], uint32(len(s)))
		h.Write(n[:])
		h.Write([]byte(s))
	}
	for _, k := range keys {
		write(k)
		write(filters[k])
	}
	return cursorEnc.EncodeToString(h.Sum(nil)[:16])
}

// PageOptions mengatur batas limit satu endpoint. Nol berarti nilai bawaan.
type PageOptions struct {
	DefaultLimit int
	MaxLimit     int
}

// Batas bawaan: limit bawaan 50 (contoh docs/08 §4), maksimum server 100.
const (
	defaultPageLimit = 50
	defaultPageMax   = 100
)

// PageRequest adalah parameter paginasi cursor dari query string.
type PageRequest struct {
	Limit  int
	Cursor string // token mentah; kosong = halaman pertama
}

// FetchLimit adalah jumlah baris yang diminta ke database: Limit+1, supaya has_more diketahui
// tanpa query count.
func (p PageRequest) FetchLimit() int { return p.Limit + 1 }

// ParsePage membaca ?limit= dan ?cursor=. limit tak valid (bukan bilangan bulat ≥ 1) →
// validation_failed; limit di atas maksimum dipotong ke maksimum (ADR-0013).
func ParsePage(r *http.Request, o PageOptions) (PageRequest, error) {
	if o.DefaultLimit < 1 {
		o.DefaultLimit = defaultPageLimit
	}
	if o.MaxLimit < 1 {
		o.MaxLimit = defaultPageMax
	}
	if o.DefaultLimit > o.MaxLimit {
		o.DefaultLimit = o.MaxLimit
	}

	q := r.URL.Query()
	limit := o.DefaultLimit
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return PageRequest{}, Validation(FieldError{Field: "limit", Code: "invalid_limit", Message: "limit must be an integer ≥ 1"})
		}
		limit = min(n, o.MaxLimit)
	}
	return PageRequest{Limit: limit, Cursor: q.Get("cursor")}, nil
}

// Paginate memotong rows (hasil query dengan FetchLimit) menjadi satu halaman dan membangun meta
// pagination {"next_cursor","has_more"}. next_cursor null bila halaman terakhir. Slice hasil tidak
// pernah nil.
func Paginate[T any](rows []T, limit int, codec *CursorCodec, dir Direction, filterHash string, pos func(T) Position) ([]T, Meta) {
	if limit < 1 {
		limit = 1
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	rows = Items(rows)

	var next any
	if hasMore {
		next = codec.Encode(pos(rows[len(rows)-1]), dir, filterHash)
	}
	return rows, Meta{"pagination": map[string]any{"next_cursor": next, "has_more": hasMore}}
}
