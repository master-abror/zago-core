package authzhttp

import "github.com/master-abror/zago-core/backend/internal/authorization/domain"

// PermissionResponse adalah satu permission pada GET /permissions.
type PermissionResponse struct {
	// Code adalah kode lengkap, mis. role.manage.
	Code string `json:"code"`

	// Resource adalah bagian kode sebelum titik terakhir.
	Resource string `json:"resource"`

	// Action adalah bagian kode setelah titik terakhir.
	Action string `json:"action"`

	// Description adalah uraian permission (string kosong bila tidak ada).
	Description string `json:"description"`

	// Module adalah kode modul pemilik; null untuk permission core.
	Module *string `json:"module"`

	// IsSystem benar untuk permission yang dideklarasikan core.
	IsSystem bool `json:"is_system"`
}

// NewPermissionResponse membentuk respons dari baris katalog.
func NewPermissionResponse(p domain.PermissionRecord) PermissionResponse {
	out := PermissionResponse{
		Code:        p.Code,
		Resource:    p.Resource,
		Action:      p.Action,
		Description: p.Description,
		IsSystem:    p.System,
	}
	if p.Module != "" {
		module := p.Module
		out.Module = &module
	}
	return out
}

// RolePermissionsResponse adalah data respons GET dan PUT /roles/{id}/permissions: himpunan kode
// permission role, terurut menurut byte.
type RolePermissionsResponse struct {
	// Permissions selalu ada pada respons (irisan kosong, bukan null).
	Permissions []string `json:"permissions"`
}

// NewRolePermissionsResponse membentuk respons dari kode permission role.
func NewRolePermissionsResponse(codes []string) RolePermissionsResponse {
	if codes == nil {
		codes = []string{}
	}
	return RolePermissionsResponse{Permissions: codes}
}
