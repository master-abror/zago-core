// Package application memuat layanan organisasi (docs/02 §6, docs/08 §10.6). Setiap perubahan
// berjalan dalam satu transaksi bersama audit dan event outbox; organisasi pemanggil selalu
// berasal dari ScopeContext, tidak pernah dari body atau query.
package application

import (
	"context"
	"errors"
	"sort"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/authorization/cache"
	authzdomain "github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/organization/domain"
	"github.com/master-abror/zago-core/backend/pkg/id"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

// TxRunner menjalankan fn di dalam satu transaksi database; dipenuhi *kernel.TxManager.
type TxRunner interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// EventPublisher menulis event ke outbox pada transaksi context; dipenuhi *kernel.Bus.
type EventPublisher interface {
	Publish(ctx context.Context, event modulesdk.Event)
}

// TxInvalidator menaikkan versi cache otorisasi di dalam transaksi pengubah; dipenuhi
// *cache.Cache. Nil berarti tidak ada cache (tanpa Redis).
type TxInvalidator interface {
	InvalidateInTx(ctx context.Context, t cache.Targets) error
}

// Locker mengambil kunci advisory organisasi yang sama dengan penulis role, assignment, dan
// policy (ADR-0022 butir 54), sehingga perubahan status organisasi diserialkan dengan mereka.
type Locker interface {
	LockOrganization(ctx context.Context, organizationID uuid.UUID) error
}

// LockFunc mengadaptasi fungsi kunci biasa (mis. repository.LockOrganization milik authorization)
// menjadi Locker.
type LockFunc func(ctx context.Context, organizationID uuid.UUID) error

// LockOrganization memanggil fungsi yang dibungkus.
func (f LockFunc) LockOrganization(ctx context.Context, organizationID uuid.UUID) error {
	return f(ctx, organizationID)
}

// Store adalah jalur data organisasi; dipenuhi repository.Organizations. Setiap metode menerima
// domain.Access, jadi pembatas tenant ikut ke dalam setiap kueri.
type Store interface {
	Get(ctx context.Context, a domain.Access, organizationID uuid.UUID) (domain.Record, error)
	List(ctx context.Context, a domain.Access, q domain.ListQuery) (domain.Page, error)
	Insert(ctx context.Context, a domain.Access, rec domain.Record) (domain.Record, error)
	ForUpdate(ctx context.Context, a domain.Access, organizationID uuid.UUID) (domain.Record, error)
	Update(ctx context.Context, a domain.Access, organizationID uuid.UUID, in domain.UpdateInput) (domain.Record, error)
	SetStatus(ctx context.Context, a domain.Access, organizationID uuid.UUID, to string) (domain.Record, error)
}

// Caller adalah pemanggil layanan: user yang bertindak dan ScopeContext request-nya (dari
// middleware Scope). Pemeriksaan permission aksi adalah tugas middleware Authorize; layanan
// menegakkan batas tenant. ScopeContext nol berarti scope platform, jadi hanya middleware Scope
// yang boleh membentuknya.
type Caller struct {
	// UserID adalah user yang bertindak; wajib.
	UserID uuid.UUID

	// Scope adalah scope request yang sudah diverifikasi.
	Scope authzdomain.ScopeContext
}

func (c Caller) access() (domain.Access, error) {
	if c.UserID == uuid.Nil {
		return domain.Access{}, domain.ErrInvalidAccess
	}
	if org, ok := c.Scope.Tenant(); ok {
		return domain.TenantAccess(org)
	}
	return domain.PlatformAccess(), nil
}

// Service adalah layanan organisasi. Setiap perubahan berjalan dalam satu transaksi: kunci
// advisory (hanya yang memengaruhi hak efektif), baca dengan kunci baris, tulis, invalidasi
// cache, audit, dan event outbox. Kegagalan di langkah mana pun membatalkan semuanya.
type Service struct {
	tx     TxRunner
	store  Store
	locker Locker
	audit  modulesdk.AuditRecorder
	events EventPublisher
	inv    TxInvalidator
}

// NewService merakit Service. inv boleh nil (tanpa cache); lainnya wajib.
func NewService(tx TxRunner, store Store, locker Locker, audit modulesdk.AuditRecorder, events EventPublisher, inv TxInvalidator) (*Service, error) {
	if tx == nil || store == nil || locker == nil || audit == nil || events == nil {
		return nil, errors.New("organization: Service butuh tx, store, locker, audit, dan events")
	}
	return &Service{tx: tx, store: store, locker: locker, audit: audit, events: events, inv: inv}, nil
}

// Get membaca satu organisasi. Pemanggil tenant yang menunjuk organisasi lain mendapat
// domain.ErrOrganizationNotFound, sama seperti id yang tidak ada.
func (s *Service) Get(ctx context.Context, c Caller, organizationID uuid.UUID) (domain.Record, error) {
	a, err := c.access()
	if err != nil {
		return domain.Record{}, err
	}
	return s.store.Get(ctx, a, organizationID)
}

// List mengembalikan satu halaman organisasi yang terlihat bagi pemanggil: semua untuk scope
// platform, hanya organisasi scope untuk pemanggil tenant.
func (s *Service) List(ctx context.Context, c Caller, q domain.ListQuery) (domain.Page, error) {
	a, err := c.access()
	if err != nil {
		return domain.Page{}, err
	}
	return s.store.List(ctx, a, q)
}

// Create membuat organisasi baru. Hanya scope platform (domain.ErrPlatformRequired untuk
// pemanggil tenant). Organisasi baru belum punya anggota atau assignment, jadi tidak ada cache
// yang perlu diinvalidasi.
func (s *Service) Create(ctx context.Context, c Caller, in domain.CreateInput) (domain.Record, error) {
	a, err := c.access()
	if err != nil {
		return domain.Record{}, err
	}
	if !a.IsPlatform() {
		return domain.Record{}, domain.ErrPlatformRequired
	}
	rec, err := domain.NewRecord(id.NewID(), in)
	if err != nil {
		return domain.Record{}, err
	}
	var out domain.Record
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		created, err := s.store.Insert(ctx, a, rec)
		if err != nil {
			return err
		}
		out = created
		meta := map[string]any{"slug": created.Slug}
		s.recordAudit(ctx, c, created.ID, "organization.create", meta)
		return s.publish(ctx, "organization.created", c, created, meta)
	})
	if err != nil {
		return domain.Record{}, err
	}
	return out, nil
}

