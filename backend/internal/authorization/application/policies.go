package application

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/policy"
	"github.com/master-abror/zago-core/backend/pkg/id"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

// PolicyStore adalah jalur tulis policy; dipenuhi repository.Policies. Semua metode berjalan pada
// transaksi context. Kunci advisory (LockOrganization/LockPlatform) HARUS menjadi panggilan
// pertama transaksi (ADR-0022 butir 54). organizationID nil berarti cakupan global.
type PolicyStore interface {
	LockOrganization(ctx context.Context, organizationID uuid.UUID) error
	LockPlatform(ctx context.Context) error
	Catalog(ctx context.Context) (policy.ActionCatalog, error)
	ActiveCount(ctx context.Context, organizationID *uuid.UUID) (int, error)
	Insert(ctx context.Context, rec domain.PolicyRecord) (domain.PolicyRecord, error)
	ForUpdate(ctx context.Context, policyID uuid.UUID, organizationID *uuid.UUID) (domain.PolicyRecord, error)
	Update(ctx context.Context, rec domain.PolicyRecord) (domain.PolicyRecord, error)
}

// CreatePolicyInput adalah masukan pembuatan policy. Status kosong berarti active.
type CreatePolicyInput struct {
	Code        string
	Name        string
	Description string
	Effect      policy.Effect
	Expression  string
	Actions     []string
	Status      string
}

// UpdatePolicyInput adalah perubahan sebagian (PATCH): field nil tidak diubah. Actions nil berarti
// tidak diubah (irisan kosong bukan nil dan ditolak validasi). Description menunjuk string kosong
// untuk menghapus deskripsi. Kode dan cakupan tidak dapat diubah.
type UpdatePolicyInput struct {
	Name        *string
	Description *string
	Effect      *policy.Effect
	Expression  *string
	Actions     []string
	Status      *string
}

func (in UpdatePolicyInput) empty() bool {
	return in.Name == nil && in.Description == nil && in.Effect == nil && in.Expression == nil && in.Actions == nil && in.Status == nil
}

// apply menerapkan perubahan pada salinan rec dan mengembalikannya.
func (in UpdatePolicyInput) apply(rec domain.PolicyRecord) domain.PolicyRecord {
	if in.Name != nil {
		rec.Name = *in.Name
	}
	if in.Description != nil {
		rec.Description = *in.Description
	}
	if in.Effect != nil {
		rec.Effect = *in.Effect
	}
	if in.Expression != nil {
		rec.Expression = *in.Expression
	}
	if in.Actions != nil {
		rec.Actions = domain.NormalizePolicyActions(in.Actions)
	}
	if in.Status != nil {
		rec.Status = *in.Status
	}
	return rec
}

// PolicyWriter membuat dan mengubah policy. Per operasi satu transaksi: kunci advisory
// (organisasi untuk pemanggil tenant, platform untuk pemanggil platform) sebagai langkah PERTAMA,
// lalu cakupan, validasi (policy.Validate terhadap katalog yang dimuat di dalam transaksi), batas
// jumlah policy aktif (policy.CheckPolicyCount, dibaca di bawah kunci itu), tulis, audit, dan event
// (ADR-0022 butir 49, 50, 54, dan 59-61). Policy tidak di-cache (butir 31-34), jadi tidak ada
// invalidasi. Penolakan kebijakan diaudit result=denied di transaksi terpisah.
type PolicyWriter struct {
	tx     TxRunner
	store  PolicyStore
	audit  modulesdk.AuditRecorder
	events EventPublisher
}

// NewPolicyWriter merakit PolicyWriter; semua dependensi wajib.
func NewPolicyWriter(tx TxRunner, store PolicyStore, audit modulesdk.AuditRecorder, events EventPublisher) (*PolicyWriter, error) {
	if tx == nil || store == nil || audit == nil || events == nil {
		return nil, errors.New("authorization: PolicyWriter butuh tx, store, audit, dan events")
	}
	return &PolicyWriter{tx: tx, store: store, audit: audit, events: events}, nil
}

