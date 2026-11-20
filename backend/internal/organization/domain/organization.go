// Package domain memuat aturan murni organisasi (docs/02 §6): bentuk dan validasi masukan,
// status dan transisinya, serta tipe Access yang membedakan pemanggil platform dari pemanggil
// tenant. Paket ini tidak melakukan I/O.
package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
	// Zona waktu ikut tertanam di biner supaya validasi default_timezone tidak bergantung pada
	// basis data zona waktu image runtime.
	_ "time/tzdata"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Status organisasi (docs/02 §6). Archived adalah keadaan akhir: organisasi yang diarsipkan
// tidak dikembalikan ke aktif.
const (
	// StatusActive adalah organisasi yang beroperasi normal.
	StatusActive = "active"

	// StatusSuspended adalah organisasi yang ditangguhkan; hanya operator platform yang berwenang
	// yang masih dapat bekerja padanya.
	StatusSuspended = "suspended"

	// StatusArchived adalah organisasi yang diarsipkan (soft delete).
	StatusArchived = "archived"
)

// Batas masukan dan nilai bawaan. Batas panjang mengikuti kolom di migrasi 003.
const (
	// MaxNameLength adalah panjang name maksimum dalam rune.
	MaxNameLength = 200

	// MaxLegalNameLength adalah panjang legal_name maksimum dalam rune.
	MaxLegalNameLength = 255

	// MinSlugLength adalah panjang slug minimum.
	MinSlugLength = 2

	// MaxSlugLength adalah panjang slug maksimum.
	MaxSlugLength = 100

	// MaxLocaleLength adalah panjang default_locale maksimum.
	MaxLocaleLength = 20

	// MaxTimezoneLength adalah panjang default_timezone maksimum.
	MaxTimezoneLength = 100

	// DefaultLocale dan DefaultTimezone sama dengan nilai bawaan kolom di migrasi 003.
	DefaultLocale = "en-US"

	// DefaultTimezone adalah zona waktu bawaan organisasi baru.
	DefaultTimezone = "UTC"

	// DefaultPageSize adalah ukuran halaman bila pemanggil tidak menyebutnya.
	DefaultPageSize = 50

	// MaxPageSize adalah ukuran halaman maksimum.
	MaxPageSize = 100
)

// Galat domain organisasi. Lapisan transport memetakan masing-masing ke kode API; sentinel baru
// yang tidak didaftarkan di sana jatuh ke 503.
var (
	// ErrOrganizationNotFound menandai organisasi yang tidak ada ATAU bukan milik pemanggil.
	// Kedua keadaan sengaja tidak dapat dibedakan (tidak membocorkan keberadaan lintas tenant).
	ErrOrganizationNotFound = errors.New("organization: organisasi tidak ditemukan")

	// ErrSlugTaken menandai slug yang sudah dipakai organisasi lain.
	ErrSlugTaken = errors.New("organization: slug sudah dipakai")

	// ErrPlatformRequired menandai operasi lintas tenant yang dipanggil dengan Access tenant.
	ErrPlatformRequired = errors.New("organization: operasi ini hanya untuk scope platform")

	// ErrInvalidAccess menandai Access yang tidak sah (nilai nol atau organisasi kosong).
	ErrInvalidAccess = errors.New("organization: access tidak sah")

	// ErrNoTransaction menandai operasi yang membutuhkan transaksi tetapi dipanggil tanpa
	// transaksi pada context.
	ErrNoTransaction = errors.New("organization: operasi harus berjalan di dalam transaksi")

	// ErrInvalidTransition menandai perubahan status yang tidak diizinkan.
	ErrInvalidTransition = errors.New("organization: transisi status tidak diizinkan")

	// ErrOrganizationArchived menandai pembaruan pada organisasi yang sudah diarsipkan.
	ErrOrganizationArchived = errors.New("organization: organisasi sudah diarsipkan")

	// ErrNothingToUpdate menandai pembaruan tanpa satu pun bidang.
	ErrNothingToUpdate = errors.New("organization: tidak ada bidang yang diubah")

	// ErrInvalidID menandai id organisasi kosong pada Record.
	ErrInvalidID = errors.New("organization: id tidak sah")

	// ErrInvalidName menandai name yang kosong, terlalu panjang, atau memuat karakter kontrol.
	ErrInvalidName = errors.New("organization: name tidak sah")

	// ErrInvalidLegalName menandai legal_name yang terlalu panjang atau memuat karakter kontrol.
	ErrInvalidLegalName = errors.New("organization: legal_name tidak sah")

	// ErrInvalidSlug menandai slug yang tidak sesuai pola.
	ErrInvalidSlug = errors.New("organization: slug tidak sah")

	// ErrInvalidLocale menandai default_locale yang tidak sesuai pola.
	ErrInvalidLocale = errors.New("organization: default_locale tidak sah")

	// ErrInvalidTimezone menandai default_timezone yang bukan zona waktu IANA.
	ErrInvalidTimezone = errors.New("organization: default_timezone tidak sah")

	// ErrInvalidStatus menandai nilai status yang tidak dikenal.
	ErrInvalidStatus = errors.New("organization: status tidak dikenal")

	// ErrInvalidFilter menandai filter status daftar yang tidak dikenal.
	ErrInvalidFilter = errors.New("organization: filter status tidak dikenal")
)

