package authzhttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/internal/permission"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

// ActivityAccessDenied adalah action activity untuk request yang ditolak Authorize (result=denied).
const ActivityAccessDenied = "authz.access_denied"

// Authorizer memutuskan Check. *application.Authorizer memenuhinya.
type Authorizer interface {
	Can(ctx context.Context, check domain.Check) (domain.Decision, error)
}

// Authorize menjaga rute dengan satu aksi katalog (docs/06 §9). Dipasang SESUDAH Scope:
//
//	Authenticate -> CSRF -> RequireAuth -> Scope -> Authorize(action) -> handler
//
// Perilaku (semuanya gagal tertutup; handler hanya tercapai pada Decision.Allowed tanpa galat):
//   - Tanpa Identity: 401 authentication_required.
//   - Identity ada tetapi Scope tidak ada: urutan pemasangan salah (kesalahan server) → 500.
//   - Galat engine: ErrInvalidCheck/ErrInvalidScope → 500 (kesalahan pemrograman); selain itu
//     (PostgreSQL gagal dsb.) → 503. Galat tidak pernah menjadi izin, walau Decision ikut terisi.
//   - Ditolak: 403 scope_mismatch bila alasannya ReasonScopeMismatch, 403
//     policy_evaluation_failed bila ReasonPolicyEvaluationFailed, selain itu 403 permission_denied
//     seragam (tanpa menyebut aksi, alasan, atau kode policy ke klien; penolakan policy tampil
//     sebagai permission_denied biasa). Penolakan direkam sebagai activity result=denied
//     (transaksi terpisah, docs/10 §4); untuk penolakan policy, kode policy ikut di metadata.
//
// Authorize memakai Resource nil: pemeriksaan sumber daya tertentu (organisasi, pemilik, grup
// sumber daya) dilakukan handler lewat Authorizer.Can dengan ResourceRef. Authorize panik saat
// dipasang bila Authorizer tidak diberikan atau aksi tak sah: kesalahan wiring harus gagal saat
// start, bukan saat request pertama.
func (m *Middleware) Authorize(action string) func(http.Handler) http.Handler {
	if m.authz == nil {
		panic("authzhttp: Authorize membutuhkan Deps.Authorizer")
	}
	if _, _, err := permission.Split(action); err != nil {
		panic(fmt.Sprintf("authzhttp: aksi rute %q tidak sah: %v", action, err))
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			ident, ok := authdomain.IdentityFromContext(ctx)
			if !ok {
				m.rs.Error(w, r, httpx.AuthenticationRequired.New())
				return
			}
			scope, ok := domain.ScopeFromContext(ctx)
			if !ok {
				m.log.ErrorContext(ctx, "Authorize berjalan tanpa Scope: urutan middleware salah", "action", action)
				m.rs.Error(w, r, httpx.InternalError.New())
				return
			}
			dec, err := m.authz.Can(ctx, domain.Check{Identity: ident, Scope: scope, Action: action})
			if err != nil {
				m.engineFailure(w, r, action, err)
				return
			}
			if dec.Allowed {
				next.ServeHTTP(w, r)
				return
			}
			m.deny(w, r, scope, action, dec)
		})
	}
}

func (m *Middleware) engineFailure(w http.ResponseWriter, r *http.Request, action string, err error) {
	if errors.Is(err, application.ErrInvalidCheck) || errors.Is(err, domain.ErrInvalidScope) {
		m.log.ErrorContext(r.Context(), "Check otorisasi salah bentuk (kesalahan pemrograman)", "action", action, "error", err)
		m.rs.Error(w, r, httpx.InternalError.Wrap(err))
		return
	}
	m.log.ErrorContext(r.Context(), "pemeriksaan otorisasi gagal karena infrastruktur", "action", action, "error", err)
	m.rs.Error(w, r, httpx.ServiceUnavailable.Wrap(err))
}

func (m *Middleware) deny(w http.ResponseWriter, r *http.Request, scope domain.ScopeContext, action string, dec domain.Decision) {
	if m.audit != nil {
		reason := "unspecified"
		if auditableDenyReason(dec.Reason) {
			reason = dec.Reason
		}
		meta := map[string]any{
			"required_action": action,
			"reason":          reason,
			"method":          r.Method,
		}
		if isPolicyReason(dec.Reason) && dec.PolicyCode != "" {
			meta["policy_code"] = dec.PolicyCode
		}
		m.audit.Record(auditContext(r.Context(), scope), modulesdk.ActivityEntry{
			Action:   ActivityAccessDenied,
			Result:   modulesdk.ResultDenied,
			Metadata: meta,
		})
	}
	switch dec.Reason {
	case domain.ReasonScopeMismatch:
		m.rs.Error(w, r, ScopeMismatch.New())
	case domain.ReasonPolicyEvaluationFailed:
		m.rs.Error(w, r, PolicyEvaluationFailed.New())
	default:
		m.rs.Error(w, r, httpx.PermissionDenied.New())
	}
}

func isPolicyReason(reason string) bool {
	return reason == domain.ReasonPolicyDenied || reason == domain.ReasonPolicyEvaluationFailed
}

// auditableDenyReason: hanya alasan stabil yang dikenal yang masuk audit; selain itu "unspecified".
func auditableDenyReason(reason string) bool {
	return reason == domain.ReasonNoPermission || reason == domain.ReasonScopeMismatch || isPolicyReason(reason)
}

// auditContext melengkapi context dengan scope request supaya activity penolakan memuat
// scope_type/scope_id (grup aktif terverifikasi > organisasi > platform).
func auditContext(ctx context.Context, s domain.ScopeContext) context.Context {
	ks := kernel.Scope{Type: "platform"}
	if org, ok := s.Tenant(); ok {
		ks = kernel.Scope{Type: "organization", ID: org}
		if grp, ok := s.Group(); ok {
			ks = kernel.Scope{Type: "group", ID: grp}
		}
	}
	return kernel.WithScope(ctx, ks)
}
