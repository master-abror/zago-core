package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/authorization/cache"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/pkg/id"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

// AssignmentStore adalah jalur tulis assignment; dipenuhi repository.Assignments. Semua metode
// berjalan pada transaksi context. Kunci advisory (LockOrganization/LockPlatform) HARUS menjadi
// panggilan pertama transaksi (ADR-0022 butir 54).
type AssignmentStore interface {
	LockOrganization(ctx context.Context, organizationID uuid.UUID) error
	LockPlatform(ctx context.Context) error
	Role(ctx context.Context, roleID uuid.UUID, organizationID *uuid.UUID) (domain.RoleRecord, error)
	PermissionCodes(ctx context.Context, roleID uuid.UUID) ([]string, error)
	GroupActive(ctx context.Context, organizationID, groupID uuid.UUID) (bool, error)
	UserActive(ctx context.Context, userID uuid.UUID) (bool, error)
	OrganizationMember(ctx context.Context, organizationID, userID uuid.UUID) (bool, error)
	OrganizationActive(ctx context.Context, organizationID uuid.UUID) (bool, error)
	ActiveDuplicate(ctx context.Context, roleID uuid.UUID, subjectType string, subjectID uuid.UUID, scope domain.GrantTarget) (bool, error)
	Insert(ctx context.Context, rec domain.AssignmentRecord) (domain.AssignmentRecord, error)
	ForUpdate(ctx context.Context, assignmentID uuid.UUID, organizationID *uuid.UUID) (domain.AssignmentRecord, error)
	Revoke(ctx context.Context, assignmentID uuid.UUID) error
	ActiveSuperAdmins(ctx context.Context) (int, error)
	GrantorEffective(ctx context.Context, userID uuid.UUID, scope domain.ScopeContext) (domain.EffectivePermissions, error)
}

// CreateAssignmentInput adalah masukan pembuatan assignment. ExpiresAt nil = tanpa masa berlaku
// (wajib nil pada scope platform).
type CreateAssignmentInput struct {
	RoleID      uuid.UUID
	SubjectType string
	SubjectID   uuid.UUID
	Scope       domain.GrantTarget
	ExpiresAt   *time.Time
}

// AssignmentResult adalah assignment yang tersimpan beserta role yang dirujuknya.
type AssignmentResult struct {
	Assignment domain.AssignmentRecord
	Role       domain.RoleRecord
}

// AssignmentWriter membuat dan mencabut assignment. Per operasi satu transaksi: kunci advisory
// (organisasi untuk grantor tenant, platform untuk grantor platform) sebagai langkah PERTAMA,
// lalu aturan, authority ceiling atas himpunan pemberi yang dihitung dari PostgreSQL di dalam
// transaksi (bukan cache), verifikasi subjek/grup di database, tulis, invalidasi cache, audit,
// dan event (ADR-0022 butir 49 dan 54). Penolakan kebijakan diaudit result=denied di transaksi
// terpisah.
type AssignmentWriter struct {
	tx     TxRunner
	store  AssignmentStore
	audit  modulesdk.AuditRecorder
	events EventPublisher
	inv    TxInvalidator
}

// NewAssignmentWriter merakit AssignmentWriter. inv boleh nil (tanpa cache); lainnya wajib.
func NewAssignmentWriter(tx TxRunner, store AssignmentStore, audit modulesdk.AuditRecorder, events EventPublisher, inv TxInvalidator) (*AssignmentWriter, error) {
	if tx == nil || store == nil || audit == nil || events == nil {
		return nil, errors.New("authorization: AssignmentWriter butuh tx, store, audit, dan events")
	}
	return &AssignmentWriter{tx: tx, store: store, audit: audit, events: events, inv: inv}, nil
}

