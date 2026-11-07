package application

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/authorization/cache"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/pkg/id"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

// maxAuditCodes membatasi daftar kode permission pada metadata audit; jumlah sebenarnya selalu
// dicatat terpisah.
const maxAuditCodes = 50

// TxRunner menjalankan fn di dalam satu transaksi database; dipenuhi *kernel.TxManager.
type TxRunner interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// EventPublisher menulis event ke outbox pada transaksi context; dipenuhi *kernel.Bus.
type EventPublisher interface {
	Publish(ctx context.Context, event modulesdk.Event)
}

// TxInvalidator menaikkan versi cache otorisasi di dalam transaksi pengubah; dipenuhi
// *cache.Cache. Nil berarti tidak ada cache (tanpa Redis), jadi tidak ada yang perlu dinaikkan.
type TxInvalidator interface {
	InvalidateInTx(ctx context.Context, t cache.Targets) error
}

// RoleStore adalah jalur tulis role; dipenuhi repository.Roles. Semua metode berjalan pada
// transaksi context.
type RoleStore interface {
	LockOrganization(ctx context.Context, organizationID uuid.UUID) error
	Insert(ctx context.Context, row domain.RoleRecord) error
	ForUpdate(ctx context.Context, roleID uuid.UUID, organizationID *uuid.UUID) (domain.RoleRecord, error)
	GroupActive(ctx context.Context, organizationID, groupID uuid.UUID) (bool, error)
	UpdateText(ctx context.Context, roleID uuid.UUID, name, description string) error
	Archive(ctx context.Context, roleID uuid.UUID) error
	PermissionCodes(ctx context.Context, roleID uuid.UUID) ([]string, error)
	MissingPermissions(ctx context.Context, codes []string) ([]string, error)
	AddPermissions(ctx context.Context, roleID uuid.UUID, codes []string, grantedBy uuid.UUID) (int, error)
	RemovePermissions(ctx context.Context, roleID uuid.UUID, codes []string) (int, error)
	GrantorEffective(ctx context.Context, userID uuid.UUID, scope domain.ScopeContext) (domain.EffectivePermissions, error)
}

// Grantor adalah pemanggil jalur tulis: user yang bertindak dan scope request-nya (dari
// middleware Scope). Layanan mempercayai keduanya; pemeriksaan permission aksi (mis. role.manage)
// adalah tugas middleware Authorize, sedangkan layanan menegakkan boundary dan authority ceiling.
type Grantor struct {
	UserID uuid.UUID
	Scope  domain.ScopeContext
}

// CreateRoleInput adalah masukan pembuatan role. Type salah satu organization, group, atau
// custom (tipe system ditolak).
type CreateRoleInput struct {
	Name        string
	Slug        string
	Description string
	Type        string
	GroupID     *uuid.UUID
	Permissions []string
}

// RoleResult adalah role beserta kode permission-nya setelah operasi.
type RoleResult struct {
	Role        domain.RoleRecord
	Permissions []string
}

// RoleWriter menulis role dan role_permissions (ADR-0022 butir 53-55). Setiap operasi satu
// transaksi: kunci advisory organisasi, baca role dengan kunci baris, himpunan pemberi dari
// PostgreSQL di dalam transaksi itu, boundary dan authority ceiling, tulis, invalidasi cache,
// audit, dan event outbox. Kegagalan di langkah mana pun membatalkan semuanya.
type RoleWriter struct {
	tx     TxRunner
	store  RoleStore
	audit  modulesdk.AuditRecorder
	events EventPublisher
	inv    TxInvalidator
}

// NewRoleWriter merakit RoleWriter. inv boleh nil (tanpa cache); lainnya wajib.
func NewRoleWriter(tx TxRunner, store RoleStore, audit modulesdk.AuditRecorder, events EventPublisher, inv TxInvalidator) (*RoleWriter, error) {
	if tx == nil || store == nil || audit == nil || events == nil {
		return nil, errors.New("authorization: RoleWriter butuh tx, store, audit, dan events")
	}
	return &RoleWriter{tx: tx, store: store, audit: audit, events: events, inv: inv}, nil
}

