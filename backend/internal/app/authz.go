package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"

	authsvc "github.com/master-abror/zago-core/backend/internal/auth/service"
	authzapp "github.com/master-abror/zago-core/backend/internal/authorization/application"
	authzcache "github.com/master-abror/zago-core/backend/internal/authorization/cache"
	authzrepo "github.com/master-abror/zago-core/backend/internal/authorization/repository"
	authzhttp "github.com/master-abror/zago-core/backend/internal/authorization/transport/http"
	"github.com/master-abror/zago-core/backend/internal/httpserver"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

// authzStack adalah hasil perakitan Authorization Engine (ADR-0022 bagian 3): Authorizer di atas
// repositori effective permissions (PostgreSQL, otoritatif) dan cache berversi di Redis.
// invalidator nil bila tanpa Redis (tanpa cache, tidak ada yang perlu dibatalkan); platformEntry
// nil bila tanpa PostgreSQL. fieldGuard (ADR-0022 butir 41-43) memakai Authorizer yang sama dan
// definisi field dari katalog di PostgreSQL. sessions diisi withSessionRevoker setelah auth dirakit
// (T5d5, ADR-0022 butir 76): httpAPI menolak stack tanpa itu.
type authzStack struct {
	authorizer    *authzapp.Authorizer
	invalidator   authzapp.Invalidator
	txInvalidator authzapp.TxInvalidator
	platformEntry *authzapp.PlatformEntry
	fieldGuard    *authzapp.FieldGuard
	sessions      authzapp.SessionRevoker
}

// withSessionRevoker mengembalikan salinan stack dengan pencabut sesi yang dipakai AssignmentWriter
// saat assignment platform dicabut. Hanya menyimpan referensi: tidak ada I/O.
func (s authzStack) withSessionRevoker(r authzapp.SessionRevoker) authzStack {
	s.sessions = r
	return s
}

// newAuthzStack merakit engine. Hanya merakit objek: tidak ada I/O. rdb nil = tanpa cache
// (Authorizer tetap bekerja, setiap keputusan dihitung dari PostgreSQL). Nilai nil untuk cache dan
// invalidator sengaja interface nil, bukan pointer nil yang dibungkus interface.
func newAuthzStack(db kernel.DBTX, rdb redis.UniversalClient, log *slog.Logger) (authzStack, error) {
	var (
		effCache authzapp.EffectiveCache
		inv      authzapp.Invalidator
		txInv    authzapp.TxInvalidator
	)
	if rdb != nil {
		c := authzcache.New(rdb, authzcache.Options{Log: log})
		effCache, inv, txInv = c, c, c
	}
	authorizer, err := authzapp.NewAuthorizer(authzrepo.NewEffective(db), effCache)
	if err != nil {
		return authzStack{}, fmt.Errorf("rakit authorizer: %w", err)
	}
	// Policy (ADR-0022 butir 31-32) hanya berlaku bila dirakit di sini; tanpa ini setiap policy
	// diabaikan tanpa suara. authz_internal_test menjaganya lewat PoliciesEnabled.
	authorizer = authorizer.WithPolicies(authzrepo.NewPolicies(db))
	entry, err := authzapp.NewPlatformEntry(authorizer)
	if err != nil {
		return authzStack{}, fmt.Errorf("rakit platform entry: %w", err)
	}
	guard, err := authzapp.NewFieldGuard(authorizer, authzrepo.NewFields(db))
	if err != nil {
		return authzStack{}, fmt.Errorf("rakit field guard: %w", err)
	}
	return authzStack{authorizer: authorizer, invalidator: inv, txInvalidator: txInv, platformEntry: entry, fieldGuard: guard}, nil
}