// Create membuat assignment. Target harus berada di dalam scope pemberi: grantor tenant hanya di
// organisasinya (organisasi atau grup di dalamnya), grantor platform hanya di scope platform
// (Super Admin tidak memberi di organisasi dari scope platform: masuk dulu lewat
// switch-organization). Pemberi hanya boleh memberi permission yang ia pegang sendiri pada
// target itu (authority ceiling).
func (w *AssignmentWriter) Create(ctx context.Context, g Grantor, in CreateAssignmentInput) (AssignmentResult, error) {
	if err := in.Scope.Validate(); err != nil {
		return AssignmentResult{}, err
	}
	org, tenant := g.Scope.Tenant()
	var res AssignmentResult
	err := w.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := w.lock(ctx, org, tenant); err != nil {
			return err
		}
		if err := domain.CheckCeiling(domain.EffectivePermissions{}, g.Scope, in.Scope, nil); err != nil {
			return err
		}
		role, err := w.store.Role(ctx, in.RoleID, tenantOrNil(org, tenant))
		if err != nil {
			return err
		}
		draft := domain.AssignmentDraft{Role: role.Ref(), SubjectType: in.SubjectType, SubjectID: in.SubjectID, Scope: in.Scope, ExpiresAt: in.ExpiresAt}
		if err := domain.ValidateAssignment(draft, time.Now()); err != nil {
			return err
		}
		codes, err := w.store.PermissionCodes(ctx, role.ID)
		if err != nil {
			return err
		}
		if err := w.checkCeiling(ctx, g, in.Scope, codes); err != nil {
			return err
		}
		if err := w.verifySubject(ctx, in); err != nil {
			return err
		}
		exists, err := w.store.ActiveDuplicate(ctx, role.ID, in.SubjectType, in.SubjectID, in.Scope)
		if err != nil {
			return err
		}
		if exists {
			return domain.ErrAssignmentExists
		}
		rec := domain.AssignmentRecord{ID: id.NewID(), RoleID: role.ID, SubjectType: in.SubjectType, SubjectID: in.SubjectID, Scope: in.Scope, AssignedBy: &g.UserID, ExpiresAt: in.ExpiresAt}
		stored, err := w.store.Insert(ctx, rec)
		if err != nil {
			return err
		}
		if err := w.invalidate(ctx, stored.Scope); err != nil {
			return err
		}
		w.recordAudit(ctx, g, org, "role_assignment.create", stored.ID, assignmentMeta(role, stored))
		if err := w.publish(ctx, "authz.role_assignment.created", g, org, role, stored); err != nil {
			return err
		}
		res = AssignmentResult{Assignment: stored, Role: role}
		return nil
	})
	if err != nil {
		w.recordDenial(ctx, g, org, "role_assignment.create", uuid.Nil, err)
		return AssignmentResult{}, err
	}
	return res, nil
}

// Revoke mencabut assignment milik scope pemanggil: grantor tenant hanya melihat assignment
// organisasinya, grantor platform hanya assignment platform (yang lain tidak ditemukan).
// Authority ceiling berlaku atas permission role itu pada scope assignment-nya. Assignment
// super-admin platform terakhir yang aktif tidak boleh dicabut; hitungannya diambil di bawah
// kunci platform. Assignment yang sudah dicabut memberi domain.ErrAssignmentRevoked.
func (w *AssignmentWriter) Revoke(ctx context.Context, g Grantor, assignmentID uuid.UUID) error {
	org, tenant := g.Scope.Tenant()
	err := w.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := w.lock(ctx, org, tenant); err != nil {
			return err
		}
		rec, err := w.store.ForUpdate(ctx, assignmentID, tenantOrNil(org, tenant))
		if err != nil {
			return err
		}
		if rec.RevokedAt != nil {
			return domain.ErrAssignmentRevoked
		}
		role, err := w.store.Role(ctx, rec.RoleID, tenantOrNil(org, tenant))
		if err != nil {
			return err
		}
		codes, err := w.store.PermissionCodes(ctx, role.ID)
		if err != nil {
			return err
		}
		if err := w.checkCeiling(ctx, g, rec.Scope, codes); err != nil {
			return err
		}
		superAdmins := 0
		if rec.Scope.Level == domain.LevelPlatform {
			if superAdmins, err = w.store.ActiveSuperAdmins(ctx); err != nil {
				return err
			}
		}
		if err := domain.CheckRevocation(rec.Revocation(role.Ref(), time.Now()), superAdmins); err != nil {
			return err
		}
		if err := w.store.Revoke(ctx, rec.ID); err != nil {
			return err
		}
		if err := w.invalidate(ctx, rec.Scope); err != nil {
			return err
		}
		w.recordAudit(ctx, g, org, "role_assignment.revoke", rec.ID, assignmentMeta(role, rec))
		return w.publish(ctx, "authz.role_assignment.revoked", g, org, role, rec)
	})
	if err != nil {
		w.recordDenial(ctx, g, org, "role_assignment.revoke", assignmentID, err)
		return err
	}
	return nil
}

// lock mengambil kunci advisory sebagai langkah pertama transaksi: organisasi untuk grantor
// tenant, platform untuk grantor platform. Grantor tenant tidak pernah mengunci platform dan
// sebaliknya; perubahan assignment platform milik pemberi tenant (Super Admin yang sudah masuk
// organisasi) tidak diserialkan dengan penulis organisasi (keterbatasan tercatat di ADR-0022).
func (w *AssignmentWriter) lock(ctx context.Context, org uuid.UUID, tenant bool) error {
	if tenant {
		return w.store.LockOrganization(ctx, org)
	}
	return w.store.LockPlatform(ctx)
}

// tenantOrNil memberi pointer organisasi untuk grantor tenant dan nil untuk grantor platform.
func tenantOrNil(org uuid.UUID, tenant bool) *uuid.UUID {
	if !tenant {
		return nil
	}
	return &org
}

