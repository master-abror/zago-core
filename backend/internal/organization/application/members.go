package application

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/authorization/cache"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/organization/domain"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

// MemberStore adalah jalur data keanggotaan organisasi; dipenuhi
// repository.OrganizationMembers. Setiap metode menerima domain.Access, jadi pembatas tenant
// ikut ke dalam setiap kueri.
type MemberStore interface {
	Get(ctx context.Context, a domain.Access, organizationID, userID uuid.UUID) (domain.Member, error)
	List(ctx context.Context, a domain.Access, organizationID uuid.UUID, q domain.MemberListQuery) (domain.MemberPage, error)
	Add(ctx context.Context, a domain.Access, organizationID, userID uuid.UUID, addedBy *uuid.UUID) (domain.Member, error)
	Remove(ctx context.Context, a domain.Access, organizationID, userID uuid.UUID) (domain.Member, error)
	RemoveFromGroups(ctx context.Context, a domain.Access, organizationID, userID uuid.UUID) (int, error)
}

// OrganizationReader membaca organisasi untuk memeriksa statusnya sebelum menulis keanggotaan;
// dipenuhi repository.Organizations.
type OrganizationReader interface {
	Get(ctx context.Context, a domain.Access, organizationID uuid.UUID) (domain.Record, error)
}

// AssignmentRevoker mencabut assignment user yang terikat satu organisasi; dipenuhi
// authorization/repository.Assignments (RevokeUserInOrganization, ADR-0023 butir 15).
type AssignmentRevoker interface {
	RevokeUserInOrganization(ctx context.Context, organizationID, userID uuid.UUID) (int, error)
}

// MemberService adalah layanan keanggotaan organisasi (ADR-0023 butir 11 sampai 17). Setiap
// perubahan berjalan dalam satu transaksi: kunci advisory organisasi sebagai langkah PERTAMA
// (ADR-0022 butir 54), pemeriksaan status organisasi, tulis, invalidasi cache, audit, dan event
// outbox. Kegagalan di langkah mana pun membatalkan semuanya.
type MemberService struct {
	tx TxRunner

	members MemberStore

	orgs OrganizationReader

	revoker AssignmentRevoker

	locker Locker

	audit modulesdk.AuditRecorder

	events EventPublisher

	inv TxInvalidator
}

// NewMemberService merakit MemberService. inv boleh nil (tanpa cache); lainnya wajib.
func NewMemberService(tx TxRunner, members MemberStore, orgs OrganizationReader, revoker AssignmentRevoker, locker Locker, audit modulesdk.AuditRecorder, events EventPublisher, inv TxInvalidator) (*MemberService, error) {
	if tx == nil || members == nil || orgs == nil || revoker == nil || locker == nil || audit == nil || events == nil {
		return nil, errors.New("organization: MemberService butuh tx, members, orgs, revoker, locker, audit, dan events")
	}
	return &MemberService{tx: tx, members: members, orgs: orgs, revoker: revoker, locker: locker, audit: audit, events: events, inv: inv}, nil
}

// Get membaca satu keanggotaan (status apa pun, termasuk removed). Pemanggil tenant yang
// menunjuk organisasi lain mendapat domain.ErrOrganizationNotFound.
func (s *MemberService) Get(ctx context.Context, c Caller, organizationID, userID uuid.UUID) (domain.Member, error) {
	a, err := c.access()
	if err != nil {
		return domain.Member{}, err
	}
	return s.members.Get(ctx, a, organizationID, userID)
}

// List membaca satu halaman anggota organisasi dengan cursor. Pemanggil tenant yang menunjuk
// organisasi lain mendapat domain.ErrOrganizationNotFound.
func (s *MemberService) List(ctx context.Context, c Caller, organizationID uuid.UUID, q domain.MemberListQuery) (domain.MemberPage, error) {
	a, err := c.access()
	if err != nil {
		return domain.MemberPage{}, err
	}
	return s.members.List(ctx, a, organizationID, q)
}

// Add menambahkan user sebagai anggota aktif organisasi. Hanya scope platform: pemanggil tenant
// mendapat domain.ErrPlatformRequired SEBELUM transaksi dibuka, jadi tidak ada kunci yang
// diambil dan tidak ada yang ditulis (ADR-0023 butir 11 dan 16). Organisasi yang diarsipkan
// ditolak (domain.ErrOrganizationArchived); organisasi yang ditangguhkan boleh menerima anggota
// karena hak efektifnya tetap kosong sampai diaktifkan kembali.
func (s *MemberService) Add(ctx context.Context, c Caller, organizationID, userID uuid.UUID) (domain.Member, error) {
	a, err := c.access()
	if err != nil {
		return domain.Member{}, err
	}
	if !a.IsPlatform() {
		return domain.Member{}, domain.ErrPlatformRequired
	}
	if organizationID == uuid.Nil || userID == uuid.Nil {
		return domain.Member{}, domain.ErrInvalidID
	}
	addedBy := c.UserID
	var out domain.Member
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.lockWritable(ctx, a, organizationID); err != nil {
			return err
		}
		added, err := s.members.Add(ctx, a, organizationID, userID, &addedBy)
		if err != nil {
			return err
		}
		if err := s.invalidate(ctx, organizationID); err != nil {
			return err
		}
		out = added
		meta := map[string]any{"user_id": userID.String(), "status": added.Status}
		s.recordAudit(ctx, c, added, "organization.member.add", meta)
		return s.publish(ctx, "organization.member.added", c, added, meta)
	})
	if err != nil {
		return domain.Member{}, err
	}
	return out, nil
}