// httpAPI merakit rakitan HTTP otorisasi (T5d, ADR-0022 butir 25): middleware Scope dan Authorize
// di atas Authorizer stack ini (yang SUDAH WithPolicies), ScopeResolver berbasis PostgreSQL, dan
// rute pengelolaan (T5d2: role dan katalog permission; T5d3: assignment; T5d4: policy dan simulate).
// Hanya merakit objek: tidak ada I/O. Penulis mencatat audit dan event di transaksinya sendiri,
// jadi audit wajib ada.
func (s authzStack) httpAPI(pool kernel.Pool, audit modulesdk.AuditRecorder, kern *httpserver.KernelServices, log *slog.Logger) (*httpserver.AuthzAPI, error) {
	// Pointer nil yang dibungkus interface akan lolos pemeriksaan nil di authzhttp.New dan panik di
	// request pertama; periksa di sini supaya kesalahan wiring gagal saat start.
	if s.authorizer == nil {
		return nil, errors.New("rakit http otorisasi: authorizer belum dirakit")
	}
	// Authorize dan Simulator memakai Authorizer yang sama; tanpa policy keduanya mengabaikan
	// setiap policy tanpa suara (ADR-0022 butir 31 dan 69h).
	if !s.authorizer.PoliciesEnabled() {
		return nil, errors.New("rakit http otorisasi: authorizer tanpa policy")
	}
	if audit == nil {
		return nil, errors.New("rakit http otorisasi: perekam audit wajib ada")
	}
	// Rute POST assignment memakai TenantIdempotency (kunci memuat scope, ADR-0022 butir 67), bukan
	// Idempotency bersama yang bisa memutar ulang respons lintas organisasi.
	if kern == nil || kern.Responder == nil || kern.Cursors == nil || kern.TenantIdempotency == nil {
		return nil, errors.New("rakit http otorisasi: toolkit kernel (Responder, Cursors, TenantIdempotency) wajib ada")
	}
	resolver, err := authzapp.NewScopeResolver(authzrepo.NewGroups(pool), log)
	if err != nil {
		return nil, fmt.Errorf("rakit scope resolver: %w", err)
	}
	mw, err := authzhttp.New(authzhttp.Deps{
		Scope: resolver, Authorizer: s.authorizer, Audit: audit, Responder: kern.Responder, Log: log,
	})
	if err != nil {
		return nil, fmt.Errorf("rakit middleware otorisasi: %w", err)
	}

	// Satu transaksi dan satu bus untuk semua penulis. txInvalidator adalah interface nil tanpa
	// Redis (penulis menerimanya nil; jangan bungkus pointer nil di sini).
	tx := kernel.NewTxManager(pool, log)
	bus := kernel.NewBus(pool, log)

	roleWriter, err := authzapp.NewRoleWriter(tx, authzrepo.NewRoles(pool), audit, bus, s.txInvalidator)
	if err != nil {
		return nil, fmt.Errorf("rakit role writer: %w", err)
	}
	roles, err := authzhttp.NewRoleHandlers(kern.Responder, authzrepo.NewRoleReader(pool), roleWriter, authzrepo.NewPermissionReader(pool))
	if err != nil {
		return nil, fmt.Errorf("rakit role handlers: %w", err)
	}

	assignWriter, err := authzapp.NewAssignmentWriter(tx, authzrepo.NewAssignments(pool), audit, bus, s.txInvalidator)
	if err != nil {
		return nil, fmt.Errorf("rakit assignment writer: %w", err)
	}
	// Tanpa pencabut sesi, sesi hasil masuk-lewat-platform tetap hidup setelah assignment platform
	// dicabut tanpa suara; stack tanpa itu ditolak saat start, bukan hanya dijaga tes.
	if s.sessions == nil {
		return nil, errors.New("rakit http otorisasi: pencabut sesi belum dirakit")
	}
	assignWriter = assignWriter.WithSessionRevoker(s.sessions, log)
	assigns, err := authzhttp.NewAssignmentHandlers(kern.Responder, kern.Cursors, authzrepo.NewAssignmentReader(pool), assignWriter)
	if err != nil {
		return nil, fmt.Errorf("rakit assignment handlers: %w", err)
	}
	idem := kern.TenantIdempotency.Require()
	assignRoutes := httpserver.AuthzMounterFunc(func(r chi.Router, authorize func(action string) func(http.Handler) http.Handler) {
		assigns.Mount(r, authorize, idem)
	})

	// Policy tidak di-cache dan tidak mengubah himpunan permission efektif, jadi PolicyWriter tidak
	// menerima invalidator (ADR-0022 butir 59). Rute policy tidak memakai Idempotency-Key.
	policyWriter, err := authzapp.NewPolicyWriter(tx, authzrepo.NewPolicies(pool), audit, bus)
	if err != nil {
		return nil, fmt.Errorf("rakit policy writer: %w", err)
	}
	policies, err := authzhttp.NewPolicyHandlers(kern.Responder, authzrepo.NewPolicyReader(pool), policyWriter)
	if err != nil {
		return nil, fmt.Errorf("rakit policy handlers: %w", err)
	}

	sim, err := s.newSimulator(pool, audit)
	if err != nil {
		return nil, err
	}
	simulate, err := authzhttp.NewSimulateHandlers(kern.Responder, sim)
	if err != nil {
		return nil, fmt.Errorf("rakit simulate handlers: %w", err)
	}

	return &httpserver.AuthzAPI{Middleware: mw, Routes: []httpserver.AuthzMounter{roles, assignRoutes, policies, simulate}}, nil
}