// Access menyatakan siapa yang memanggil repository: operator platform (melihat semua
// organisasi) atau pemanggil tenant (hanya organisasi di ScopeContext-nya). Nilai nol TIDAK
// sah dan ditolak setiap metode repository, sehingga kelalaian tidak pernah menjadi akses
// platform; satu-satunya jalan ke akses platform adalah PlatformAccess.
type Access struct {
	tenantID uuid.UUID

	platform bool
}

// PlatformAccess mengembalikan Access platform. Hanya dipanggil bila ScopeContext request
// bersifat platform.
func PlatformAccess() Access { return Access{platform: true} }

// TenantAccess mengembalikan Access untuk satu organisasi (dari ScopeContext, bukan dari
// body/path/query).
func TenantAccess(organizationID uuid.UUID) (Access, error) {
	if organizationID == uuid.Nil {
		return Access{}, ErrInvalidAccess
	}
	return Access{tenantID: organizationID}, nil
}

// IsPlatform melaporkan apakah ini Access platform.
func (a Access) IsPlatform() bool { return a.platform }

// Valid melaporkan apakah Access dibuat lewat PlatformAccess atau TenantAccess.
func (a Access) Valid() bool { return a.platform || a.tenantID != uuid.Nil }

// Tenant mengembalikan organisasi Access tenant; ok=false untuk Access platform atau tak sah.
func (a Access) Tenant() (uuid.UUID, bool) {
	if a.platform || a.tenantID == uuid.Nil {
		return uuid.Nil, false
	}
	return a.tenantID, true
}

// Filter mengembalikan nilai parameter SQL pembatas tenant: nil untuk platform (tanpa batas),
// id organisasi untuk tenant, dan ErrInvalidAccess untuk Access tak sah.
func (a Access) Filter() (*uuid.UUID, error) {
	if a.platform {
		return nil, nil
	}
	if a.tenantID == uuid.Nil {
		return nil, ErrInvalidAccess
	}
	tid := a.tenantID
	return &tid, nil
}

// Record adalah satu baris organizations.
type Record struct {
	// ID adalah kunci organisasi.
	ID uuid.UUID

	// Name adalah nama tampilan.
	Name string

	// Slug adalah pengenal unik berpola huruf kecil, angka, dan tanda hubung.
	Slug string

	// LegalName adalah nama hukum; nil bila tidak diisi.
	LegalName *string

	// Status adalah salah satu dari StatusActive, StatusSuspended, StatusArchived.
	Status string

	// DefaultLocale adalah locale bawaan organisasi.
	DefaultLocale string

	// DefaultTimezone adalah zona waktu IANA bawaan organisasi.
	DefaultTimezone string

	// CreatedAt adalah waktu pembuatan.
	CreatedAt time.Time

	// UpdatedAt adalah waktu perubahan terakhir.
	UpdatedAt time.Time

	// ArchivedAt adalah isi kolom deleted_at; nil selama organisasi belum diarsipkan.
	ArchivedAt *time.Time
}

