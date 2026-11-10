package authzhttp

import (
	"net/http"

	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
)

// Kode error endpoint pengelolaan otorisasi (docs/06 §11-§12, docs/08 §11; ADR-0022 butir 62).
// Pesan bawaan berbahasa Inggris, tetap, dan tidak pernah memuat nilai yang dikirim klien.
// Kode umum (resource_not_found, validation_failed) dipakai untuk assignment dan policy yang
// tidak ditemukan serta untuk galat validasi per field; kode di bawah hanya untuk kasus yang
// dibutuhkan klien untuk bercabang.
var (
	// RoleNotFound: role tidak ada di organisasi pemanggil. Role organisasi lain dan role yang
	// tidak ada memberi respons yang sama, supaya kode ini bukan alat menebak id.
	RoleNotFound = httpx.Register(httpx.Code{Name: "role_not_found", Status: http.StatusNotFound, Message: "The role was not found."})

	// PermissionNotFound: sebuah kode permission di body tidak terdaftar di katalog.
	PermissionNotFound = httpx.Register(httpx.Code{Name: "permission_not_found", Status: http.StatusUnprocessableEntity, Message: "One or more permissions are not registered."})

	// GrantExceedsAuthority: pemberi mencoba memberi atau menyentuh lebih dari otoritasnya
	// (authority ceiling, docs/06 §5.3). Kode sendiri, bukan permission_denied, supaya UI dan
	// pencarian audit dapat membedakannya. Respons tidak memuat daftar permission.
	GrantExceedsAuthority = httpx.Register(httpx.Code{Name: "grant_exceeds_authority", Status: http.StatusForbidden, Message: "You cannot grant or change more authority than you hold."})

	// InvalidScope: role tidak boleh ditetapkan pada scope yang diminta.
	InvalidScope = httpx.Register(httpx.Code{Name: "invalid_scope", Status: http.StatusBadRequest, Message: "The requested scope is not valid for this operation."})

	// RoleSlugTaken: slug role sudah dipakai di organisasi pemanggil atau dicadangkan.
	RoleSlugTaken = httpx.Register(httpx.Code{Name: "role_slug_taken", Status: http.StatusConflict, Message: "A role with this slug already exists."})

	// RoleImmutable: role sistem tidak dapat diubah lewat API; klon sebagai role custom.
	RoleImmutable = httpx.Register(httpx.Code{Name: "role_immutable", Status: http.StatusConflict, Message: "System roles cannot be changed through the API."})

	// RoleArchived: role sudah diarsipkan.
	RoleArchived = httpx.Register(httpx.Code{Name: "role_archived", Status: http.StatusConflict, Message: "The role is archived."})

	// AssignmentExists: assignment aktif yang sama sudah ada.
	AssignmentExists = httpx.Register(httpx.Code{Name: "assignment_exists", Status: http.StatusConflict, Message: "An identical active assignment already exists."})

	// AssignmentRevoked: assignment sudah dicabut.
	AssignmentRevoked = httpx.Register(httpx.Code{Name: "assignment_revoked", Status: http.StatusConflict, Message: "The assignment is already revoked."})

	// LastSuperAdmin: assignment Super Admin platform terakhir yang aktif tidak boleh dicabut.
	LastSuperAdmin = httpx.Register(httpx.Code{Name: "last_super_admin", Status: http.StatusConflict, Message: "The last active platform administrator assignment cannot be revoked."})

	// PolicyCodeTaken: kode policy sudah dipakai (organisasi atau global).
	PolicyCodeTaken = httpx.Register(httpx.Code{Name: "policy_code_taken", Status: http.StatusConflict, Message: "A policy with this code already exists."})

	// PolicyLimitReached: batas policy aktif untuk cakupan itu tercapai.
	PolicyLimitReached = httpx.Register(httpx.Code{Name: "policy_limit_reached", Status: http.StatusConflict, Message: "The limit of active policies has been reached."})
)