// CreateRole membuat role di organisasi pemanggil. Scope platform ditolak
// (domain.ErrTargetOutsideScope): Super Admin masuk dulu ke organisasi lewat switch-organization.
// Seluruh permission yang diminta harus tercakup authority ceiling pemberi pada boundary role.
func (w *RoleWriter) CreateRole(ctx context.Context, g Grantor, in CreateRoleInput) (RoleResult, error) {
	draft, err := domain.NormalizeRoleDraft(domain.RoleDraft{Name: in.Name, Slug: in.Slug, Description: in.Description})
	if err != nil {
		return RoleResult{}, err
	}
	codes, err := domain.NormalizePermissionCodes(in.Permissions)
	if err != nil {
		return RoleResult{}, err
	}
	org, ok := g.Scope.Tenant()
	if !ok {
		return RoleResult{}, domain.ErrTargetOutsideScope
	}
	target, err := domain.ValidateRoleBoundary(in.Type, &org, in.GroupID)
	if err != nil {
		return RoleResult{}, err
	}

	var result RoleResult
	err = w.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := w.store.LockOrganization(ctx, org); err != nil {
			return err
		}
		if in.GroupID != nil {
			active, err := w.store.GroupActive(ctx, org, *in.GroupID)
			if err != nil {
				return err
			}
			if !active {
				return domain.ErrGroupNotFound
			}
		}
		if err := w.requireRegistered(ctx, codes); err != nil {
			return err
		}
		if err := w.checkCeiling(ctx, g, target, codes); err != nil {
			return err
		}
		rec := domain.RoleRecord{
			ID:             id.NewID(),
			OrganizationID: &org,
			GroupID:        in.GroupID,
			Name:           draft.Name,
			Slug:           draft.Slug,
			Description:    draft.Description,
			Type:           in.Type,
			Status:         "active",
		}
		if err := w.store.Insert(ctx, rec); err != nil {
			return err
		}
		if _, err := w.store.AddPermissions(ctx, rec.ID, codes, g.UserID); err != nil {
			return err
		}
		// Tanpa invalidasi cache: role baru belum punya assignment, jadi tidak ada himpunan
		// efektif yang berubah.
		meta := map[string]any{"slug": rec.Slug, "role_type": rec.Type, "permission_count": len(codes)}
		if in.GroupID != nil {
			meta["group_id"] = in.GroupID.String()
		}
		w.recordAudit(ctx, g, org, "role.create", rec.ID, meta)
		if err := w.publish(ctx, "authz.role.created", g, org, rec, map[string]any{"permission_count": len(codes)}); err != nil {
			return err
		}
		result = RoleResult{Role: rec, Permissions: codes}
		return nil
	})
	if err != nil {
		w.recordDenial(ctx, g, org, "role.create", uuid.Nil, err)
		return RoleResult{}, err
	}
	return result, nil
}

// UpdateRole mengubah nama dan deskripsi role non-sistem yang aktif. Slug tidak dapat diubah.
// Pemberi harus tercakup authority ceiling untuk permission yang sedang dipegang role itu:
// admin tidak dapat mengubah role yang memuat izin di atasnya.
func (w *RoleWriter) UpdateRole(ctx context.Context, g Grantor, roleID uuid.UUID, name, description string) (RoleResult, error) {
	n, d, err := domain.NormalizeRoleText(name, description)
	if err != nil {
		return RoleResult{}, err
	}
	org, ok := g.Scope.Tenant()
	if !ok {
		return RoleResult{}, domain.ErrTargetOutsideScope
	}

	var result RoleResult
	err = w.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := w.store.LockOrganization(ctx, org); err != nil {
			return err
		}
		row, target, current, err := w.loadMutable(ctx, roleID, org)
		if err != nil {
			return err
		}
		if err := w.checkCeiling(ctx, g, target, current); err != nil {
			return err
		}
		if row.Name != n || row.Description != d {
			if err := w.store.UpdateText(ctx, row.ID, n, d); err != nil {
				return err
			}
			row.Name, row.Description = n, d
			w.recordAudit(ctx, g, org, "role.update", row.ID, map[string]any{"slug": row.Slug})
			if err := w.publish(ctx, "authz.role.updated", g, org, row, nil); err != nil {
				return err
			}
		}
		result = RoleResult{Role: row, Permissions: current}
		return nil
	})
	if err != nil {
		w.recordDenial(ctx, g, org, "role.update", roleID, err)
		return RoleResult{}, err
	}
	return result, nil
}