// verifySubject memeriksa di database bahwa subjek dan grup scope ada, aktif, dan milik
// organisasi scope itu (ADR-0022 butir 49b). Semua kegagalan subjek memberi
// domain.ErrSubjectNotFound tanpa membedakan sebabnya, dan grup scope yang tidak sah
// domain.ErrGroupNotFound.
func (w *AssignmentWriter) verifySubject(ctx context.Context, in CreateAssignmentInput) error {
	scope := in.Scope
	if scope.Level == domain.LevelGroup {
		ok, err := w.store.GroupActive(ctx, *scope.OrganizationID, *scope.GroupID)
		if err != nil {
			return err
		}
		if !ok {
			return domain.ErrGroupNotFound
		}
	}
	var ok bool
	var err error
	switch in.SubjectType {
	case domain.SubjectUser:
		if scope.Level == domain.LevelPlatform {
			ok, err = w.store.UserActive(ctx, in.SubjectID)
		} else {
			ok, err = w.store.OrganizationMember(ctx, *scope.OrganizationID, in.SubjectID)
		}
	case domain.SubjectGroup:
		ok, err = w.store.GroupActive(ctx, *scope.OrganizationID, in.SubjectID)
	case domain.SubjectOrganization:
		ok, err = w.store.OrganizationActive(ctx, in.SubjectID)
	default:
		return domain.ErrAssignmentSubject
	}
	if err != nil {
		return err
	}
	if !ok {
		return domain.ErrSubjectNotFound
	}
	return nil
}

// checkCeiling menghitung himpunan pemberi dari PostgreSQL di dalam transaksi (BUKAN cache) lalu
// menerapkan domain.CheckCeiling.
func (w *AssignmentWriter) checkCeiling(ctx context.Context, g Grantor, target domain.GrantTarget, codes []string) error {
	eff, err := w.store.GrantorEffective(ctx, g.UserID, g.Scope)
	if err != nil {
		return fmt.Errorf("authorization: himpunan pemberi: %w", err)
	}
	return domain.CheckCeiling(eff, g.Scope, target, codes)
}

// invalidate menaikkan versi cache di dalam transaksi: platform untuk scope platform,
// organisasi scope itu untuk selainnya. Kegagalan membatalkan transaksi.
func (w *AssignmentWriter) invalidate(ctx context.Context, scope domain.GrantTarget) error {
	if w.inv == nil {
		return nil
	}
	if scope.Level == domain.LevelPlatform {
		return w.inv.InvalidateInTx(ctx, cache.Targets{Platform: true})
	}
	return w.inv.InvalidateInTx(ctx, cache.Targets{Organizations: []uuid.UUID{*scope.OrganizationID}})
}

func (w *AssignmentWriter) recordAudit(ctx context.Context, g Grantor, org uuid.UUID, action string, assignmentID uuid.UUID, meta map[string]any) {
	w.audit.Record(actorContext(ctx, g, org), modulesdk.ActivityEntry{
		Action:       action,
		ResourceType: "role_assignment",
		ResourceID:   assignmentID,
		Metadata:     meta,
	})
}

// recordDenial mencatat penolakan kebijakan (ceiling, target di luar scope, role tidak cocok
// dengan scope, super-admin terakhir) sebagai activity result=denied di transaksi terpisah.
// Dipanggil di luar transaksi. Galat lain (validasi, tidak ditemukan, ganda, infrastruktur)
// tidak dicatat di sini.
func (w *AssignmentWriter) recordDenial(ctx context.Context, g Grantor, org uuid.UUID, action string, assignmentID uuid.UUID, err error) {
	var reason string
	switch {
	case errors.Is(err, domain.ErrAuthorityCeiling):
		reason = "authority_ceiling"
	case errors.Is(err, domain.ErrTargetOutsideScope):
		reason = "target_outside_scope"
	case errors.Is(err, domain.ErrAssignmentScope):
		reason = "assignment_scope"
	case errors.Is(err, domain.ErrLastSuperAdmin):
		reason = "last_super_admin"
	default:
		return
	}
	w.audit.Record(actorContext(ctx, g, org), modulesdk.ActivityEntry{
		Action:       action,
		ResourceType: "role_assignment",
		ResourceID:   assignmentID,
		Result:       modulesdk.ResultDenied,
		Metadata:     map[string]any{"reason": reason},
	})
}

func (w *AssignmentWriter) publish(ctx context.Context, name string, g Grantor, org uuid.UUID, role domain.RoleRecord, rec domain.AssignmentRecord) error {
	payload := assignmentMeta(role, rec)
	payload["assignment_id"] = rec.ID.String()
	payload["actor_user_id"] = g.UserID.String()
	ev, err := modulesdk.NewEvent(name, payload)
	if err != nil {
		return err
	}
	ev.OrganizationID = org
	ev.AggregateType = "role_assignment"
	ev.AggregateID = rec.ID
	w.events.Publish(ctx, ev)
	return nil
}

// assignmentMeta adalah metadata audit dan payload event: tidak memuat nama atau email subjek,
// hanya id dan tipe.
func assignmentMeta(role domain.RoleRecord, rec domain.AssignmentRecord) map[string]any {
	meta := map[string]any{"role_id": role.ID.String(), "role_slug": role.Slug}
	meta["subject_type"] = rec.SubjectType
	meta["subject_id"] = rec.SubjectID.String()
	meta["scope_type"] = string(rec.Scope.Level)
	if rec.Scope.GroupID != nil {
		meta["group_id"] = rec.Scope.GroupID.String()
	}
	if rec.ExpiresAt != nil {
		meta["expires_at"] = rec.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return meta
}