// Create membuat policy pada cakupan pemanggil: policy organisasi untuk pemanggil tenant, policy
// global (organization_id NULL) untuk pemanggil platform. Pemanggil dengan grup aktif ditolak
// (domain.ErrTargetOutsideScope): policy berlaku di seluruh organisasi, bukan hanya grup. Kode
// ganda pada cakupan yang sama memberi domain.ErrPolicyCodeTaken; policy aktif ke-51 pada cakupan
// yang sama memberi policy.ErrTooManyPolicies.
func (w *PolicyWriter) Create(ctx context.Context, g Grantor, in CreatePolicyInput) (domain.PolicyRecord, error) {
	status := in.Status
	if status == "" {
		status = domain.PolicyActive
	}
	if !domain.ValidPolicyStatus(status) {
		return domain.PolicyRecord{}, domain.ErrInvalidPolicyStatus
	}
	org, tenant := g.Scope.Tenant()
	scope := tenantOrNil(org, tenant)
	var res domain.PolicyRecord
	err := w.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := w.lock(ctx, org, tenant); err != nil {
			return err
		}
		if err := checkPolicyGrantorScope(g.Scope); err != nil {
			return err
		}
		rec := domain.PolicyRecord{ID: id.NewID(), OrganizationID: scope, Code: in.Code, Name: in.Name, Description: in.Description, Effect: in.Effect, Expression: in.Expression, Actions: domain.NormalizePolicyActions(in.Actions), Status: status}
		if err := w.validate(ctx, rec); err != nil {
			return err
		}
		if status == domain.PolicyActive {
			if err := w.checkCount(ctx, scope); err != nil {
				return err
			}
		}
		stored, err := w.store.Insert(ctx, rec)
		if err != nil {
			return err
		}
		meta := policyMeta(stored)
		w.recordAudit(ctx, g, org, "policy.create", stored.ID, meta)
		if err := w.publish(ctx, g, org, "created", stored, meta); err != nil {
			return err
		}
		res = stored
		return nil
	})
	if err != nil {
		w.recordDenial(ctx, g, org, "policy.create", uuid.Nil, err)
		return domain.PolicyRecord{}, err
	}
	return res, nil
}

// Update mengubah policy milik cakupan pemanggil (organisasi pemanggil, atau global untuk
// pemanggil platform; yang lain domain.ErrPolicyNotFound). Perubahan tanpa efek tidak menulis,
// tidak diaudit, dan tidak menerbitkan event. Isi yang berubah divalidasi penuh; perubahan yang
// hanya menonaktifkan tidak divalidasi ulang, supaya policy yang actions-nya sudah hilang dari
// katalog tetap dapat dinonaktifkan. Mengaktifkan kembali policy divalidasi penuh dan dihitung
// terhadap batas jumlah policy aktif di bawah kunci yang sama.
func (w *PolicyWriter) Update(ctx context.Context, g Grantor, policyID uuid.UUID, in UpdatePolicyInput) (domain.PolicyRecord, error) {
	if in.empty() {
		return domain.PolicyRecord{}, domain.ErrEmptyPolicyPatch
	}
	if in.Status != nil && !domain.ValidPolicyStatus(*in.Status) {
		return domain.PolicyRecord{}, domain.ErrInvalidPolicyStatus
	}
	org, tenant := g.Scope.Tenant()
	scope := tenantOrNil(org, tenant)
	var res domain.PolicyRecord
	err := w.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := w.lock(ctx, org, tenant); err != nil {
			return err
		}
		if err := checkPolicyGrantorScope(g.Scope); err != nil {
			return err
		}
		current, err := w.store.ForUpdate(ctx, policyID, scope)
		if err != nil {
			return err
		}
		next := in.apply(current)
		if next.SameContent(current) {
			res = current
			return nil
		}
		probe := next
		probe.Status = current.Status
		contentChanged := !probe.SameContent(current)
		enabling := current.Status != domain.PolicyActive && next.Status == domain.PolicyActive
		if contentChanged || enabling {
			if err := w.validate(ctx, next); err != nil {
				return err
			}
		}
		if enabling {
			if err := w.checkCount(ctx, scope); err != nil {
				return err
			}
		}
		stored, err := w.store.Update(ctx, next)
		if err != nil {
			return err
		}
		meta := policyMeta(stored)
		meta["changed"] = changedPolicyFields(current, next)
		meta["previous_status"] = current.Status
		w.recordAudit(ctx, g, org, "policy.update", stored.ID, meta)
		if err := w.publish(ctx, g, org, "updated", stored, meta); err != nil {
			return err
		}
		res = stored
		return nil
	})
	if err != nil {
		w.recordDenial(ctx, g, org, "policy.update", policyID, err)
		return domain.PolicyRecord{}, err
	}
	return res, nil
}

// lock mengambil kunci advisory sebagai langkah pertama transaksi: organisasi untuk pemanggil
// tenant, platform untuk pemanggil platform. Pemanggil tenant tidak pernah mengunci platform dan
// sebaliknya; hitungan policy global (dijaga kunci platform) dan hitungan policy organisasi
// (dijaga kunci organisasi) adalah cakupan yang terpisah.
func (w *PolicyWriter) lock(ctx context.Context, org uuid.UUID, tenant bool) error {
	if tenant {
		return w.store.LockOrganization(ctx, org)
	}
	return w.store.LockPlatform(ctx)
}

