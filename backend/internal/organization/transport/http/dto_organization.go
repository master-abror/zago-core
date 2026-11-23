package orghttp

import (
	"time"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/internal/organization/domain"
)

// DTO endpoint organisasi (docs/08 §10.6; ADR-0023 butir 18). Body didekode lewat
// httpx.DecodeJSON yang menolak field tak dikenal: DTO sengaja TIDAK punya id, status, atau
// organization_id, dan slug tidak ada pada DTO ubah (slug tidak dapat diubah), sehingga body
// yang membawanya ditolak sebagai unknown_field. Validasi isi (panjang, pola, zona waktu) ada di
// domain; DTO hanya memeriksa bentuk yang tidak dapat dibedakan di sana.

// CreateOrganizationRequest adalah body POST /organizations. Urutan field berbeda dari
// domain.CreateInput dengan sengaja, supaya konversi struct tidak tampak mungkin.
type CreateOrganizationRequest struct {
	// Slug adalah pengenal organisasi, unik, tidak dapat diubah.
	Slug string `json:"slug"`

	// Name adalah nama tampilan.
	Name string `json:"name"`

	// DefaultLocale opsional; kosong berarti en-US.
	DefaultLocale string `json:"default_locale"`

	// DefaultTimezone opsional; kosong berarti UTC.
	DefaultTimezone string `json:"default_timezone"`

	// LegalName opsional; null atau string kosong berarti tidak diisi.
	LegalName *string `json:"legal_name"`
}

// Input mengubah body menjadi masukan layanan.
func (q CreateOrganizationRequest) Input() domain.CreateInput {
	return domain.CreateInput{
		Name:            q.Name,
		Slug:            q.Slug,
		LegalName:       q.LegalName,
		DefaultLocale:   q.DefaultLocale,
		DefaultTimezone: q.DefaultTimezone,
	}
}

// UpdateOrganizationRequest adalah body PATCH /organizations/{id}: field yang tidak dikirim
// (atau null) tidak diubah. Slug tidak dapat diubah dan tidak ada di sini. Urutan field berbeda
// dari domain.UpdateInput dengan sengaja.
type UpdateOrganizationRequest struct {
	// DefaultTimezone adalah zona waktu IANA baru; tidak boleh kosong.
	DefaultTimezone *string `json:"default_timezone"`

	// DefaultLocale adalah locale baru; tidak boleh kosong.
	DefaultLocale *string `json:"default_locale"`

	// LegalName adalah nama hukum baru; string kosong menghapusnya.
	LegalName *string `json:"legal_name"`

	// Name adalah nama tampilan baru; tidak boleh kosong.
	Name *string `json:"name"`
}

// Validate menolak perubahan kosong.
func (q UpdateOrganizationRequest) Validate() error {
	if q.Name == nil && q.LegalName == nil && q.DefaultLocale == nil && q.DefaultTimezone == nil {
		return httpx.Validation(httpx.FieldError{Code: "empty_patch", Message: "the request changes nothing"})
	}
	return nil
}

// Input mengubah body menjadi masukan layanan.
func (q UpdateOrganizationRequest) Input() domain.UpdateInput {
	return domain.UpdateInput{
		Name:            q.Name,
		LegalName:       q.LegalName,
		DefaultLocale:   q.DefaultLocale,
		DefaultTimezone: q.DefaultTimezone,
	}
}

// OrganizationResponse adalah satu organisasi pada respons.
type OrganizationResponse struct {
	// ID adalah id organisasi.
	ID uuid.UUID `json:"id"`

	// Name adalah nama tampilan.
	Name string `json:"name"`

	// Slug adalah pengenal unik organisasi.
	Slug string `json:"slug"`

	// LegalName adalah nama hukum; null bila tidak diisi.
	LegalName *string `json:"legal_name"`

	// Status adalah active, suspended, atau archived.
	Status string `json:"status"`

	// DefaultLocale adalah locale bawaan organisasi.
	DefaultLocale string `json:"default_locale"`

	// DefaultTimezone adalah zona waktu IANA bawaan organisasi.
	DefaultTimezone string `json:"default_timezone"`

	// CreatedAt adalah waktu pembuatan (UTC).
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt adalah waktu perubahan terakhir (UTC).
	UpdatedAt time.Time `json:"updated_at"`

	// ArchivedAt adalah waktu pengarsipan (UTC); null selama belum diarsipkan.
	ArchivedAt *time.Time `json:"archived_at"`
}

// NewOrganizationResponse membentuk respons dari baris organisasi.
func NewOrganizationResponse(r domain.Record) OrganizationResponse {
	out := OrganizationResponse{
		ID:              r.ID,
		Name:            r.Name,
		Slug:            r.Slug,
		LegalName:       r.LegalName,
		Status:          r.Status,
		DefaultLocale:   r.DefaultLocale,
		DefaultTimezone: r.DefaultTimezone,
		CreatedAt:       r.CreatedAt.UTC(),
		UpdatedAt:       r.UpdatedAt.UTC(),
	}
	if r.ArchivedAt != nil {
		at := r.ArchivedAt.UTC()
		out.ArchivedAt = &at
	}
	return out
}

// NewOrganizationResponses membentuk larik respons; hasilnya tidak pernah nil sehingga daftar
// kosong tampil sebagai [] di JSON.
func NewOrganizationResponses(rows []domain.Record) []OrganizationResponse {
	out := make([]OrganizationResponse, 0, len(rows))
	for _, r := range rows {
		out = append(out, NewOrganizationResponse(r))
	}
	return out
}
