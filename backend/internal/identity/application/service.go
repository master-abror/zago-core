// Package application memuat layanan identitas (docs/02 §7, docs/08 §10.8-§10.9, ADR-0023 butir
// 21-26): profil milik pemanggil dan pengelolaan user. Setiap perubahan berjalan dalam satu
// transaksi bersama audit dan event outbox, dan hanya mencatat NAMA bidang yang berubah, tidak
// pernah nilainya (data pribadi). Pemeriksaan permission aksi adalah tugas middleware Authorize;
// layanan menegakkan batas tenant dan aturan siapa boleh mengubah siapa.
package application

import (
	"context"
	"errors"

	"github.com/google/uuid"

	authzdomain "github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/identity/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	orgdomain "github.com/master-abror/zago-core/backend/internal/organization/domain"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

// Nama aksi audit dan event. Aksi audit imperatif seperti M04 (role.create).
const (
	// ActionProfileUpdate adalah aksi audit pemilik data mengubah profilnya sendiri.
	ActionProfileUpdate = "profile.update"

	// ActionUserUpdate adalah aksi audit operator platform mengubah data seorang user.
	ActionUserUpdate = "user.update"

	// EventUserUpdated adalah event outbox untuk kedua aksi di atas.
	EventUserUpdated = "user.updated"

	// ResourceTypeUser adalah jenis sumber daya pada audit dan event.
	ResourceTypeUser = "user"
)

// TxRunner menjalankan fn di dalam satu transaksi database; dipenuhi *kernel.TxManager.
type TxRunner interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// EventPublisher menulis event ke outbox pada transaksi context; dipenuhi *kernel.Bus.
type EventPublisher interface {
	Publish(ctx context.Context, event modulesdk.Event)
}

// Store adalah jalur data user; dipenuhi repository.Users. Metode ber-Access membawa pembatas
// tenant ke dalam setiap kueri; metode Self melayani pemilik data atas barisnya sendiri.
type Store interface {
	Get(ctx context.Context, a orgdomain.Access, userID uuid.UUID) (domain.Record, error)
	GetSelf(ctx context.Context, userID uuid.UUID) (domain.Record, error)
	ForUpdate(ctx context.Context, a orgdomain.Access, userID uuid.UUID) (domain.Record, error)
	ForUpdateSelf(ctx context.Context, userID uuid.UUID) (domain.Record, error)
	List(ctx context.Context, a orgdomain.Access, q domain.ListQuery) (domain.Page, error)
	Update(ctx context.Context, a orgdomain.Access, userID uuid.UUID, in domain.UpdateInput) (domain.Record, error)
	UpdateSelf(ctx context.Context, userID uuid.UUID, in domain.UpdateInput) (domain.Record, error)
}

// Caller adalah pemanggil layanan: user yang bertindak dan ScopeContext request-nya (dari
// middleware Scope). ScopeContext nol berarti scope platform, jadi hanya middleware Scope yang
// boleh membentuknya.
type Caller struct {
	// UserID adalah user yang bertindak; wajib. Untuk operasi profil ia juga sasaran operasi
	// dan tidak pernah berasal dari path atau body.
	UserID uuid.UUID

	// Scope adalah scope request yang sudah diverifikasi.
	Scope authzdomain.ScopeContext
}

func (c Caller) valid() error {
	if c.UserID == uuid.Nil {
		return orgdomain.ErrInvalidAccess
	}
	return nil
}

func (c Caller) access() (orgdomain.Access, error) {
	if err := c.valid(); err != nil {
		return orgdomain.Access{}, err
	}
	if org, ok := c.Scope.Tenant(); ok {
		return orgdomain.TenantAccess(org)
	}
	return orgdomain.PlatformAccess(), nil
}

// scopeOrganization adalah organisasi scope pemanggil, atau uuid.Nil untuk scope platform. Audit
// dan outbox menyimpannya sebagai organization_id yang boleh NULL.
func (c Caller) scopeOrganization() uuid.UUID {
	if org, ok := c.Scope.Tenant(); ok {
		return org
	}
	return uuid.Nil
}

// Service adalah layanan identitas. Tidak ada kunci advisory: tidak ada role, assignment, atau
// policy yang berubah di sini, sehingga hak efektif tidak terpengaruh (ADR-0023 butir 25). Dua
// perubahan serentak pada user yang sama diserialkan oleh kunci baris, dan yang terakhir menang.
type Service struct {
	tx     TxRunner
	store  Store
	audit  modulesdk.AuditRecorder
	events EventPublisher
}

// NewService merakit Service; semua dependensi wajib.
func NewService(tx TxRunner, store Store, audit modulesdk.AuditRecorder, events EventPublisher) (*Service, error) {
	if tx == nil || store == nil || audit == nil || events == nil {
		return nil, errors.New("identity: Service butuh tx, store, audit, dan events")
	}
	return &Service{tx: tx, store: store, audit: audit, events: events}, nil
}

// GetProfile membaca baris milik pemanggil sendiri.
func (s *Service) GetProfile(ctx context.Context, c Caller) (domain.Record, error) {
	if err := c.valid(); err != nil {
		return domain.Record{}, err
	}
	return s.store.GetSelf(ctx, c.UserID)
}

// UpdateProfile mengubah profil pemanggil sendiri. Pembaruan yang tidak mengubah nilai apa pun
// mengembalikan keadaan sekarang tanpa menulis, mencatat audit, atau menerbitkan event.
func (s *Service) UpdateProfile(ctx context.Context, c Caller, in domain.UpdateInput) (domain.Record, error) {
	if err := c.valid(); err != nil {
		return domain.Record{}, err
	}
	norm, err := in.Normalize()
	if err != nil {
		return domain.Record{}, err
	}
	var out domain.Record
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		before, err := s.store.ForUpdateSelf(ctx, c.UserID)
		if err != nil {
			return err
		}
		changed := before.ChangedFields(norm)
		if len(changed) == 0 {
			out = before
			return nil
		}
		after, err := s.store.UpdateSelf(ctx, c.UserID, norm)
		if err != nil {
			return err
		}
		out = after
		return s.recordChange(ctx, c, c.UserID, ActionProfileUpdate, changed)
	})
	if err != nil {
		return domain.Record{}, err
	}
	return out, nil
}