// newSimulator merakit Simulator di atas Authorizer stack ini, yang HARUS sudah WithPolicies
// (ADR-0022 butir 69h): Authorizer tanpa policy membuat simulasi menjawab ALLOW untuk request yang
// produksi tolak, tanpa galat apa pun. Karena itu stack tanpa policy ditolak saat start, bukan
// hanya dijaga tes. Hanya merakit objek: tidak ada I/O.
func (s authzStack) newSimulator(db kernel.DBTX, audit modulesdk.AuditRecorder) (*authzapp.Simulator, error) {
	if s.authorizer == nil {
		return nil, errors.New("rakit simulator: authorizer belum dirakit")
	}
	if !s.authorizer.PoliciesEnabled() {
		return nil, errors.New("rakit simulator: authorizer tanpa policy akan menjawab ALLOW untuk request yang ditolak policy")
	}
	sim, err := authzapp.NewSimulator(s.authorizer, authzrepo.NewMembers(db), authzrepo.NewGroups(db), audit)
	if err != nil {
		return nil, fmt.Errorf("rakit simulator: %w", err)
	}
	return sim, nil
}

// entryChecker mengembalikan PlatformEntryChecker untuk AuthService, atau interface nil bila tidak
// dirakit (identitas platform lalu tidak dapat memasuki organisasi mana pun: fail-closed).
func (s authzStack) entryChecker() authsvc.PlatformEntryChecker {
	if s.platformEntry == nil {
		return nil
	}
	return s.platformEntry
}

// syncAuthorization menyinkronkan katalog permission core dan role sistem (ADR-0022 butir 5-8)
// lewat pool runtime (app_user). Dipanggil SEKALI saat start, sesudah pool dibuka dan sebelum
// autentikasi dirakit, supaya role sistem (terutama Super Admin) sudah punya permission begitu
// API melayani request. Galat apa pun dikembalikan (pemanggil: ExitFailed); permission core yang
// tidak ada lagi di katalog (orphan) hanya dilaporkan dan tidak pernah dihapus otomatis.
// inv (boleh nil) menaikkan versi platform cache bila sinkron mengubah sesuatu; kegagalannya hanya
// di-log dan tidak menggagalkan start (ADR-0022 butir 21).
// Fungsi ini SATU percobaan; retry terbatas saat start ada di syncAuthorizationWithRetry (T4d,
// ADR-0022 butir 46).
func syncAuthorization(ctx context.Context, db authzapp.Beginner, inv authzapp.Invalidator, log *slog.Logger) error {
	syncer, err := authzapp.NewDefaultSyncer()
	if err != nil {
		return fmt.Errorf("rakit syncer otorisasi: %w: %w", errSyncSetup, err)
	}
	if inv != nil {
		syncer = syncer.WithInvalidator(inv, log)
	}
	res, err := syncer.Run(ctx, db)
	if err != nil {
		return fmt.Errorf("sinkron katalog otorisasi: %w", err)
	}
	logSync(log, res)
	return nil
}

// logSync melaporkan hasil sinkron: ringkasan (hanya hitungan, bukan rahasia) pada level info
// dan orphan pada level warn.
func logSync(log *slog.Logger, res authzapp.SyncResult) {
	if res.Changed() {
		log.Info("katalog otorisasi disinkronkan",
			"permissions_created", len(res.Permissions.Created),
			"permissions_updated", len(res.Permissions.Updated),
			"roles_created", len(res.Roles.RolesCreated),
			"roles_updated", len(res.Roles.RolesUpdated),
			"role_permissions_added", res.Roles.PermissionsAdded,
			"role_permissions_removed", res.Roles.PermissionsRemoved,
		)
	} else {
		log.Info("katalog otorisasi sudah sinkron")
	}
	if len(res.Permissions.Orphans) > 0 {
		log.Warn("permission core tidak ada di katalog (orphan; tidak dihapus otomatis)",
			"orphans", res.Permissions.Orphans)
	}
}
