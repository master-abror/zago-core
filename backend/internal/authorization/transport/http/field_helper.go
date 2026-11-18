package authzhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
)

// maxLoggedFieldNames membatasi jumlah nama kunci klien pada log penolakan penulisan field.
const maxLoggedFieldNames = 10

// maxLoggedFieldRunes membatasi panjang (dalam rune) satu nama kunci klien pada log.
const maxLoggedFieldRunes = 64

// FieldFilterSource membuat FieldFilter untuk satu (identity, scope, tipe). Dipenuhi oleh
// application.FieldGuard; didefinisikan di sisi pemakai supaya helper dapat diuji tanpa database.
type FieldFilterSource interface {
	For(ctx context.Context, identity authdomain.Identity, scope domain.ScopeContext, resourceType string) (domain.FieldFilter, error)
}

// FieldHelper adalah helper handler untuk field permission (ADR-0022 butir 40, 44, dan 77).
// Ia BUKAN gerbang izin resource: izin resource tetap dijaga Authorize/Can di rute, dan helper
// dipanggil sesudahnya. Identity dan ScopeContext hanya diambil dari context request.
type FieldHelper struct {
	src FieldFilterSource

	log *slog.Logger
}

// NewFieldHelper membuat FieldHelper; semua dependensi wajib ada. src HARUS interface nil, bukan
// pointer nil yang dibungkus interface.
func NewFieldHelper(src FieldFilterSource, log *slog.Logger) (*FieldHelper, error) {
	if src == nil || log == nil {
		return nil, errors.New("authzhttp: FieldHelper membutuhkan FieldFilterSource dan logger")
	}
	return &FieldHelper{src: src, log: log}, nil
}

// caller mengambil Identity dan ScopeContext dari context request: tanpa Identity 401, tanpa
// Scope 500 (middleware Scope tidak terpasang), seperti handler lain di paket ini.
func (h *FieldHelper) caller(r *http.Request) (authdomain.Identity, domain.ScopeContext, error) {
	ident, ok := authdomain.IdentityFromContext(r.Context())
	if !ok {
		return authdomain.Identity{}, domain.ScopeContext{}, httpx.AuthenticationRequired.New()
	}
	scope, ok := domain.ScopeFromContext(r.Context())
	if !ok {
		return authdomain.Identity{}, domain.ScopeContext{}, httpx.InternalError.New()
	}
	return ident, scope, nil
}

// DecodeWrite membaca body JSON request tulis ke dst setelah memastikan pemanggil boleh menulis
// setiap field terbatas yang terkirim. Urutan (setiap galat adalah *httpx.Error yang siap
// diserahkan ke Responder.Error, dan tidak ada galat yang menjadi izin):
//
//  1. Identity dan Scope dari context (401 atau 500).
//  2. Body dibaca SEKALI lewat httpx.DecodeJSON (Content-Type, batas ukuran, tepat satu nilai).
//  3. Body harus objek JSON; kunci MENTAH tingkat atas diambil dari objek itu, sebelum didekode
//     ke struct (dekoder melipat huruf dan dapat memetakan kunci berbeda ke field yang sama).
//  4. FieldFilter untuk ref.Type dibuat lewat sumbernya dan CheckWrite diterapkan pada kunci
//     mentah: field terbatas tanpa hak tulis memberi 403 field_permission_denied tanpa nama
//     field pada respons (nama hanya masuk log, disanitasi dan dipotong).
//  5. Baru sesudah itu dst didekode dari byte yang sama lewat httpx.DecodeJSON (field tak
//     dikenal ditolak), sehingga penolakan field terbatas selalu mendahului galat decode.
//
// ref menyatakan sumber daya yang ditulis: pada pembuatan, tipe dan organisasi scope; pada
// perubahan, ref dari sumber daya tersimpan (pemilik dan grup tidak pernah dari body). maxBytes <= 0
// memakai batas bawaan httpx.
func (h *FieldHelper) DecodeWrite(w http.ResponseWriter, r *http.Request, ref domain.ResourceRef, dst any, maxBytes int64) error {
	ident, scope, err := h.caller(r)
	if err != nil {
		return err
	}

	var raw json.RawMessage
	if err := httpx.DecodeJSON(w, r, &raw, maxBytes); err != nil {
		return err
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil || members == nil {
		return httpx.Validation(httpx.FieldError{Code: "invalid_type", Message: "request body must be a JSON object"})
	}
	sent := make(map[string]any, len(members))
	for key := range members {
		sent[key] = nil
	}

	filter, err := h.src.For(r.Context(), ident, scope, ref.Type)
	if err != nil {
		return FieldError(err)
	}
	if err := filter.CheckWrite(ref, sent); err != nil {
		h.logDenied(r.Context(), ident, ref, err)
		return FieldError(err)
	}

	r.Body = io.NopCloser(bytes.NewReader(raw))
	return httpx.DecodeJSON(w, r, dst, maxBytes)
}

// FilterRead mengembalikan salinan BARU obj tanpa field terbatas yang tidak boleh dibaca
// pemanggil (kunci absen, bukan null). Galatnya *httpx.Error (lihat FieldError). Untuk banyak
// objek satu tipe dalam satu request, minta FieldFilter sekali dari sumbernya dan panggil Apply
// per objek.
func (h *FieldHelper) FilterRead(r *http.Request, ref domain.ResourceRef, obj map[string]any) (map[string]any, error) {
	ident, scope, err := h.caller(r)
	if err != nil {
		return nil, err
	}
	filter, err := h.src.For(r.Context(), ident, scope, ref.Type)
	if err != nil {
		return nil, FieldError(err)
	}
	out, err := filter.Apply(ref, obj)
	if err != nil {
		return nil, FieldError(err)
	}
	return out, nil
}

// logDenied mencatat penolakan penulisan field: nama kunci klien dipotong dan disanitasi, dan
// TIDAK pernah masuk respons (ADR-0022 butir 44).
func (h *FieldHelper) logDenied(ctx context.Context, ident authdomain.Identity, ref domain.ResourceRef, err error) {
	var denied *domain.FieldDeniedError
	if !errors.As(err, &denied) {
		return
	}
	h.log.WarnContext(ctx, "authorization: penulisan field terbatas ditolak",
		slog.String("resource_type", ref.Type),
		slog.String("user_id", ident.UserID.String()),
		slog.Int("denied_total", denied.Total),
		slog.Any("denied_fields", sanitizeFieldNames(denied.Fields)),
	)
}

// sanitizeFieldNames memotong daftar nama dan membersihkan tiap nama untuk log.
func sanitizeFieldNames(names []string) []string {
	if len(names) > maxLoggedFieldNames {
		names = names[:maxLoggedFieldNames]
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, sanitizeFieldName(name))
	}
	return out
}

// sanitizeFieldName memotong satu nama ke maxLoggedFieldRunes rune (dengan penanda "...") dan
// mengganti rune yang tidak dapat dicetak (kontrol, urutan UTF-8 rusak) dengan '?'.
func sanitizeFieldName(name string) string {
	var b strings.Builder
	n := 0
	for _, r := range name {
		if n == maxLoggedFieldRunes {
			b.WriteString("...")
			break
		}
		if r == utf8.RuneError || !unicode.IsPrint(r) {
			r = '?'
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}