// Update mengubah name, legal_name, default_locale, dan default_timezone. Pembaruan yang tidak
// mengubah nilai apa pun mengembalikan keadaan sekarang tanpa menulis, mencatat audit, atau
// menerbitkan event. Organisasi terarsip menjadi domain.ErrOrganizationArchived.
func (s *Service) Update(ctx context.Context, c Caller, organizationID uuid.UUID, in domain.UpdateInput) (domain.Record, error) {
	a, err := c.access()
	if err != nil {
		return domain.Record{}, err
	}
	norm, err := in.Normalize()
	if err != nil {
		return domain.Record{}, err
	}
	var out domain.Record
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		before, err := s.store.ForUpdate(ctx, a, organizationID)
		if err != nil {
			return err
		}
		if before.ArchivedAt != nil {
			return domain.ErrOrganizationArchived
		}
		changed := changedFields(before, norm)
		if len(changed) == 0 {
			out = before
			return nil
		}
		after, err := s.store.Update(ctx, a, organizationID, norm)
		if err != nil {
			return err
		}
		out = after
		meta := map[string]any{"changed_fields": changed}
		s.recordAudit(ctx, c, after.ID, "organization.update", meta)
		return s.publish(ctx, "organization.updated", c, after, meta)
	})
	if err != nil {
		return domain.Record{}, err
	}
	return out, nil
}