// checkPolicyGrantorScope menolak pemanggil dengan grup aktif: policy berlaku di seluruh
// organisasi, jadi menulisnya dari scope grup akan melampaui scope pemanggil.
func checkPolicyGrantorScope(s domain.ScopeContext) error {
	if _, active := s.Group(); active {
		return domain.ErrTargetOutsideScope
	}
	return nil
}

// validate memuat katalog di dalam transaksi lalu menerapkan policy.Validate dan aturan aksi
// terlindungi. Galat memuat katalog adalah galat infrastruktur, bukan penolakan.
func (w *PolicyWriter) validate(ctx context.Context, rec domain.PolicyRecord) error {
	cat, err := w.store.Catalog(ctx)
	if err != nil {
		return err
	}
	d := rec.Draft()
	if err := policy.Validate(d, cat); err != nil {
		return err
	}
	return domain.CheckPolicyActions(d.Actions)
}

// checkCount menerapkan batas policy aktif pada cakupan scope (nil = global); hitungan dibaca di
// bawah kunci advisory yang sudah diambil transaksi ini.
func (w *PolicyWriter) checkCount(ctx context.Context, scope *uuid.UUID) error {
	n, err := w.store.ActiveCount(ctx, scope)
	if err != nil {
		return err
	}
	return policy.CheckPolicyCount(n, scope == nil)
}

func (w *PolicyWriter) recordAudit(ctx context.Context, g Grantor, org uuid.UUID, action string, policyID uuid.UUID, meta map[string]any) {
	w.audit.Record(actorContext(ctx, g, org), modulesdk.ActivityEntry{
		Action:       action,
		ResourceType: "policy",
		ResourceID:   policyID,
		Metadata:     meta,
	})
}

// recordDenial mencatat penolakan kebijakan (target di luar scope, aksi pengelolaan policy yang
// dilindungi) sebagai activity result=denied di transaksi terpisah. Dipanggil di luar transaksi.
// Galat lain (validasi, tidak ditemukan, ganda, batas jumlah, infrastruktur) tidak dicatat di
// sini.
func (w *PolicyWriter) recordDenial(ctx context.Context, g Grantor, org uuid.UUID, action string, policyID uuid.UUID, err error) {
	var reason string
	switch {
	case errors.Is(err, domain.ErrTargetOutsideScope):
		reason = "target_outside_scope"
	case errors.Is(err, domain.ErrProtectedPolicyAction):
		reason = "protected_action"
	default:
		return
	}
	w.audit.Record(actorContext(ctx, g, org), modulesdk.ActivityEntry{
		Action:       action,
		ResourceType: "policy",
		ResourceID:   policyID,
		Result:       modulesdk.ResultDenied,
		Metadata:     map[string]any{"reason": reason},
	})
}

// publish menerbitkan authz.policy.changed ke outbox pada transaksi context. change adalah
// created atau updated.
func (w *PolicyWriter) publish(ctx context.Context, g Grantor, org uuid.UUID, change string, rec domain.PolicyRecord, meta map[string]any) error {
	payload := make(map[string]any, len(meta)+2)
	for k, v := range meta {
		payload[k] = v
	}
	payload["change"] = change
	payload["actor_user_id"] = g.UserID.String()
	ev, err := modulesdk.NewEvent("authz.policy.changed", payload)
	if err != nil {
		return err
	}
	ev.OrganizationID = org
	ev.AggregateType = "policy"
	ev.AggregateID = rec.ID
	w.events.Publish(ctx, ev)
	return nil
}

// policyMeta adalah metadata audit dan payload event: tanpa ekspresi, nama, dan deskripsi (isi
// ketikan operator), hanya identitas, efek, status, dan actions.
func policyMeta(rec domain.PolicyRecord) map[string]any {
	meta := map[string]any{"policy_id": rec.ID.String(), "code": rec.Code, "effect": string(rec.Effect), "status": rec.Status}
	meta["global"] = rec.Global()
	meta["actions_count"] = len(rec.Actions)
	meta["actions"] = slices.Clone(rec.Actions)
	return meta
}

// changedPolicyFields menyebut field isi yang berbeda antara cur dan next, dalam urutan tetap.
// Urutan actions tidak dihitung.
func changedPolicyFields(cur, next domain.PolicyRecord) []string {
	var out []string
	if cur.Name != next.Name {
		out = append(out, "name")
	}
	if cur.Description != next.Description {
		out = append(out, "description")
	}
	if cur.Effect != next.Effect {
		out = append(out, "effect")
	}
	if cur.Expression != next.Expression {
		out = append(out, "expression")
	}
	if !slices.Equal(domain.NormalizePolicyActions(cur.Actions), domain.NormalizePolicyActions(next.Actions)) {
		out = append(out, "actions")
	}
	if cur.Status != next.Status {
		out = append(out, "status")
	}
	return out
}