// CreateInput adalah masukan pembuatan organisasi.
type CreateInput struct {
	// Name wajib.
	Name string

	// Slug wajib; huruf besar dilipat menjadi huruf kecil.
	Slug string

	// LegalName opsional; nil atau string kosong berarti tidak diisi.
	LegalName *string

	// DefaultLocale opsional; kosong berarti DefaultLocale.
	DefaultLocale string

	// DefaultTimezone opsional; kosong berarti DefaultTimezone.
	DefaultTimezone string
}

// UpdateInput adalah pembaruan sebagian: nil berarti bidang tidak diubah. Slug dan status
// sengaja tidak ada di sini.
type UpdateInput struct {
	// Name mengganti name; tidak boleh kosong.
	Name *string

	// LegalName mengganti legal_name; string kosong menghapusnya.
	LegalName *string

	// DefaultLocale mengganti default_locale; tidak boleh kosong.
	DefaultLocale *string

	// DefaultTimezone mengganti default_timezone; tidak boleh kosong.
	DefaultTimezone *string
}

// ListQuery adalah masukan daftar organisasi.
type ListQuery struct {
	// Window adalah jendela halaman; selalu dipangkas ClampWindow.
	Window Window

	// Filter adalah salah satu dari FilterOpen (bawaan bila kosong), FilterAll, atau nilai status.
	Filter string
}

// Page adalah satu halaman daftar organisasi beserta total yang cocok dengan filter.
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
	// FilterOpen memuat organisasi aktif dan ditangguhkan (bawaan).
	FilterOpen = "open"

	// FilterAll memuat semua status.
	FilterAll = "all"
)

// StatusesForFilter menerjemahkan filter daftar ke himpunan status. Filter kosong sama dengan
// FilterOpen; nilai tak dikenal tidak pernah menjadi "semua".
func StatusesForFilter(filter string) ([]string, error) {
	switch filter {
	case "", FilterOpen:
		return []string{StatusActive, StatusSuspended}, nil
	case FilterAll:
		return []string{StatusActive, StatusSuspended, StatusArchived}, nil
	case StatusActive, StatusSuspended, StatusArchived:
		return []string{filter}, nil
	default:
		return nil, ErrInvalidFilter
	}
}

// ValidStatus melaporkan apakah s adalah status organisasi yang dikenal.
func ValidStatus(s string) bool {
	return s == StatusActive || s == StatusSuspended || s == StatusArchived
}

// PredecessorStatuses mengembalikan status asal yang boleh berpindah ke status to. Active dan
// suspended saling berpindah, dan keduanya dapat diarsipkan; archived tidak punya jalan keluar.
// Perpindahan ke status yang sama tidak diizinkan.
func PredecessorStatuses(to string) ([]string, error) {
	switch to {
	case StatusActive:
		return []string{StatusSuspended}, nil
	case StatusSuspended:
		return []string{StatusActive}, nil
	case StatusArchived:
		return []string{StatusActive, StatusSuspended}, nil
	default:
		return nil, ErrInvalidStatus
	}
}