// Archive mengarsipkan organisasi (DELETE /organizations/{id}); arsip bersifat akhir. Kunci
// advisory organisasi diambil sebagai langkah PERTAMA transaksi (butir 54), cache otorisasi
// dinaikkan di dalam transaksi sesudah status ditulis, dan pencabutan sesi menjadi urusan T5.
// Pemanggil tenant yang menunjuk organisasi lain ditolak SEBELUM transaksi, sehingga tidak ada
// kunci yang diambil atas organisasi milik orang lain.
func (s *Service) Archive(ctx context.Context, c Caller, organizationID uuid.UUID) (domain.Record, error) {
	a, err := c.access()
	if err != nil {
		return domain.Record{}, err
	}
	if own, ok := a.Tenant(); ok && own != organizationID {
		return domain.Record{}, domain.ErrOrganizationNotFound
	}
	var out domain.Record
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.locker.LockOrganization(ctx, organizationID); err != nil {
			return err
		}
		before, err := s.store.ForUpdate(ctx, a, organizationID)
		if err != nil {
			return err
		}
		after, err := s.store.SetStatus(ctx, a, organizationID, domain.StatusArchived)
		if err != nil {
			return err
		}
		if err := s.invalidate(ctx, organizationID); err != nil {
			return err
		}
		out = after
		meta := map[string]any{"previous_status": before.Status}
		s.recordAudit(ctx, c, after.ID, "organization.archive", meta)
		return s.publish(ctx, "organization.archived", c, after, meta)
	})
	if err != nil {
		return domain.Record{}, err
	}
	return out, nil
}

func (s *Service) invalidate(ctx context.Context, organizationID uuid.UUID) error {
	if s.inv == nil {
		return nil
	}
	return s.inv.InvalidateInTx(ctx, cache.Targets{Organizations: []uuid.UUID{organizationID}})
}

// recordAudit mencatat activity dengan organisasi sasaran sebagai organisasi aktor, supaya
// administrator organisasi itu melihatnya di log audit organisasinya sendiri.
func (s *Service) recordAudit(ctx context.Context, c Caller, organizationID uuid.UUID, action string, meta map[string]any) {
	actor := kernel.Actor{UserID: c.UserID, Type: kernel.ActorUser, OrganizationID: organizationID}
	s.audit.Record(kernel.WithActor(ctx, actor), modulesdk.ActivityEntry{
		Action:       action,
		ResourceType: "organization",
		ResourceID:   organizationID,
		Metadata:     meta,
	})
}

func (s *Service) publish(ctx context.Context, name string, c Caller, rec domain.Record, extra map[string]any) error {
	payload := map[string]any{"organization_id": rec.ID.String(), "actor_user_id": c.UserID.String()}
	for k, v := range extra {
		payload[k] = v
	}
	ev, err := modulesdk.NewEvent(name, payload)
	if err != nil {
		return err
	}
	ev.OrganizationID = rec.ID
	ev.AggregateType = "organization"
	ev.AggregateID = rec.ID
	s.events.Publish(ctx, ev)
	return nil
}

// changedFields mengembalikan nama bidang yang nilainya benar-benar berbeda dari keadaan
// sebelum, terurut menurut abjad. Hanya nama bidang yang dicatat, tidak pernah nilainya.
func changedFields(before domain.Record, in domain.UpdateInput) []string {
	out := []string{}
	if in.DefaultLocale != nil && *in.DefaultLocale != before.DefaultLocale {
		out = append(out, "default_locale")
	}
	if in.DefaultTimezone != nil && *in.DefaultTimezone != before.DefaultTimezone {
		out = append(out, "default_timezone")
	}
	if in.LegalName != nil {
		current := ""
		if before.LegalName != nil {
			current = *before.LegalName
		}
		if *in.LegalName != current {
			out = append(out, "legal_name")
		}
	}
	if in.Name != nil && *in.Name != before.Name {
		out = append(out, "name")
	}
	sort.Strings(out)
	return out
}
