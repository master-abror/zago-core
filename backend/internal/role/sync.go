package role

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/master-abror/zago-core/backend/pkg/id"
)

// SyncResult menjelaskan apa yang dikerjakan SyncSystem.
type SyncResult struct {
	// IDs memetakan slug role sistem ke id barisnya.
	IDs map[string]uuid.UUID

	// RolesCreated dan RolesUpdated berisi slug. RolesUpdated mencakup adopsi baris minimal
	// bootstrap-admin (nama/deskripsi diselaraskan) dan pemulihan status 'active'.
	RolesCreated []string
	RolesUpdated []string

	// PermissionsAdded dan PermissionsRemoved menghitung baris role_permissions CORE yang ditambah
	// atau dicabut saat set role dikonvergensikan ke default.
	PermissionsAdded   int
	PermissionsRemoved int
}

// Changed melaporkan apakah ada baris role atau role_permissions yang berubah.
func (r SyncResult) Changed() bool {
	return len(r.RolesCreated)+len(r.RolesUpdated)+r.PermissionsAdded+r.PermissionsRemoved > 0
}

// SyncSystem menyelaraskan role sistem ke database DI DALAM transaksi pemanggil (pemanggil memegang
// kunci advisory, audit, dan commit). permIDs memetakan kode permission katalog ke id barisnya
// (hasil permission.Sync dalam transaksi yang sama).
//
// Upsert by slug pada baris global (organization_id IS NULL) sehingga baris minimal buatan
// bootstrap-admin DIADOPSI, bukan digandakan (uq_roles_global_slug). Set permission CORE tiap
// role dikonvergensikan persis ke default; permission modul pada role tidak disentuh.
func SyncSystem(ctx context.Context, tx pgx.Tx, roles []SystemRole, permIDs map[string]uuid.UUID) (SyncResult, error) {
	res := SyncResult{IDs: make(map[string]uuid.UUID, len(roles))}
	for _, def := range roles {
		roleID, created, updated, err := upsertRole(ctx, tx, def)
		if err != nil {
			return SyncResult{}, err
		}
		res.IDs[def.Slug] = roleID
		switch {
		case created:
			res.RolesCreated = append(res.RolesCreated, def.Slug)
		case updated:
			res.RolesUpdated = append(res.RolesUpdated, def.Slug)
		}

		want, err := desiredPermissionIDs(ctx, tx, def, permIDs)
		if err != nil {
			return SyncResult{}, err
		}
		added, removed, err := convergeCore(ctx, tx, roleID, want)
		if err != nil {
			return SyncResult{}, fmt.Errorf("role: set permission %q: %w", def.Slug, err)
		}
		res.PermissionsAdded += added
		res.PermissionsRemoved += removed
	}
	return res, nil
}

func upsertRole(ctx context.Context, tx pgx.Tx, def SystemRole) (roleID uuid.UUID, created, updated bool, err error) {
	var name, description, status string
	err = tx.QueryRow(ctx, `
		SELECT id, name, COALESCE(description, ''), status
		FROM roles WHERE organization_id IS NULL AND slug = $1`, def.Slug).
		Scan(&roleID, &name, &description, &status)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		roleID = id.NewID()
		if _, err = tx.Exec(ctx, `
			INSERT INTO roles (id, name, slug, description, role_type)
			VALUES ($1, $2, $3, $4, 'system')`,
			roleID, def.Name, def.Slug, def.Description); err != nil {
			return uuid.Nil, false, false, fmt.Errorf("role: buat %q: %w", def.Slug, err)
		}
		return roleID, true, false, nil
	case err != nil:
		return uuid.Nil, false, false, fmt.Errorf("role: baca %q: %w", def.Slug, err)
	}

	if name == def.Name && description == def.Description && status == "active" {
		return roleID, false, false, nil
	}
	if _, err = tx.Exec(ctx, `
		UPDATE roles SET name = $2, description = $3, status = 'active'
		WHERE id = $1 AND organization_id IS NULL`,
		roleID, def.Name, def.Description); err != nil {
		return uuid.Nil, false, false, fmt.Errorf("role: ubah %q: %w", def.Slug, err)
	}
	return roleID, false, true, nil
}

// desiredPermissionIDs menghitung id permission core yang harus dipegang role. Super Admin
// memegang SEMUA permission core is_system yang ada di database (termasuk orphan).
func desiredPermissionIDs(ctx context.Context, tx pgx.Tx, def SystemRole, permIDs map[string]uuid.UUID) ([]uuid.UUID, error) {
	if def.AllSystemPermissions {
		rows, err := tx.Query(ctx, "SELECT id FROM permissions WHERE is_system AND module_id IS NULL")
		if err != nil {
			return nil, fmt.Errorf("role: baca permission core: %w", err)
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return nil, fmt.Errorf("role: baca permission core: %w", err)
		}
		return ids, nil
	}
	ids := make([]uuid.UUID, 0, len(def.Permissions))
	for _, code := range def.Permissions {
		pid, ok := permIDs[code]
		if !ok {
			return nil, fmt.Errorf("role: default %q memuat permission %q yang tidak ada di katalog", def.Slug, code)
		}
		ids = append(ids, pid)
	}
	return ids, nil
}

// convergeCore menambah permission core yang kurang dan mencabut yang berlebih. Baris yang merujuk
// permission modul (module_id terisi) tidak dibaca maupun dihapus.
func convergeCore(ctx context.Context, tx pgx.Tx, roleID uuid.UUID, want []uuid.UUID) (added, removed int, err error) {
	rows, err := tx.Query(ctx, `
		SELECT rp.permission_id FROM role_permissions rp
		JOIN permissions p ON p.id = rp.permission_id
		WHERE rp.role_id = $1 AND p.is_system AND p.module_id IS NULL`, roleID)
	if err != nil {
		return 0, 0, err
	}
	have, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return 0, 0, err
	}

	seen := make(map[uuid.UUID]bool, len(want))
	for _, pid := range want {
		if seen[pid] || slices.Contains(have, pid) {
			continue
		}
		seen[pid] = true
		if _, err = tx.Exec(ctx, `
			INSERT INTO role_permissions (id, role_id, permission_id) VALUES ($1, $2, $3)`,
			id.NewID(), roleID, pid); err != nil {
			return 0, 0, err
		}
		added++
	}
	for _, pid := range have {
		if slices.Contains(want, pid) {
			continue
		}
		if _, err = tx.Exec(ctx,
			"DELETE FROM role_permissions WHERE role_id = $1 AND permission_id = $2", roleID, pid); err != nil {
			return 0, 0, err
		}
		removed++
	}
	return added, removed, nil
}