// SetPermissions mengganti seluruh himpunan permission role non-sistem yang aktif (PUT).
// Authority ceiling berlaku untuk gabungan himpunan lama dan baru: admin tidak dapat menyentuh
// role yang memuat izin di atasnya, juga tidak untuk mencabutnya. Himpunan yang sama dengan yang
// berlaku tidak menulis, tidak mengaudit, dan tidak menginvalidasi apa pun.
func (w *RoleWriter) SetPermissions(ctx context.Context, g Grantor, roleID uuid.UUID, requested []string) (RoleResult, error) {
	want, err := domain.NormalizePermissionCodes(requested)
	if err != nil {
		return RoleResult{}, err
	}
	org, ok := g.Scope.Tenant()
	if !ok {
		return RoleResult{}, domain.ErrTargetOutsideScope
	}

	var result RoleResult
	err = w.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := w.store.LockOrganization(ctx, org); err != nil {
			return err
		}
		row, target, current, err := w.loadMutable(ctx, roleID, org)
		if err != nil {
			return err
		}
		if err := w.requireRegistered(ctx, want); err != nil {
			return err
		}
		if err := w.checkCeiling(ctx, g, target, unionCodes(current, want)); err != nil {
			return err
		}
		toAdd, toRemove := diffCodes(want, current), diffCodes(current, want)
		if len(toAdd) == 0 && len(toRemove) == 0 {
			result = RoleResult{Role: row, Permissions: current}
			return nil
		}
		if _, err := w.store.RemovePermissions(ctx, row.ID, toRemove); err != nil {
			return err
		}
		if _, err := w.store.AddPermissions(ctx, row.ID, toAdd, g.UserID); err != nil {
			return err
		}
		if err := w.invalidate(ctx, org); err != nil {
			return err
		}
		meta := map[string]any{"slug": row.Slug}
		meta["added_count"] = len(toAdd)
		meta["removed_count"] = len(toRemove)
		meta["added"] = capCodes(toAdd)
		meta["removed"] = capCodes(toRemove)
		w.recordAudit(ctx, g, org, "role.permissions.update", row.ID, meta)
		payload := map[string]any{"added_count": len(toAdd), "removed_count": len(toRemove)}
		if err := w.publish(ctx, "authz.role.permissions_changed", g, org, row, payload); err != nil {
			return err
		}
		result = RoleResult{Role: row, Permissions: want}
		return nil
	})
	if err != nil {
		w.recordDenial(ctx, g, org, "role.permissions.update", roleID, err)
		return RoleResult{}, err
	}
	return result, nil
}

// ArchiveRole mengarsipkan role non-sistem yang aktif (DELETE /roles/{id}). Role yang sudah
// dipakai assignment tidak dihapus (role_assignments.role_id RESTRICT): assignment-nya menjadi
// tidak berlaku karena engine hanya menghitung role aktif, dan cache organisasi dinaikkan.
// Authority ceiling berlaku untuk permission yang dipegang role itu.
func (w *RoleWriter) ArchiveRole(ctx context.Context, g Grantor, roleID uuid.UUID) error {
	org, ok := g.Scope.Tenant()
	if !ok {
		return domain.ErrTargetOutsideScope
	}
	err := w.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := w.store.LockOrganization(ctx, org); err != nil {
			return err
		}
		row, target, current, err := w.loadMutable(ctx, roleID, org)
		if err != nil {
			return err
		}
		if err := w.checkCeiling(ctx, g, target, current); err != nil {
			return err
		}
		if err := w.store.Archive(ctx, row.ID); err != nil {
			return err
		}
		if err := w.invalidate(ctx, org); err != nil {
			return err
		}
		w.recordAudit(ctx, g, org, "role.archive", row.ID, map[string]any{"slug": row.Slug})
		return w.publish(ctx, "authz.role.archived", g, org, row, nil)
	})
	if err != nil {
		w.recordDenial(ctx, g, org, "role.archive", roleID, err)
		return err
	}
	return nil
}

// loadMutable membaca role dengan kunci baris lalu menegakkan: milik organisasi pemanggil atau
// sistem (sistem ditolak), belum diarsipkan, dan boundary sah. Mengembalikan role, target
// ceiling-nya, dan kode permission yang sedang dipegang.
func (w *RoleWriter) loadMutable(ctx context.Context, roleID, org uuid.UUID) (domain.RoleRecord, domain.GrantTarget, []string, error) {
	row, err := w.store.ForUpdate(ctx, roleID, &org)
	if err != nil {
		return domain.RoleRecord{}, domain.GrantTarget{}, nil, err
	}
	ref := row.Ref()
	if err := domain.CheckRoleMutable(ref); err != nil {
		return domain.RoleRecord{}, domain.GrantTarget{}, nil, err
	}
	if ref.Archived {
		return domain.RoleRecord{}, domain.GrantTarget{}, nil, domain.ErrRoleArchived
	}
	target, err := domain.ValidateRoleBoundary(row.Type, row.OrganizationID, row.GroupID)
	if err != nil {
		return domain.RoleRecord{}, domain.GrantTarget{}, nil, err
	}
	current, err := w.store.PermissionCodes(ctx, row.ID)
	if err != nil {
		return domain.RoleRecord{}, domain.GrantTarget{}, nil, err
	}
	return row, target, current, nil
}

