package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"

	authsvc "github.com/master-abror/zago-core/backend/internal/auth/service"
	authzapp "github.com/master-abror/zago-core/backend/internal/authorization/application"
	authzcache "github.com/master-abror/zago-core/backend/internal/authorization/cache"
	authzrepo "github.com/master-abror/zago-core/backend/internal/authorization/repository"
	"github.com/master-abror/zago-core/backend/internal/kernel"
)

// authzStack adalah hasil perakitan Authorization Engine (ADR-0022 bagian 3): Authorizer di atas
// repositori effective permissions (PostgreSQL, otoritatif) dan cache berversi di Redis.
// invalidator nil bila tanpa Redis (tanpa cache, tidak ada yang perlu dibatalkan); platformEntry
// nil bila tanpa PostgreSQL. fieldGuard (ADR-0022 butir 41-43) memakai Authorizer yang sama dan
// definisi field dari katalog di PostgreSQL.
type authzStack struct {
	authorizer    *authzapp.Authorizer
	invalidator   authzapp.Invalidator
	platformEntry *authzapp.PlatformEntry
	fieldGuard    *authzapp.FieldGuard
}

// newAuthzStack merakit engine. Hanya merakit objek: tidak ada I/O. rdb nil = tanpa cache
// (Authorizer tetap bekerja, setiap keputusan dihitung dari PostgreSQL). Nilai nil untuk cache dan
// invalidator sengaja interface nil, bukan pointer nil yang dibungkus interface.
func newAuthzStack(db kernel.DBTX, rdb redis.UniversalClient, log *slog.Logger) (authzStack, error) {
	var (
		effCache authzapp.EffectiveCache
		inv      authzapp.Invalidator
	)
	if rdb != nil {
		c := authzcache.New(rdb, authzcache.Options{Log: log})
		effCache, inv = c, c
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
	return authzStack{authorizer: authorizer, invalidator: inv, platformEntry: entry, fieldGuard: guard}, nil
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
func syncAuthorization(ctx context.Context, db authzapp.Beginner, inv authzapp.Invalidator, log *slog.Logger) error {
	syncer, err := authzapp.NewDefaultSyncer()
	if err != nil {
		return fmt.Errorf("rakit syncer otorisasi: %w", err)
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
