package app

import (
	"context"
	"fmt"
	"log/slog"

	authzapp "github.com/master-abror/zago-core/backend/internal/authorization/application"
)

// syncAuthorization menyinkronkan katalog permission core dan role sistem (ADR-0022 butir 5-8)
// lewat pool runtime (app_user). Dipanggil SEKALI saat start, sesudah pool dibuka dan sebelum
// autentikasi dirakit, supaya role sistem (terutama Super Admin) sudah punya permission begitu
// API melayani request. Galat apa pun dikembalikan (pemanggil: ExitFailed); permission core yang
// tidak ada lagi di katalog (orphan) hanya dilaporkan dan tidak pernah dihapus otomatis.
func syncAuthorization(ctx context.Context, db authzapp.Beginner, log *slog.Logger) error {
	syncer, err := authzapp.NewDefaultSyncer()
	if err != nil {
		return fmt.Errorf("rakit syncer otorisasi: %w", err)
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