var (
	slugPattern     = regexp.MustCompile(`^[A-Za-z0-9]+(-[A-Za-z0-9]+)*$`)
	localePattern   = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8}){0,3}$`)
	timezonePattern = regexp.MustCompile(`^[A-Za-z0-9_+-]+(/[A-Za-z0-9_+-]+){0,2}$`)
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

// NormalizeName memangkas spasi tepi dan memvalidasi name.
func NormalizeName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || utf8.RuneCountInString(s) > MaxNameLength || !validText(s) {
		return "", ErrInvalidName
	}
	return s, nil
}

func normalizeLegalName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > MaxLegalNameLength || !validText(s) {
		return "", ErrInvalidLegalName
	}
	return s, nil
}

// NormalizeSlug memangkas spasi tepi, memvalidasi pola ASCII, lalu melipat ke huruf kecil.
// Pola diperiksa SEBELUM pelipatan supaya karakter non-ASCII yang dilipat menjadi ASCII (mis.
// tanda Kelvin) tidak lolos.
func NormalizeSlug(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) < MinSlugLength || len(s) > MaxSlugLength || !slugPattern.MatchString(s) {
		return "", ErrInvalidSlug
	}
	return strings.ToLower(s), nil
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

// NewRecord membentuk organisasi baru berstatus aktif dari masukan: bidang dinormalkan dan
// divalidasi, bidang opsional kosong diberi nilai bawaan. id dibangkitkan pemanggil (id.NewID).
func NewRecord(organizationID uuid.UUID, in CreateInput) (Record, error) {
	if organizationID == uuid.Nil {
		return Record{}, ErrInvalidID
	}
	name, err := NormalizeName(in.Name)
	if err != nil {
		return Record{}, err
	}
	slug, err := NormalizeSlug(in.Slug)
	if err != nil {
		return Record{}, err
	}
	var legal *string
	if in.LegalName != nil {
		l, lerr := normalizeLegalName(*in.LegalName)
		if lerr != nil {
			return Record{}, lerr
		}
		if l != "" {
			legal = &l
		}
	}
	locale := strings.TrimSpace(in.DefaultLocale)
	if locale == "" {
		locale = DefaultLocale
	}
	locale, err = normalizeLocale(locale)
	if err != nil {
		return Record{}, err
	}
	zone := strings.TrimSpace(in.DefaultTimezone)
	if zone == "" {
		zone = DefaultTimezone
	}
	zone, err = normalizeTimezone(zone)
	if err != nil {
		return Record{}, err
	}
	return Record{ID: organizationID, Name: name, Slug: slug, LegalName: legal, Status: StatusActive, DefaultLocale: locale, DefaultTimezone: zone}, nil
}

// Validate memastikan Record yang akan ditulis sudah dalam bentuk normal. Repository
// memanggilnya, jadi Record yang dirakit tanpa NewRecord tidak melewati aturan bentuk.
func (r Record) Validate() error {
	if r.ID == uuid.Nil {
		return ErrInvalidID
	}
	if n, err := NormalizeName(r.Name); err != nil || n != r.Name {
		return ErrInvalidName
	}
	if s, err := NormalizeSlug(r.Slug); err != nil || s != r.Slug {
		return ErrInvalidSlug
	}
	if r.LegalName != nil {
		if l, err := normalizeLegalName(*r.LegalName); err != nil || l == "" || l != *r.LegalName {
			return ErrInvalidLegalName
		}
	}
	if l, err := normalizeLocale(r.DefaultLocale); err != nil || l != r.DefaultLocale {
		return ErrInvalidLocale
	}
	if z, err := normalizeTimezone(r.DefaultTimezone); err != nil || z != r.DefaultTimezone {
		return ErrInvalidTimezone
	}
	if !ValidStatus(r.Status) {
		return ErrInvalidStatus
	}
	return nil
}

// Normalize memvalidasi dan menormalkan pembaruan, dan mengembalikan salinan baru (pointer
// pemanggil tidak dibagi). Tanpa satu pun bidang hasilnya ErrNothingToUpdate.
func (in UpdateInput) Normalize() (UpdateInput, error) {
	if in.Name == nil && in.LegalName == nil && in.DefaultLocale == nil && in.DefaultTimezone == nil {
		return UpdateInput{}, ErrNothingToUpdate
	}
	var out UpdateInput
	if in.Name != nil {
		n, err := NormalizeName(*in.Name)
		if err != nil {
			return UpdateInput{}, err
		}
		out.Name = &n
	}
	if in.LegalName != nil {
		l, err := normalizeLegalName(*in.LegalName)
		if err != nil {
			return UpdateInput{}, err
		}
		out.LegalName = &l
	}
	if in.DefaultLocale != nil {
		l, err := normalizeLocale(*in.DefaultLocale)
		if err != nil {
			return UpdateInput{}, err
		}
		out.DefaultLocale = &l
	}
	if in.DefaultTimezone != nil {
		z, err := normalizeTimezone(*in.DefaultTimezone)
		if err != nil {
			return UpdateInput{}, err
		}
		out.DefaultTimezone = &z
	}
	return out, nil
}