// GetUser membaca satu user yang terlihat bagi pemanggil. User organisasi lain, user yang tidak
// ada, dan user soft-deleted semuanya menjadi domain.ErrUserNotFound.
func (s *Service) GetUser(ctx context.Context, c Caller, userID uuid.UUID) (domain.Record, error) {
	a, err := c.access()
	if err != nil {
		return domain.Record{}, err
	}
	return s.store.Get(ctx, a, userID)
}

// ListUsers mengembalikan satu halaman user yang terlihat bagi pemanggil: semua untuk scope
// platform, hanya anggota organisasi scope untuk pemanggil tenant.
func (s *Service) ListUsers(ctx context.Context, c Caller, q domain.ListQuery) (domain.Page, error) {
	a, err := c.access()
	if err != nil {
		return domain.Page{}, err
	}
	return s.store.List(ctx, a, q)
}

// UpdateUser mengubah data seorang user. Hanya scope platform: pemanggil tenant mendapat
// orgdomain.ErrPlatformRequired SEBELUM transaksi dimulai dan sebelum store disentuh, karena satu
// user dapat beranggota di banyak organisasi dan perubahannya terlihat oleh semuanya (ADR-0023
// butir 23). Pembaruan tanpa perubahan nilai tidak menulis, mencatat audit, atau menerbitkan event.
func (s *Service) UpdateUser(ctx context.Context, c Caller, userID uuid.UUID, in domain.UpdateInput) (domain.Record, error) {
	a, err := c.access()
	if err != nil {
		return domain.Record{}, err
	}
	if !a.IsPlatform() {
		return domain.Record{}, orgdomain.ErrPlatformRequired
	}
	norm, err := in.Normalize()
	if err != nil {
		return domain.Record{}, err
	}
	var out domain.Record
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		before, err := s.store.ForUpdate(ctx, a, userID)
		if err != nil {
			return err
		}
		changed := before.ChangedFields(norm)
		if len(changed) == 0 {
			out = before
			return nil
		}
		after, err := s.store.Update(ctx, a, userID, norm)
		if err != nil {
			return err
		}
		out = after
		return s.recordChange(ctx, c, userID, ActionUserUpdate, changed)
	})
	if err != nil {
		return domain.Record{}, err
	}
	return out, nil
}

// recordChange mencatat audit lalu menulis event outbox pada transaksi context. Yang tersimpan
// hanya nama bidang yang berubah; nilai (mis. nomor telepon) tidak pernah masuk audit atau event.
func (s *Service) recordChange(ctx context.Context, c Caller, userID uuid.UUID, action string, changed []string) error {
	orgID := c.scopeOrganization()
	actor := kernel.Actor{UserID: c.UserID, Type: kernel.ActorUser, OrganizationID: orgID}

	entry := modulesdk.ActivityEntry{}
	entry.Action = action
	entry.ResourceType = ResourceTypeUser
	entry.ResourceID = userID
	entry.Metadata = map[string]any{"changed_fields": changed}
	s.audit.Record(kernel.WithActor(ctx, actor), entry)

	payload := map[string]any{}
	payload["user_id"] = userID.String()
	payload["actor_user_id"] = c.UserID.String()
	payload["changed_fields"] = changed
	ev, err := modulesdk.NewEvent(EventUserUpdated, payload)
	if err != nil {
		return err
	}
	ev.OrganizationID = orgID
	ev.AggregateType = ResourceTypeUser
	ev.AggregateID = userID
	s.events.Publish(ctx, ev)
	return nil
}
