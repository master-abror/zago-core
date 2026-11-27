package identityhttp

import (
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/identity/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
)

// DTO endpoint profil dan user (docs/08 §10.8-§10.9; ADR-0023 butir 22-24). Body didekode lewat
// httpx.DecodeJSON yang menolak field tak dikenal: UpdateRequest sengaja TIDAK punya email,
// username, status, atau id, sehingga body yang membawanya ditolak sebagai unknown_field.
// Validasi isi (panjang, pola, zona waktu, skema URL) ada di domain; DTO hanya memeriksa bentuk
// yang tidak dapat dibedakan di sana.

// UpdateRequest adalah body PATCH /profile dan PATCH /users/{id}: field yang tidak dikirim (atau
// null) tidak diubah. Untuk first_name, last_name, phone, dan avatar_url string kosong menghapus
// nilainya. Urutan field berbeda dari domain.UpdateInput dengan sengaja, supaya konversi struct
// tidak tampak mungkin.
type UpdateRequest struct {
	// Locale adalah locale baru; tidak boleh kosong.
	Locale *string `json:"locale"`

	// Timezone adalah zona waktu IANA baru; tidak boleh kosong.
	Timezone *string `json:"timezone"`

	// DisplayName adalah nama tampilan baru; tidak boleh kosong.
	DisplayName *string `json:"display_name"`

	// FirstName adalah nama depan baru; string kosong menghapusnya.
	FirstName *string `json:"first_name"`

	// LastName adalah nama belakang baru; string kosong menghapusnya.
	LastName *string `json:"last_name"`

	// Phone adalah nomor telepon baru; string kosong menghapusnya. Field terbatas: mengirim
	// kunci ini membutuhkan hak tulis field (profile.field.phone.write atau
	// user.field.phone.write), diperiksa handler sebelum body didekode.
	Phone *string `json:"phone"`

	// AvatarURL adalah URL https gambar profil baru (tanpa unggah); string kosong menghapusnya.
	AvatarURL *string `json:"avatar_url"`
}

// Validate menolak perubahan kosong.
func (q UpdateRequest) Validate() error {
	if q.DisplayName == nil && q.FirstName == nil && q.LastName == nil && q.Phone == nil &&
		q.AvatarURL == nil && q.Locale == nil && q.Timezone == nil {
		return httpx.Validation(httpx.FieldError{Code: "empty_patch", Message: "the request changes nothing"})
	}
	return nil
}

// Input mengubah body menjadi masukan layanan.
func (q UpdateRequest) Input() domain.UpdateInput {
	in := domain.UpdateInput{}
	in.DisplayName = q.DisplayName
	in.FirstName = q.FirstName
	in.LastName = q.LastName
	in.Phone = q.Phone
	in.AvatarURL = q.AvatarURL
	in.Locale = q.Locale
	in.Timezone = q.Timezone
	return in
}

