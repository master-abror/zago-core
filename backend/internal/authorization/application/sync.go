// Package application memuat kasus penggunaan Authorization Engine (docs/06 §13). Berkas ini:
// sinkron katalog permission core dan role sistem saat startup (docs/00 B-04, ADR-0022 butir 5-8).
package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/master-abror/zago-core/backend/internal/permission"
	"github.com/master-abror/zago-core/backend/internal/role"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// ActionCatalogSynced adalah nama activity yang ditulis bila sinkron mengubah sesuatu.
const ActionCatalogSynced = "authz.catalog_synced"

// SyncLock menserialkan sinkron bersamaan antar-instance (advisory lock per transaksi). Berbeda dari
// kunci seeder platform (0x7a61676f01) dan bootstrap-admin (0x7a61676f02).
const SyncLock int64 = 0x7a61676f03 // "zago" + 03

// Beginner dipenuhi *pgx.Conn dan *pgxpool.Pool.
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// SyncResult menggabungkan hasil sinkron permission dan role.
type SyncResult struct {
	Permissions permission.SyncResult
	Roles       role.SyncResult
}

// Changed melaporkan apakah ada baris yang dibuat, diubah, atau dicabut.
func (r SyncResult) Changed() bool { return r.Permissions.Changed() || r.Roles.Changed() }

// Syncer menyinkronkan katalog permission dan role sistem dari kode ke database.
type Syncer struct {
	defs  []permission.Def
	roles []role.SystemRole
}

// NewSyncer membuat Syncer dari katalog dan role yang diberikan, dan menolak definisi yang
// tak konsisten SEBELUM database disentuh: kode tidak sah atau ganda, slug kosong atau ganda,
// dan default role yang memuat kode di luar katalog.
func NewSyncer(defs []permission.Def, roles []role.SystemRole) (*Syncer, error) {
	inCatalog := make(map[string]bool, len(defs))
	for _, d := range defs {
		if _, _, err := permission.Split(d.Code); err != nil {
			return nil, err
		}
		if inCatalog[d.Code] {
			return nil, fmt.Errorf("authorization: kode ganda di katalog: %q", d.Code)
		}
		inCatalog[d.Code] = true
	}
	slugs := make(map[string]bool, len(roles))
	for _, r := range roles {
		if r.Slug == "" || r.Name == "" {
			return nil, errors.New("authorization: role sistem butuh slug dan nama")
		}
		if slugs[r.Slug] {
			return nil, fmt.Errorf("authorization: slug role sistem ganda: %q", r.Slug)
		}
		slugs[r.Slug] = true
		seen := make(map[string]bool, len(r.Permissions))
		for _, code := range r.Permissions {
			if !inCatalog[code] {
				return nil, fmt.Errorf("authorization: role %q memuat permission %q yang tidak ada di katalog", r.Slug, code)
			}
			if seen[code] {
				return nil, fmt.Errorf("authorization: role %q memuat permission %q dua kali", r.Slug, code)
			}
			seen[code] = true
		}
	}
	return &Syncer{defs: append([]permission.Def(nil), defs...), roles: append([]role.SystemRole(nil), roles...)}, nil
}

// NewDefaultSyncer membuat Syncer untuk katalog core dan role sistem bawaan (docs/06 §5.4, §5.5).
func NewDefaultSyncer() (*Syncer, error) {
	return NewSyncer(permission.Catalog(), role.SystemRoles())
}

// Run menjalankan sinkron dalam SATU transaksi yang diserialkan advisory lock. Galat apa pun
// me-rollback semuanya (tak ada permission atau role setengah jadi). Idempoten: run tanpa
// perubahan tidak menulis audit.
func (s *Syncer) Run(ctx context.Context, db Beginner) (SyncResult, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return SyncResult{}, fmt.Errorf("authorization: mulai transaksi sinkron: %w", err)
	}
	res, err := s.apply(ctx, tx)
	if err != nil {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		return SyncResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return SyncResult{}, fmt.Errorf("authorization: commit sinkron: %w", err)
	}
	return res, nil
}

func (s *Syncer) apply(ctx context.Context, tx pgx.Tx) (SyncResult, error) {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", SyncLock); err != nil {
		return SyncResult{}, fmt.Errorf("authorization: kunci advisory sinkron: %w", err)
	}

	perms, err := permission.Sync(ctx, tx, s.defs)
	if err != nil {
		return SyncResult{}, err
	}
	roles, err := role.SyncSystem(ctx, tx, s.roles, perms.IDs)
	if err != nil {
		return SyncResult{}, err
	}
	res := SyncResult{Permissions: perms, Roles: roles}

	if res.Changed() {
		if err := writeAudit(ctx, tx, res); err != nil {
			return SyncResult{}, err
		}
	}
	return res, nil
}

// writeAudit menulis activity pada transaksi yang sama dengan perubahan state. Metadata hanya
// berisi hitungan dan kode orphan (bukan rahasia).
func writeAudit(ctx context.Context, tx pgx.Tx, res SyncResult) error {
	meta, err := json.Marshal(map[string]any{
		"permissions_created":  len(res.Permissions.Created),
		"permissions_updated":  len(res.Permissions.Updated),
		"roles_created":        len(res.Roles.RolesCreated),
		"roles_updated":        len(res.Roles.RolesUpdated),
		"role_permissions_add": res.Roles.PermissionsAdded,
		"role_permissions_del": res.Roles.PermissionsRemoved,
		"orphans":              res.Permissions.Orphans,
	})
	if err != nil {
		return fmt.Errorf("authorization: metadata audit sinkron: %w", err)
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO activities (id, actor_type, action, resource_type, scope_type, result, metadata)
		VALUES ($1, 'system', $2, 'authorization_catalog', 'platform', 'success', $3::jsonb)`,
		id.NewID(), ActionCatalogSynced, meta); err != nil {
		return fmt.Errorf("authorization: tulis activity sinkron: %w", err)
	}
	return nil
}