// Remove menghapus keanggotaan secara logis beserta akibatnya dalam SATU transaksi (ADR-0023
// butir 13 dan 16): keanggotaan menjadi removed, keanggotaan grup user di organisasi itu menjadi
// removed, dan semua assignment user yang terikat organisasi itu dicabut, lalu cache otorisasi
// dinaikkan di dalam transaksi sebelum audit dan event. Pemanggil tenant yang menunjuk
// organisasi lain ditolak SEBELUM transaksi (tidak ada kunci milik organisasi lain yang
// diraba), dan pemanggil tenant tidak boleh menghapus dirinya sendiri (domain.ErrCannotRemoveSelf,
// butir 17). Sesi tidak dicabut di sini (T5).
func (s *MemberService) Remove(ctx context.Context, c Caller, organizationID, userID uuid.UUID) (domain.Member, error) {
	a, err := c.access()
	if err != nil {
		return domain.Member{}, err
	}
	if organizationID == uuid.Nil || userID == uuid.Nil {
		return domain.Member{}, domain.ErrInvalidID
	}
	if own, ok := a.Tenant(); ok {
		if own != organizationID {
			return domain.Member{}, domain.ErrOrganizationNotFound
		}
		if userID == c.UserID {
			return domain.Member{}, domain.ErrCannotRemoveSelf
		}
	}
	var out domain.Member
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.lockWritable(ctx, a, organizationID); err != nil {
			return err
		}
		removed, err := s.members.Remove(ctx, a, organizationID, userID)
		if err != nil {
			return err
		}
		groups, err := s.members.RemoveFromGroups(ctx, a, organizationID, userID)
		if err != nil {
			return err
		}
		revoked, err := s.revoker.RevokeUserInOrganization(ctx, organizationID, userID)
		if err != nil {
			return err
		}
		if err := s.invalidate(ctx, organizationID); err != nil {
			return err
		}
		out = removed
		meta := map[string]any{"user_id": userID.String(), "groups_removed": groups, "assignments_revoked": revoked}
		s.recordAudit(ctx, c, removed, "organization.member.remove", meta)
		return s.publish(ctx, "organization.member.removed", c, removed, meta)
	})
	if err != nil {
		return domain.Member{}, err
	}
	return out, nil
}

// lockWritable mengambil kunci advisory organisasi sebagai langkah PERTAMA transaksi lalu
// menolak organisasi yang diarsipkan. Pembacaan sesudah kunci diserialkan dengan Archive, yang
// mengambil kunci yang sama sebelum menulis status.
func (s *MemberService) lockWritable(ctx context.Context, a domain.Access, organizationID uuid.UUID) error {
	if err := s.locker.LockOrganization(ctx, organizationID); err != nil {
		return err
	}
	rec, err := s.orgs.Get(ctx, a, organizationID)
	if err != nil {
		return err
	}
	if rec.Status == domain.StatusArchived || rec.ArchivedAt != nil {
		return domain.ErrOrganizationArchived
	}
	return nil
}

func (s *MemberService) invalidate(ctx context.Context, organizationID uuid.UUID) error {
	if s.inv == nil {
		return nil
	}
	return s.inv.InvalidateInTx(ctx, cache.Targets{Organizations: []uuid.UUID{organizationID}})
}

// recordAudit mencatat activity dengan organisasi sasaran sebagai organisasi aktor, supaya
// administrator organisasi itu melihatnya di log audit organisasinya sendiri. Metadata hanya
// memuat id dan hitungan, tidak pernah email atau nama.
func (s *MemberService) recordAudit(ctx context.Context, c Caller, m domain.Member, action string, meta map[string]any) {
	actor := kernel.Actor{UserID: c.UserID, Type: kernel.ActorUser, OrganizationID: m.OrganizationID}
	s.audit.Record(kernel.WithActor(ctx, actor), modulesdk.ActivityEntry{
		Action:       action,
		ResourceType: "organization_membership",
		ResourceID:   m.ID,
		Metadata:     meta,
	})
}

func (s *MemberService) publish(ctx context.Context, name string, c Caller, m domain.Member, extra map[string]any) error {
	payload := map[string]any{"organization_id": m.OrganizationID.String(), "actor_user_id": c.UserID.String()}
	for k, v := range extra {
		payload[k] = v
	}
	ev, err := modulesdk.NewEvent(name, payload)
	if err != nil {
		return err
	}
	ev.OrganizationID = m.OrganizationID
	ev.AggregateType = "organization_membership"
	ev.AggregateID = m.ID
	s.events.Publish(ctx, ev)
	return nil
}
