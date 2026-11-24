package app

import (
	"errors"
	"fmt"
	"log/slog"

	authzrepo "github.com/master-abror/zago-core/backend/internal/authorization/repository"
	"github.com/master-abror/zago-core/backend/internal/httpserver"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	orgapp "github.com/master-abror/zago-core/backend/internal/organization/application"
	orgrepo "github.com/master-abror/zago-core/backend/internal/organization/repository"
	orghttp "github.com/master-abror/zago-core/backend/internal/organization/transport/http"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

// organizationRoutes merakit rute /organizations dan /organizations/{id}/members (ADR-0023 butir
// 18-20): repository di atas pool, Service dan MemberService, lalu orghttp.Handlers sebagai
// AuthzMounter. Hasilnya TIDAK masuk ke httpAPI: tabel 15 rute otorisasi M04 tetap utuh dan
// dijaga tesnya sendiri; RunAPI menambahkan mounter ini ke AuthzAPI.Routes sehingga rute
// organisasi memakai Middleware yang sama (Scope, lalu Authorize(action) per rute).
//
// Semua penulis memakai satu TxManager, satu Bus, dan kunci advisory organisasi yang sama dengan
// penulis role, assignment, dan policy (authzrepo.LockOrganization; ADR-0022 butir 54).
// txInvalidator stack ini adalah interface nil tanpa Redis dan diteruskan sebagai interface nil
// (bukan pointer nil terbungkus). Hanya merakit objek: tidak ada I/O.
func (s authzStack) organizationRoutes(pool kernel.Pool, audit modulesdk.AuditRecorder, rs *httpx.Responder, log *slog.Logger) (httpserver.AuthzMounter, error) {
	if audit == nil {
		return nil, errors.New("rakit rute organisasi: perekam audit wajib ada")
	}
	if rs == nil {
		return nil, errors.New("rakit rute organisasi: Responder wajib ada")
	}

	tx := kernel.NewTxManager(pool, log)
	bus := kernel.NewBus(pool, log)

	var inv orgapp.TxInvalidator
	if s.txInvalidator != nil {
		inv = s.txInvalidator
	}
	locker := orgapp.LockFunc(authzrepo.LockOrganization)
	orgs := orgrepo.NewOrganizations(pool)

	svc, err := orgapp.NewService(tx, orgs, locker, audit, bus, inv)
	if err != nil {
		return nil, fmt.Errorf("rakit organization service: %w", err)
	}
	members, err := orgapp.NewMemberService(tx, orgrepo.NewOrganizationMembers(pool), orgs, authzrepo.NewAssignments(pool), locker, audit, bus, inv)
	if err != nil {
		return nil, fmt.Errorf("rakit member service: %w", err)
	}
	handlers, err := orghttp.NewHandlers(rs, svc, members)
	if err != nil {
		return nil, fmt.Errorf("rakit organization handlers: %w", err)
	}
	return handlers, nil
}