// requireRegistered menolak kode yang tidak ada di tabel permissions (domain.ErrUnknownPermission).
func (w *RoleWriter) requireRegistered(ctx context.Context, codes []string) error {
	missing, err := w.store.MissingPermissions(ctx, codes)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: %v", domain.ErrUnknownPermission, capCodes(missing))
	}
	return nil
}

// checkCeiling menghitung himpunan pemberi dari PostgreSQL di dalam transaksi (BUKAN cache) lalu
// menerapkan domain.CheckCeiling.
func (w *RoleWriter) checkCeiling(ctx context.Context, g Grantor, target domain.GrantTarget, codes []string) error {
	eff, err := w.store.GrantorEffective(ctx, g.UserID, g.Scope)
	if err != nil {
		return fmt.Errorf("authorization: himpunan pemberi: %w", err)
	}
	return domain.CheckCeiling(eff, g.Scope, target, codes)
}

func (w *RoleWriter) invalidate(ctx context.Context, org uuid.UUID) error {
	if w.inv == nil {
		return nil
	}
	return w.inv.InvalidateInTx(ctx, cache.Targets{Organizations: []uuid.UUID{org}})
}

// actorContext membawa aktor dan organisasi dari argumen yang SAMA yang dipakai untuk
// authority ceiling, sehingga baris audit selalu konsisten dengan keputusan otorisasinya.
func actorContext(ctx context.Context, g Grantor, org uuid.UUID) context.Context {
	return kernel.WithActor(ctx, kernel.Actor{UserID: g.UserID, Type: kernel.ActorUser, OrganizationID: org})
}

func (w *RoleWriter) recordAudit(ctx context.Context, g Grantor, org uuid.UUID, action string, roleID uuid.UUID, meta map[string]any) {
	w.audit.Record(actorContext(ctx, g, org), modulesdk.ActivityEntry{
		Action:       action,
		ResourceType: "role",
		ResourceID:   roleID,
		Metadata:     meta,
	})
}

// recordDenial mencatat penolakan kebijakan (ceiling, role sistem, target di luar scope) sebagai
// activity result=denied di transaksi terpisah, supaya tetap tercatat walau transaksi utama
// dibatalkan. Dipanggil di luar transaksi. Galat lain (validasi, tidak ditemukan, infrastruktur)
// tidak dicatat di sini.
func (w *RoleWriter) recordDenial(ctx context.Context, g Grantor, org uuid.UUID, action string, roleID uuid.UUID, err error) {
	var reason string
	switch {
	case errors.Is(err, domain.ErrAuthorityCeiling):
		reason = "authority_ceiling"
	case errors.Is(err, domain.ErrSystemRoleImmutable):
		reason = "system_role_immutable"
	case errors.Is(err, domain.ErrTargetOutsideScope):
		reason = "target_outside_scope"
	default:
		return
	}
	w.audit.Record(actorContext(ctx, g, org), modulesdk.ActivityEntry{
		Action:       action,
		ResourceType: "role",
		ResourceID:   roleID,
		Result:       modulesdk.ResultDenied,
		Metadata:     map[string]any{"reason": reason},
	})
}

func (w *RoleWriter) publish(ctx context.Context, name string, g Grantor, org uuid.UUID, role domain.RoleRecord, extra map[string]any) error {
	payload := map[string]any{"role_id": role.ID.String(), "slug": role.Slug}
	payload["actor_user_id"] = g.UserID.String()
	for k, v := range extra {
		payload[k] = v
	}
	ev, err := modulesdk.NewEvent(name, payload)
	if err != nil {
		return err
	}
	ev.OrganizationID = org
	ev.AggregateType = "role"
	ev.AggregateID = role.ID
	w.events.Publish(ctx, ev)
	return nil
}

// diffCodes mengembalikan kode di a yang tidak ada di b, terurut.
func diffCodes(a, b []string) []string {
	have := make(map[string]struct{}, len(b))
	for _, c := range b {
		have[c] = struct{}{}
	}
	out := []string{}
	for _, c := range a {
		if _, ok := have[c]; !ok {
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return out
}

// unionCodes mengembalikan gabungan a dan b tanpa duplikat, terurut.
func unionCodes(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, list := range [][]string{a, b} {
		for _, c := range list {
			if _, dup := seen[c]; dup {
				continue
			}
			seen[c] = struct{}{}
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return out
}

// capCodes memotong daftar kode untuk metadata audit dan pesan galat.
func capCodes(codes []string) []string {
	if len(codes) <= maxAuditCodes {
		return codes
	}
	return codes[:maxAuditCodes]
}