// UserResponse mendokumentasikan bentuk satu user pada respons (swagger). Respons NYATA dibangun
// sebagai peta oleh UserFields supaya field terbatas dapat dihapus oleh FieldHelper (kunci
// absen, bukan null); tes menjaga agar kunci peta dan tag struct ini tidak menyimpang.
type UserResponse struct {
	// ID adalah id user.
	ID uuid.UUID `json:"id"`

	// Username adalah pengenal opsional; null bila tidak diisi.
	Username *string `json:"username"`

	// DisplayName adalah nama tampilan.
	DisplayName string `json:"display_name"`

	// FirstName adalah nama depan; null bila tidak diisi.
	FirstName *string `json:"first_name"`

	// LastName adalah nama belakang; null bila tidak diisi.
	LastName *string `json:"last_name"`

	// Email adalah alamat email.
	Email string `json:"email"`

	// Phone adalah nomor telepon; null bila tidak diisi, ABSEN bila pemanggil tidak memegang hak
	// baca field (profile.field.phone.read atau user.field.phone.read).
	Phone *string `json:"phone,omitempty"`

	// AvatarURL adalah URL gambar profil; null bila tidak diisi.
	AvatarURL *string `json:"avatar_url"`

	// Locale adalah locale pilihan user.
	Locale string `json:"locale"`

	// Timezone adalah zona waktu IANA pilihan user.
	Timezone string `json:"timezone"`

	// Status adalah pending, active, suspended, locked, deactivated, atau deleted.
	Status string `json:"status"`

	// LastLoginAt adalah waktu login terakhir (UTC, global lintas organisasi); ada pada
	// /profile dan pada /users hanya untuk scope platform, null bila belum pernah login.
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`

	// CreatedAt adalah waktu pembuatan (UTC).
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt adalah waktu perubahan terakhir (UTC).
	UpdatedAt time.Time `json:"updated_at"`
}

// nullableString mengubah *string menjadi nilai JSON: nil menjadi null.
func nullableString(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

// UserFields membentuk objek respons user sebagai peta berkunci snake_case (ADR-0023 butir 22).
// last_login_at hanya dimuat bila includeLastLogin (profil milik sendiri, atau scope platform pada
// /users): nilainya global lintas organisasi, sehingga pemanggil tenant tidak boleh melihat
// aktivitas login user di organisasi lain; kunci absen untuk mereka, null bila belum pernah login.
// Peta ini adalah masukan FieldHelper.FilterRead, yang membuang phone bila hak baca tidak ada.
func UserFields(r domain.Record, includeLastLogin bool) map[string]any {
	m := map[string]any{}
	m["id"] = r.ID
	m["username"] = nullableString(r.Username)
	m["display_name"] = r.DisplayName
	m["first_name"] = nullableString(r.FirstName)
	m["last_name"] = nullableString(r.LastName)
	m["email"] = r.Email
	m["phone"] = nullableString(r.Phone)
	m["avatar_url"] = nullableString(r.AvatarURL)
	m["locale"] = r.Locale
	m["timezone"] = r.Timezone
	m["status"] = r.Status
	if includeLastLogin {
		var at any
		if r.LastLoginAt != nil {
			at = r.LastLoginAt.UTC()
		}
		m["last_login_at"] = at
	}
	m["created_at"] = r.CreatedAt.UTC()
	m["updated_at"] = r.UpdatedAt.UTC()
	return m
}

// maxPageNumber membatasi page supaya offset (page-1)*per_page tidak dapat meluap.
const maxPageNumber = 10000

// ListParams adalah hasil penguraian query daftar user.
type ListParams struct {
	// Query adalah masukan layanan.
	Query domain.ListQuery

	// Page adalah nomor halaman (mulai 1), untuk meta.pagination.
	Page int

	// PerPage adalah ukuran halaman yang berlaku, untuk meta.pagination.
	PerPage int
}

// ParseList membaca status, page, dan per_page dari query (daftar offset: docs/08 §4). Nilai yang
// hilang memakai bawaan (page 1, per_page domain.DefaultPageSize); per_page di atas batas
// dipangkas ke domain.MaxPageSize; nilai bukan angka, nol, atau negatif menjadi validation_failed
// dengan satu entri per field yang salah. Nilai status diteruskan apa adanya: domain menolak yang
// tak dikenal (ErrInvalidFilter) sebelum menyentuh database.
func ParseList(q url.Values) (ListParams, error) {
	var bad httpx.Fields
	page, perPage := 1, domain.DefaultPageSize

	if v := q.Get("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxPageNumber {
			bad.Add("page", "invalid", "page must be an integer between 1 and 10000")
		} else {
			page = n
		}
	}
	if v := q.Get("per_page"); v != "" {
		n, err := strconv.Atoi(v)
		switch {
		case err != nil || n < 1:
			bad.Add("per_page", "invalid", "per_page must be a positive integer")
		case n > domain.MaxPageSize:
			perPage = domain.MaxPageSize
		default:
			perPage = n
		}
	}
	if bad.Len() > 0 {
		return ListParams{}, bad.Err()
	}

	out := ListParams{Page: page, PerPage: perPage}
	out.Query.Filter = q.Get("status")
	out.Query.Window = domain.Window{Offset: (page - 1) * perPage, Limit: perPage}
	return out, nil
}

// PaginationMeta membentuk meta.pagination untuk daftar offset.
func PaginationMeta(page, perPage, total int) httpx.Meta {
	return httpx.Meta{"pagination": map[string]int{"page": page, "per_page": perPage, "total": total}}
}
