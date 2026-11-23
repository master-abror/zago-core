// Package orghttp memuat lapisan HTTP modul organisasi (docs/08 §10.6, ADR-0023 butir 18):
// kode error, pemetaan galat domain ke kode API, dan DTO. Handler organisasi dan anggota
// berada di paket yang sama.
package orghttp

import (
	"net/http"

	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
)

// Kode error endpoint organisasi dan anggota (docs/08 §11; ADR-0023 butir 18). Pesan bawaan
// berbahasa Inggris, tetap, dan tidak pernah memuat nilai yang dikirim klien. Kode umum
// (validation_failed, permission_denied, internal_error) dipakai untuk galat masukan, galat
// operasi platform-saja, dan galat pemrograman; kode di bawah hanya untuk kasus yang dibutuhkan
// klien untuk bercabang.
var (
	// OrganizationNotFound: organisasi tidak ada atau bukan milik pemanggil. Kedua keadaan
	// memberi respons yang sama, supaya kode ini bukan alat menebak id lintas tenant.
	OrganizationNotFound = httpx.Register(httpx.Code{Name: "organization_not_found", Status: http.StatusNotFound, Message: "The organization was not found."})

	// MemberNotFound: anggota tidak ada, milik organisasi lain, atau sudah dihapus.
	MemberNotFound = httpx.Register(httpx.Code{Name: "member_not_found", Status: http.StatusNotFound, Message: "The member was not found."})

	// OrganizationSlugTaken: slug sudah dipakai organisasi lain.
	OrganizationSlugTaken = httpx.Register(httpx.Code{Name: "organization_slug_taken", Status: http.StatusConflict, Message: "An organization with this slug already exists."})

	// OrganizationArchived: organisasi sudah diarsipkan dan tidak dapat diubah.
	OrganizationArchived = httpx.Register(httpx.Code{Name: "organization_archived", Status: http.StatusConflict, Message: "The organization is archived."})

	// InvalidStatusTransition: perubahan status yang diminta tidak diizinkan dari status sekarang.
	InvalidStatusTransition = httpx.Register(httpx.Code{Name: "invalid_status_transition", Status: http.StatusConflict, Message: "The status change is not allowed from the current status."})

	// MemberExists: pengguna sudah menjadi anggota organisasi itu.
	MemberExists = httpx.Register(httpx.Code{Name: "member_exists", Status: http.StatusConflict, Message: "The user is already a member of this organization."})

	// CannotRemoveSelf: pemanggil tenant tidak dapat menghapus keanggotaannya sendiri.
	CannotRemoveSelf = httpx.Register(httpx.Code{Name: "cannot_remove_self", Status: http.StatusConflict, Message: "You cannot remove your own membership."})

	// UserNotEligible: pengguna tidak ada, tidak aktif, atau sudah dihapus. Ketiganya tidak
	// dapat dibedakan dari respons.
	UserNotEligible = httpx.Register(httpx.Code{Name: "user_not_eligible", Status: http.StatusUnprocessableEntity, Message: "The user does not exist or is not active."})
)
