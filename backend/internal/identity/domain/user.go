// Package domain memuat aturan murni identitas pengguna (docs/02 §7, docs/08 §10.8-§10.9):
// bentuk baris user, normalisasi dan validasi masukan ubah, status dan filter daftar, serta
// penentuan bidang yang benar-benar berubah. Paket ini tidak melakukan I/O.
//
// Siapa yang memanggil (platform atau tenant) dinyatakan oleh organization/domain.Access, satu
// definisi untuk seluruh inti (ADR-0023 butir 26a); arah impor hanya identity ke
// organization/domain, tidak pernah sebaliknya.
package domain

import (
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Status user (migrasi 004). Hanya status yang dapat dipilih sebagai filter daftar.
const (
	// StatusPending adalah user yang belum menyelesaikan aktivasi.
	StatusPending = "pending"

	// StatusActive adalah user yang beroperasi normal.
	StatusActive = "active"

	// StatusSuspended adalah user yang ditangguhkan.
	StatusSuspended = "suspended"

	// StatusLocked adalah user yang terkunci karena kegagalan login berulang.
	StatusLocked = "locked"

	// StatusDeactivated adalah user yang dinonaktifkan (DELETE /users/{id}, T5).
	StatusDeactivated = "deactivated"

	// StatusDeleted adalah user yang dihapus.
	StatusDeleted = "deleted"
)

// Batas masukan dan nilai bawaan. Batas panjang mengikuti kolom di migrasi 004.
const (
	// MaxDisplayNameLength adalah panjang display_name maksimum dalam rune.
	MaxDisplayNameLength = 200

	// MaxPersonNameLength adalah panjang first_name dan last_name maksimum dalam rune.
	MaxPersonNameLength = 100

	// MaxPhoneLength adalah panjang phone maksimum dalam byte.
	MaxPhoneLength = 50

	// MinPhoneDigits dan MaxPhoneDigits membatasi jumlah angka pada phone (E.164: paling
	// banyak 15 angka).
	MinPhoneDigits = 5

	// MaxPhoneDigits adalah jumlah angka maksimum pada phone.
	MaxPhoneDigits = 15

	// MaxAvatarURLLength adalah panjang avatar_url maksimum dalam byte.
	MaxAvatarURLLength = 2048

	// MaxLocaleLength adalah panjang locale maksimum.
	MaxLocaleLength = 20

	// MaxTimezoneLength adalah panjang timezone maksimum.
	MaxTimezoneLength = 100

	// DefaultPageSize adalah ukuran halaman bila pemanggil tidak menyebutnya.
	DefaultPageSize = 50

	// MaxPageSize adalah ukuran halaman maksimum.
	MaxPageSize = 100
)

// Nama bidang yang dapat diubah. Dipakai audit (hanya nama, tidak pernah nilai) dan field
// permission (ADR-0023 butir 24).
const (
	// FieldDisplayName adalah kolom display_name.
	FieldDisplayName = "display_name"

	// FieldFirstName adalah kolom first_name.
	FieldFirstName = "first_name"

	// FieldLastName adalah kolom last_name.
	FieldLastName = "last_name"

	// FieldPhone adalah kolom phone; satu-satunya bidang yang dikendalikan field permission.
	FieldPhone = "phone"

	// FieldAvatarURL adalah kolom avatar_url.
	FieldAvatarURL = "avatar_url"

	// FieldLocale adalah kolom locale.
	FieldLocale = "locale"

	// FieldTimezone adalah kolom timezone.
	FieldTimezone = "timezone"
)

// Galat domain identitas. Lapisan transport memetakan masing-masing ke kode API; sentinel baru
// yang tidak didaftarkan di sana jatuh ke 503. Galat Access (organization/domain.ErrInvalidAccess
// dan ErrPlatformRequired) berasal dari paket organisasi.
var (
	// ErrUserNotFound menandai user yang tidak ada, soft-deleted, ATAU tidak terlihat bagi
	// pemanggil (bukan anggota organisasinya). Ketiganya sengaja tidak dapat dibedakan.
	ErrUserNotFound = errors.New("identity: user tidak ditemukan")

	// ErrNothingToUpdate menandai pembaruan tanpa satu pun bidang.
	ErrNothingToUpdate = errors.New("identity: tidak ada bidang yang diubah")

	// ErrInvalidID menandai id user kosong.
	ErrInvalidID = errors.New("identity: id tidak sah")

	// ErrNoTransaction menandai operasi yang membutuhkan transaksi tetapi dipanggil tanpa
	// transaksi pada context.
	ErrNoTransaction = errors.New("identity: operasi harus berjalan di dalam transaksi")

	// ErrInvalidDisplayName menandai display_name yang kosong, terlalu panjang, atau memuat
	// karakter kontrol.
	ErrInvalidDisplayName = errors.New("identity: display_name tidak sah")

	// ErrInvalidFirstName menandai first_name yang terlalu panjang atau memuat karakter kontrol.
	ErrInvalidFirstName = errors.New("identity: first_name tidak sah")

	// ErrInvalidLastName menandai last_name yang terlalu panjang atau memuat karakter kontrol.
	ErrInvalidLastName = errors.New("identity: last_name tidak sah")

	// ErrInvalidPhone menandai phone yang bentuknya tidak sah.
	ErrInvalidPhone = errors.New("identity: phone tidak sah")

	// ErrInvalidAvatarURL menandai avatar_url yang bukan URL https yang sah.
	ErrInvalidAvatarURL = errors.New("identity: avatar_url tidak sah")

	// ErrInvalidLocale menandai locale yang tidak sesuai pola.
	ErrInvalidLocale = errors.New("identity: locale tidak sah")

	// ErrInvalidTimezone menandai timezone yang bukan zona waktu IANA.
	ErrInvalidTimezone = errors.New("identity: timezone tidak sah")

	// ErrInvalidFilter menandai filter status daftar yang tidak dikenal.
	ErrInvalidFilter = errors.New("identity: filter status tidak dikenal")
)

// Record adalah satu baris users yang terlihat bagi pemanggil. Kolom deleted_at tidak dibawa:
// baris soft-deleted tidak pernah dikembalikan.
type Record struct {
	// ID adalah kunci user.
	ID uuid.UUID

	// Username adalah pengenal opsional (unik global); nil bila tidak diisi.
	Username *string

	// DisplayName adalah nama tampilan.
	DisplayName string

	// FirstName adalah nama depan; nil bila tidak diisi.
	FirstName *string

	// LastName adalah nama belakang; nil bila tidak diisi.
	LastName *string

	// Email adalah alamat email huruf kecil (unik global).
	Email string

	// Phone adalah nomor telepon; nil bila tidak diisi.
	Phone *string

	// AvatarURL adalah URL gambar profil; nil bila tidak diisi. Tidak ada unggah.
	AvatarURL *string

	// Locale adalah locale pilihan user.
	Locale string

	// Timezone adalah zona waktu IANA pilihan user.
	Timezone string

	// Status adalah salah satu status user.
	Status string

	// LastLoginAt adalah waktu login terakhir (global lintas organisasi); nil bila belum pernah.
	LastLoginAt *time.Time

	// CreatedAt adalah waktu pembuatan.
	CreatedAt time.Time

	// UpdatedAt adalah waktu perubahan terakhir.
	UpdatedAt time.Time
}

// UpdateInput adalah pembaruan sebagian: nil berarti bidang tidak diubah. Untuk FirstName,
// LastName, Phone, dan AvatarURL string kosong menghapus nilainya; DisplayName, Locale, dan
// Timezone tidak boleh kosong. Email, username, status, dan id sengaja tidak ada di sini
// (ADR-0023 butir 24b).
type UpdateInput struct {
	// DisplayName mengganti display_name.
	DisplayName *string

	// FirstName mengganti first_name; string kosong menghapusnya.
	FirstName *string

	// LastName mengganti last_name; string kosong menghapusnya.
	LastName *string

	// Phone mengganti phone; string kosong menghapusnya.
	Phone *string

	// AvatarURL mengganti avatar_url; string kosong menghapusnya.
	AvatarURL *string

	// Locale mengganti locale.
	Locale *string

	// Timezone mengganti timezone.
	Timezone *string
}

// ListQuery adalah masukan daftar user.
type ListQuery struct {
	// Window adalah jendela halaman; selalu dipangkas ClampWindow.
	Window Window

	// Filter adalah salah satu dari FilterOpen (bawaan bila kosong), FilterAll, atau nilai status.
	Filter string
}

// Page adalah satu halaman daftar user beserta total yang cocok dengan filter.
type Page struct {
	// Items adalah baris halaman ini; tidak pernah nil.
	Items []Record

	// Total adalah jumlah seluruh baris yang cocok.
	Total int
}

// Window adalah jendela offset pada daftar.
type Window struct {
	// Offset adalah jumlah baris yang dilewati.
	Offset int

	// Limit adalah jumlah baris maksimum yang dikembalikan.
	Limit int
}

// ClampWindow memangkas Window ke batas yang sah: Offset tidak negatif, Limit antara 1 dan
// MaxPageSize (Limit nol atau negatif menjadi DefaultPageSize).
func ClampWindow(w Window) Window {
	if w.Offset < 0 {
		w.Offset = 0
	}
	if w.Limit < 1 {
		w.Limit = DefaultPageSize
	}
	if w.Limit > MaxPageSize {
		w.Limit = MaxPageSize
	}
	return w
}

// Nilai filter daftar selain nilai status.
const (
	// FilterOpen memuat semua status kecuali deactivated dan deleted (bawaan).
	FilterOpen = "open"

	// FilterAll memuat semua status.
	FilterAll = "all"
)

// StatusesForFilter menerjemahkan filter daftar ke himpunan status. Filter kosong sama dengan
// FilterOpen; nilai tak dikenal tidak pernah menjadi "semua".
func StatusesForFilter(filter string) ([]string, error) {
	switch filter {
	case "", FilterOpen:
		return []string{StatusPending, StatusActive, StatusSuspended, StatusLocked}, nil
	case FilterAll:
		return []string{StatusPending, StatusActive, StatusSuspended, StatusLocked, StatusDeactivated, StatusDeleted}, nil
	case StatusPending, StatusActive, StatusSuspended, StatusLocked, StatusDeactivated, StatusDeleted:
		return []string{filter}, nil
	default:
		return nil, ErrInvalidFilter
	}
}

var (
	localePattern   = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8}){0,3}$`)
	timezonePattern = regexp.MustCompile(`^[A-Za-z0-9_+-]+(/[A-Za-z0-9_+-]+){0,2}$`)
	phonePattern    = regexp.MustCompile(`^\+?[0-9 ().-]+$`)
)

func hasControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func validText(s string) bool { return utf8.ValidString(s) && !hasControl(s) }

// NormalizeDisplayName memangkas spasi tepi dan memvalidasi display_name.
func NormalizeDisplayName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || utf8.RuneCountInString(s) > MaxDisplayNameLength || !validText(s) {
		return "", ErrInvalidDisplayName
	}
	return s, nil
}

// normalizePersonName memangkas spasi tepi nama depan atau belakang; string kosong lolos
// (artinya menghapus). invalid adalah galat yang dikembalikan bila nilainya tidak sah.
func normalizePersonName(s string, invalid error) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > MaxPersonNameLength || !validText(s) {
		return "", invalid
	}
	return s, nil
}

// NormalizePhone memangkas spasi tepi dan memvalidasi phone: tanda plus hanya di awal, selebihnya
// angka, spasi, kurung, titik, dan tanda hubung, dengan MinPhoneDigits sampai MaxPhoneDigits
// angka. String kosong lolos (artinya menghapus).
func NormalizePhone(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if len(s) > MaxPhoneLength || !phonePattern.MatchString(s) {
		return "", ErrInvalidPhone
	}
	digits := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	if digits < MinPhoneDigits || digits > MaxPhoneDigits {
		return "", ErrInvalidPhone
	}
	return s, nil
}

// NormalizeAvatarURL memangkas spasi tepi dan memvalidasi avatar_url: hanya skema https dengan
// host, tanpa kredensial dan tanpa spasi. String kosong lolos (artinya menghapus). Tidak ada
// unggah dan tidak ada pengambilan URL oleh server.
func NormalizeAvatarURL(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if len(s) > MaxAvatarURLLength || !validText(s) || strings.ContainsRune(s, ' ') {
		return "", ErrInvalidAvatarURL
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return "", ErrInvalidAvatarURL
	}
	return s, nil
}

func normalizeLocale(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) > MaxLocaleLength || !localePattern.MatchString(s) {
		return "", ErrInvalidLocale
	}
	return s, nil
}

func normalizeTimezone(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) > MaxTimezoneLength || s == "Local" || !timezonePattern.MatchString(s) {
		return "", ErrInvalidTimezone
	}
	if _, err := time.LoadLocation(s); err != nil {
		return "", ErrInvalidTimezone
	}
	return s, nil
}

// Normalize memvalidasi dan menormalkan pembaruan, dan mengembalikan salinan baru (pointer
// pemanggil tidak dibagi). Tanpa satu pun bidang hasilnya ErrNothingToUpdate.
func (in UpdateInput) Normalize() (UpdateInput, error) {
	if in.DisplayName == nil && in.FirstName == nil && in.LastName == nil && in.Phone == nil &&
		in.AvatarURL == nil && in.Locale == nil && in.Timezone == nil {
		return UpdateInput{}, ErrNothingToUpdate
	}
	var out UpdateInput
	if in.DisplayName != nil {
		v, err := NormalizeDisplayName(*in.DisplayName)
		if err != nil {
			return UpdateInput{}, err
		}
		out.DisplayName = &v
	}
	if in.FirstName != nil {
		v, err := normalizePersonName(*in.FirstName, ErrInvalidFirstName)
		if err != nil {
			return UpdateInput{}, err
		}
		out.FirstName = &v
	}
	if in.LastName != nil {
		v, err := normalizePersonName(*in.LastName, ErrInvalidLastName)
		if err != nil {
			return UpdateInput{}, err
		}
		out.LastName = &v
	}
	if in.Phone != nil {
		v, err := NormalizePhone(*in.Phone)
		if err != nil {
			return UpdateInput{}, err
		}
		out.Phone = &v
	}
	if in.AvatarURL != nil {
		v, err := NormalizeAvatarURL(*in.AvatarURL)
		if err != nil {
			return UpdateInput{}, err
		}
		out.AvatarURL = &v
	}
	if in.Locale != nil {
		v, err := normalizeLocale(*in.Locale)
		if err != nil {
			return UpdateInput{}, err
		}
		out.Locale = &v
	}
	if in.Timezone != nil {
		v, err := normalizeTimezone(*in.Timezone)
		if err != nil {
			return UpdateInput{}, err
		}
		out.Timezone = &v
	}
	return out, nil
}

// nullableDiffers melaporkan apakah nilai baru (string kosong berarti hapus) berbeda dari nilai
// sekarang (nil berarti tidak terisi).
func nullableDiffers(current *string, next string) bool {
	cur := ""
	if current != nil {
		cur = *current
	}
	return cur != next
}

// ChangedFields mengembalikan nama bidang pada in (yang sudah dinormalkan) yang nilainya benar-
// benar berbeda dari r, terurut menurut abjad; tidak pernah nil. Hanya nama bidang yang dicatat
// audit, tidak pernah nilainya (data pribadi).
func (r Record) ChangedFields(in UpdateInput) []string {
	out := []string{}
	if in.DisplayName != nil && *in.DisplayName != r.DisplayName {
		out = append(out, FieldDisplayName)
	}
	if in.FirstName != nil && nullableDiffers(r.FirstName, *in.FirstName) {
		out = append(out, FieldFirstName)
	}
	if in.LastName != nil && nullableDiffers(r.LastName, *in.LastName) {
		out = append(out, FieldLastName)
	}
	if in.Phone != nil && nullableDiffers(r.Phone, *in.Phone) {
		out = append(out, FieldPhone)
	}
	if in.AvatarURL != nil && nullableDiffers(r.AvatarURL, *in.AvatarURL) {
		out = append(out, FieldAvatarURL)
	}
	if in.Locale != nil && *in.Locale != r.Locale {
		out = append(out, FieldLocale)
	}
	if in.Timezone != nil && *in.Timezone != r.Timezone {
		out = append(out, FieldTimezone)
	}
	sort.Strings(out)
	return out
}
