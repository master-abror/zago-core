package authzhttp

import (
	"net/http"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
)

// IdempotencyScope mengembalikan penanda scope untuk httpx.IdempotencyOptions.Scope (ADR-0022
// butir 67). Nilainya diturunkan HANYA dari ScopeContext hasil middleware Scope (organisasi dari
// Identity, grup aktif yang sudah diverifikasi), tidak pernah dari header atau body:
// "platform", "org:<uuid>", atau "grp:<org-uuid>.<group-uuid>".
//
// ok=false bila request belum melewati middleware Scope atau ScopeContext-nya tidak konsisten
// (grup aktif tanpa organisasi); httpx menjawab 500 dan tidak mengeksekusi handler.
func IdempotencyScope(r *http.Request) (string, bool) {
	s, ok := domain.ScopeFromContext(r.Context())
	if !ok {
		return "", false
	}
	org, tenant := s.Tenant()
	if !tenant {
		if s.ActiveGroupID != nil {
			return "", false
		}
		return "platform", true
	}
	if s.ActiveGroupID != nil {
		return "grp:" + org.String() + "." + s.ActiveGroupID.String(), true
	}
	return "org:" + org.String(), true
}
