package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel"
)

// Effective menghitung effective permissions dari PostgreSQL (docs/06 §5.2). Inilah sumber
// kebenaran; cache (T3b) hanya menyalinnya.
type Effective struct {
	db kernel.DBTX
}

// NewEffective membuat Effective di atas db (pool runtime app_user).
func NewEffective(db kernel.DBTX) *Effective { return &Effective{db: db} }

// effectiveSQL mengikuti docs/06 §5.2 dan MENGETATKANNYA (setiap titik di bawah menolak, tidak
// ada yang menambah izin):
//
//   - Assignment berlaku bila belum dicabut, belum kedaluwarsa, dan role-nya aktif.
//   - Scope platform: hanya subjek `user` yang cocok dengan $1. Subjek grup/organisasi/service
//     TIDAK berlaku di scope platform, supaya keanggotaan grup atau organisasi tidak pernah
//     menjadi jalur menuju otoritas platform. Tidak bergantung pada organisasi mana pun.
//   - Scope organization/group (tenant): assignment harus ber-organization_id = $2 (organisasi
//     scope), organisasi harus aktif dan tidak terhapus, user harus anggota aktif organisasi,
//     dan role harus global (sistem) atau milik organisasi yang sama.
//   - Scope organization: scope_id kosong atau organisasi itu sendiri. Scope group: scope_id harus
//     sama dengan grup aktif $3 dan grup itu aktif di organisasi $2.
//   - Subjek tenant: user itu sendiri; organisasi $2 (anggota aktif); atau grup yang user-nya
//     anggota aktif dan grupnya aktif di organisasi $2. Subjek `service` tidak pernah berlaku
//     untuk user.
//   - Scope resource dan own tidak termasuk (dievaluasi saat pengecekan, docs/06 §5.2).
//
// $2 dan $3 NULL berarti scope tanpa organisasi/grup; perbandingan dengan NULL tidak pernah
// benar, sehingga cabang tenant otomatis mati (gagal tertutup).
const effectiveSQL = `
SELECT DISTINCT p.code, ra.scope_type
  FROM role_assignments ra
  JOIN roles r             ON r.id = ra.role_id
  JOIN role_permissions rp ON rp.role_id = r.id
  JOIN permissions p       ON p.id = rp.permission_id
 WHERE ra.revoked_at IS NULL
   AND (ra.expires_at IS NULL OR ra.expires_at > now())
   AND r.status = 'active'
   AND (
        (    ra.scope_type     = 'platform'
         AND ra.organization_id IS NULL
         AND ra.subject_type   = 'user'
         AND ra.subject_id     = $1)
     OR (    ra.scope_type IN ('organization', 'group')
         AND ra.organization_id = $2
         AND (r.organization_id IS NULL OR r.organization_id = ra.organization_id)
         AND EXISTS (
               SELECT 1 FROM organizations o
                WHERE o.id = $2 AND o.status = 'active' AND o.deleted_at IS NULL)
         AND EXISTS (
               SELECT 1 FROM organization_memberships om
                WHERE om.organization_id = $2 AND om.user_id = $1 AND om.status = 'active')
         AND (
                 (ra.scope_type = 'organization' AND (ra.scope_id IS NULL OR ra.scope_id = $2))
              OR (ra.scope_type = 'group' AND ra.scope_id = $3
                  AND EXISTS (
                        SELECT 1 FROM groups g
                         WHERE g.id = ra.scope_id AND g.organization_id = $2
                           AND g.status = 'active' AND g.deleted_at IS NULL))
             )
         AND (
                 (ra.subject_type = 'user' AND ra.subject_id = $1)
              OR (ra.subject_type = 'organization' AND ra.subject_id = $2)
              OR (ra.subject_type = 'group' AND EXISTS (
                        SELECT 1
                          FROM group_memberships gm
                          JOIN groups sg
                            ON sg.id = gm.group_id AND sg.organization_id = gm.organization_id
                         WHERE gm.group_id        = ra.subject_id
                           AND gm.user_id         = $1
                           AND gm.organization_id = $2
                           AND gm.status          = 'active'
                           AND sg.status          = 'active'
                           AND sg.deleted_at IS NULL))
             )
        )
   )`

// Load mengembalikan effective permissions userID pada scope. Scope yang melanggar invarian
// ditolak dengan domain.ErrInvalidScope, user UUID nol ditolak, dan kegagalan database
// dikembalikan sebagai galat (tidak pernah himpunan kosong yang menyamar sebagai "tidak punya
// izin"). Hasil selalu berupa himpunan baru.
func (e *Effective) Load(ctx context.Context, userID uuid.UUID, scope domain.ScopeContext) (domain.EffectivePermissions, error) {
	if userID == uuid.Nil {
		return domain.EffectivePermissions{}, errors.New("authorization: user id kosong")
	}
	// NewScope menegakkan invarian dan menyalin pointer, sehingga scope yang dibangun tangan
	// (mis. dipalsukan) tidak lolos ke kueri.
	valid, err := domain.NewScope(scope.OrganizationID, scope.ActiveGroupID)
	if err != nil {
		return domain.EffectivePermissions{}, err
	}

	rows, err := e.db.Query(ctx, effectiveSQL, userID, optionalID(valid.OrganizationID), optionalID(valid.ActiveGroupID))
	if err != nil {
		return domain.EffectivePermissions{}, fmt.Errorf("authorization: kueri effective permissions: %w", err)
	}
	defer rows.Close()

	b := domain.NewEffectiveBuilder()
	for rows.Next() {
		var code, scopeType string
		if err := rows.Scan(&code, &scopeType); err != nil {
			return domain.EffectivePermissions{}, fmt.Errorf("authorization: baca effective permissions: %w", err)
		}
		level, err := levelOf(scopeType)
		if err != nil {
			return domain.EffectivePermissions{}, err
		}
		if err := b.Add(code, level); err != nil {
			return domain.EffectivePermissions{}, fmt.Errorf("authorization: permission %q: %w", code, err)
		}
	}
	if err := rows.Err(); err != nil {
		return domain.EffectivePermissions{}, fmt.Errorf("authorization: baca effective permissions: %w", err)
	}
	return b.Build(), nil
}

// levelOf memetakan scope_type assignment ke level keputusan. Hanya tiga scope yang dapat muncul
// dari effectiveSQL; yang lain berarti kueri atau data menyimpang dan dijadikan galat.
func levelOf(scopeType string) (domain.DecisionLevel, error) {
	switch scopeType {
	case "platform":
		return domain.LevelPlatform, nil
	case "organization":
		return domain.LevelOrganization, nil
	case "group":
		return domain.LevelGroup, nil
	default:
		return "", fmt.Errorf("authorization: scope_type tak terduga %q pada effective permissions", scopeType)
	}
}

// optionalID mengubah *uuid.UUID menjadi argumen kueri: nil menjadi NULL.
func optionalID(p *uuid.UUID) any {
	if p == nil {
		return nil
	}
	return *p
}
