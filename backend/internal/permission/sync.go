package permission

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/master-abror/zago-core/backend/pkg/id"
)

// ErrCodeConflict: kode katalog core ternyata dimiliki sebuah modul (permissions.module_id terisi).
// Modul tidak boleh mendeklarasikan kode di namespace core (docs/07 §7), jadi ini tanda data rusak
// dan sinkron berhenti keras alih-alih mengambil alih baris itu diam-diam.
var ErrCodeConflict = errors.New("permission: kode katalog core dimiliki modul")

// SyncResult menjelaskan apa yang dikerjakan Sync.
type SyncResult struct {
	// IDs memetakan SEMUA kode katalog ke id barisnya (dipakai sinkron role).
	IDs map[string]uuid.UUID

	// Created dan Updated berisi kode, dalam urutan katalog.
	Created []string
	Updated []string

	// Orphans adalah permission core (is_system, tanpa modul) yang sudah tidak ada di katalog.
	// Tidak pernah dihapus otomatis; penanganannya lewat migrasi (docs/06 §5.4).
	Orphans []string
}

// Changed melaporkan apakah ada baris permission yang dibuat atau diubah.
func (r SyncResult) Changed() bool { return len(r.Created)+len(r.Updated) > 0 }

type existing struct {
	id          uuid.UUID
	moduleOwned bool
	isSystem    bool
	resource    string
	action      string
	description string
}

// Sync menyelaraskan katalog core ke tabel permissions DI DALAM transaksi pemanggil (pemanggil
// memegang kunci advisory, audit, dan commit). Upsert by code; tidak pernah menghapus. Semua
// konflik dideteksi sebelum satu pun baris ditulis.
func Sync(ctx context.Context, tx pgx.Tx, defs []Def) (SyncResult, error) {
	codes := make([]string, 0, len(defs))
	seen := make(map[string]bool, len(defs))
	for _, d := range defs {
		if _, _, err := Split(d.Code); err != nil {
			return SyncResult{}, err
		}
		if seen[d.Code] {
			return SyncResult{}, fmt.Errorf("permission: kode ganda di katalog: %q", d.Code)
		}
		seen[d.Code] = true
		codes = append(codes, d.Code)
	}

	have, err := loadExisting(ctx, tx, codes)
	if err != nil {
		return SyncResult{}, err
	}
	for _, d := range defs {
		if e, ok := have[d.Code]; ok && e.moduleOwned {
			return SyncResult{}, fmt.Errorf("%w: %q", ErrCodeConflict, d.Code)
		}
	}

	res := SyncResult{IDs: make(map[string]uuid.UUID, len(defs))}
	for _, d := range defs {
		resource, action, _ := Split(d.Code) // sudah divalidasi di atas
		e, ok := have[d.Code]
		if !ok {
			newID := id.NewID()
			if _, err := tx.Exec(ctx, `
				INSERT INTO permissions (id, resource, action, code, description, is_system)
				VALUES ($1, $2, $3, $4, $5, true)`,
				newID, resource, action, d.Code, d.Description); err != nil {
				return SyncResult{}, fmt.Errorf("permission: buat %q: %w", d.Code, err)
			}
			res.IDs[d.Code] = newID
			res.Created = append(res.Created, d.Code)
			continue
		}
		res.IDs[d.Code] = e.id
		if e.isSystem && e.resource == resource && e.action == action && e.description == d.Description {
			continue
		}
		if _, err := tx.Exec(ctx, `
			UPDATE permissions SET resource = $2, action = $3, description = $4, is_system = true
			WHERE id = $1 AND module_id IS NULL`,
			e.id, resource, action, d.Description); err != nil {
			return SyncResult{}, fmt.Errorf("permission: ubah %q: %w", d.Code, err)
		}
		res.Updated = append(res.Updated, d.Code)
	}

	res.Orphans, err = loadOrphans(ctx, tx, codes)
	if err != nil {
		return SyncResult{}, err
	}
	return res, nil
}

func loadExisting(ctx context.Context, tx pgx.Tx, codes []string) (map[string]existing, error) {
	rows, err := tx.Query(ctx, `
		SELECT code, id, module_id IS NOT NULL, is_system, resource, action, COALESCE(description, '')
		FROM permissions WHERE code = ANY($1)`, codes)
	if err != nil {
		return nil, fmt.Errorf("permission: baca baris yang ada: %w", err)
	}
	defer rows.Close()

	have := make(map[string]existing, len(codes))
	for rows.Next() {
		var (
			code string
			e    existing
		)
		if err := rows.Scan(&code, &e.id, &e.moduleOwned, &e.isSystem, &e.resource, &e.action, &e.description); err != nil {
			return nil, fmt.Errorf("permission: pindai baris: %w", err)
		}
		have[code] = e
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("permission: baca baris yang ada: %w", err)
	}
	return have, nil
}

func loadOrphans(ctx context.Context, tx pgx.Tx, codes []string) ([]string, error) {
	rows, err := tx.Query(ctx, `
		SELECT code FROM permissions
		WHERE is_system AND module_id IS NULL AND NOT (code = ANY($1))
		ORDER BY code`, codes)
	if err != nil {
		return nil, fmt.Errorf("permission: cari orphan: %w", err)
	}
	orphans, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("permission: cari orphan: %w", err)
	}
	return orphans, nil
}
