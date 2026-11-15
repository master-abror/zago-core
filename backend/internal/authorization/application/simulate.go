package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/permission"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

// OrganizationMemberChecker memverifikasi bahwa user adalah user aktif dan anggota AKTIF
// organisasi. (false, nil) = terbukti bukan; galat = tidak dapat diverifikasi.
// repository.Members memenuhinya.
type OrganizationMemberChecker interface {
	IsActiveMember(ctx context.Context, organizationID, userID uuid.UUID) (bool, error)
}

// AuditActionSimulate adalah nama activity audit satu simulasi (ADR-0022 butir 69).
const AuditActionSimulate = "permission.simulate"

// SimulateResource menggambarkan sumber daya hipotetis yang ditanyakan. Organisasinya TIDAK ada
// di sini: selalu organisasi pemanggil.
type SimulateResource struct {
	Type    string
	ID      uuid.UUID
	OwnerID *uuid.UUID
	GroupID *uuid.UUID
	Attrs   map[string]any
}

// SimulateInput adalah satu pertanyaan simulasi: bolehkah UserID melakukan Action (pada Resource,
// bila ada) dengan ActiveGroupID sebagai grup aktifnya.
type SimulateInput struct {
	UserID        uuid.UUID
	Action        string
	ActiveGroupID *uuid.UUID
	Resource      *SimulateResource
}

// SimulationResult adalah jawaban ringkas simulasi. Reason berupa kode stabil
// (domain.Reason*). Level kosong untuk keputusan yang ditolak. PolicyCode dan DeferredPolicies
// hanya terisi bila pemanggil sendiri berhak membaca policy (policy.read pada scope-nya).
type SimulationResult struct {
	Allowed          bool
	Reason           string
	Level            domain.DecisionLevel
	PolicyCode       string
	DeferredPolicies []string
}

// Simulator menjawab "apa yang akan diputuskan engine untuk user ini?" (docs/06 §10-§12) dengan
// memanggil Authorizer.Can; tidak ada implementasi paralel (ADR-0022 butir 69). Simulator TIDAK
// memeriksa permission.simulate pemanggil: itu tugas middleware Authorize di endpoint.
type Simulator struct {
	decider Decider
	members OrganizationMemberChecker
	groups  GroupMembershipChecker
	audit   modulesdk.AuditRecorder
}

// NewSimulator merakit Simulator; semua argumen wajib.
func NewSimulator(decider Decider, members OrganizationMemberChecker, groups GroupMembershipChecker, audit modulesdk.AuditRecorder) (*Simulator, error) {
	if decider == nil || members == nil || groups == nil || audit == nil {
		return nil, errors.New("authorization: Simulator butuh decider, members, groups, dan audit")
	}
	return &Simulator{decider: decider, members: members, groups: groups, audit: audit}, nil
}

// Simulate menjawab satu pertanyaan simulasi untuk pemanggil g. Pemanggil di scope platform
// ditolak (ErrTargetOutsideScope): masuk dulu lewat switch-organization, sejalan dengan butir 48.
// Target harus anggota aktif organisasi pemanggil; selain itu galat seragam
// ErrSimulationTargetNotFound (tidak ada, bukan anggota, tidak aktif, organisasi lain). Pemanggil
// level group hanya menyimulasikan anggota aktif grup aktifnya sendiri, dengan grup itu sebagai
// grup aktif simulasi; grup lain yang diminta ditolak (ErrTargetOutsideScope). Pemanggil level
// organisasi boleh meminta grup aktif; grup yang bukan keanggotaan aktif target DIBUANG seperti
// header X-Active-Group-ID di produksi (scope = organisasi saja). Identity simulasi adalah target
// di organisasi pemanggil, dan organisasi sumber daya selalu organisasi pemanggil.
func (s *Simulator) Simulate(ctx context.Context, g Grantor, in SimulateInput) (SimulationResult, error) {
	org, tenant := g.Scope.Tenant()
	if !tenant {
		return SimulationResult{}, domain.ErrTargetOutsideScope
	}
	if g.UserID == uuid.Nil {
		return SimulationResult{}, fmt.Errorf("%w: pemanggil kosong", ErrInvalidCheck)
	}
	if _, _, err := permission.Split(in.Action); err != nil {
		return SimulationResult{}, fmt.Errorf("%w: aksi simulasi", ErrInvalidCheck)
	}
	ref, err := simulationResource(in.Resource, org)
	if err != nil {
		return SimulationResult{}, err
	}

	callerGroup, callerHasGroup := g.Scope.Group()
	wantGroup := in.ActiveGroupID
	if callerHasGroup {
		if wantGroup != nil && *wantGroup != callerGroup {
			return SimulationResult{}, domain.ErrTargetOutsideScope
		}
		wantGroup = &callerGroup
	}

	scope, err := s.targetScope(ctx, org, in.UserID, wantGroup, callerHasGroup)
	if err != nil {
		return SimulationResult{}, err
	}

	check := domain.Check{
		Identity: authdomain.Identity{UserID: in.UserID, OrganizationID: &org},
		Scope:    scope,
		Action:   in.Action,
		Resource: ref,
	}
	dec, err := s.decider.Can(ctx, check)
	if err != nil {
		return SimulationResult{}, fmt.Errorf("authorization: simulasi: %w", err)
	}

	res := SimulationResult{Allowed: dec.Allowed, Reason: dec.Reason, Level: dec.Level}
	if dec.PolicyCode != "" || len(dec.DeferredPolicies) > 0 {
		visible, err := s.callerMayReadPolicies(ctx, g, org)
		if err != nil {
			return SimulationResult{}, err
		}
		if visible {
			res.PolicyCode = dec.PolicyCode
			res.DeferredPolicies = append([]string(nil), dec.DeferredPolicies...)
		}
	}

	s.recordAudit(ctx, g, org, in, scope, ref, dec)
	return res, nil
}

// targetScope memverifikasi target dan membentuk scope simulasi. Urutan: keanggotaan organisasi
// dulu (galat seragam), baru grup.
func (s *Simulator) targetScope(ctx context.Context, org, target uuid.UUID, group *uuid.UUID, groupIsCallers bool) (domain.ScopeContext, error) {
	member, err := s.members.IsActiveMember(ctx, org, target)
	if err != nil {
		return domain.ScopeContext{}, fmt.Errorf("authorization: simulasi: periksa anggota: %w", err)
	}
	if !member {
		return domain.ScopeContext{}, domain.ErrSimulationTargetNotFound
	}
	var verified *uuid.UUID
	if group != nil {
		inGroup, err := s.groups.IsActiveGroupMember(ctx, target, org, *group)
		if err != nil {
			return domain.ScopeContext{}, fmt.Errorf("authorization: simulasi: periksa grup: %w", err)
		}
		switch {
		case inGroup:
			verified = group
		case groupIsCallers:
			return domain.ScopeContext{}, domain.ErrSimulationTargetNotFound
		}
	}
	return domain.NewScope(&org, verified)
}

// callerMayReadPolicies menanyakan engine apakah pemanggil sendiri boleh membaca policy pada
// scope-nya. Nama dan kode policy tidak dibuka bagi pemegang permission.simulate saja.
func (s *Simulator) callerMayReadPolicies(ctx context.Context, g Grantor, org uuid.UUID) (bool, error) {
	dec, err := s.decider.Can(ctx, domain.Check{
		Identity: authdomain.Identity{UserID: g.UserID, OrganizationID: &org},
		Scope:    g.Scope,
		Action:   permission.PolicyRead,
	})
	if err != nil {
		return false, fmt.Errorf("authorization: simulasi: periksa policy.read pemanggil: %w", err)
	}
	return dec.Allowed, nil
}

func (s *Simulator) recordAudit(ctx context.Context, g Grantor, org uuid.UUID, in SimulateInput, scope domain.ScopeContext, ref *domain.ResourceRef, dec domain.Decision) {
	result := "deny"
	if dec.Allowed {
		result = "allow"
	}
	meta := map[string]any{"action": in.Action, "result": result, "reason": dec.Reason}
	if dec.Level != "" {
		meta["level"] = string(dec.Level)
	}
	if dec.PolicyCode != "" {
		meta["policy_code"] = dec.PolicyCode
	}
	if gid, ok := scope.Group(); ok {
		meta["active_group_id"] = gid.String()
	}
	if ref != nil {
		meta["resource_type"] = ref.Type
	}
	s.audit.Record(actorContext(ctx, g, org), modulesdk.ActivityEntry{
		Action:       AuditActionSimulate,
		ResourceType: "user",
		ResourceID:   in.UserID,
		Metadata:     meta,
	})
}

// simulationResource membentuk ResourceRef dari masukan. Organisasi selalu org (pemanggil);
// atribut dinormalisasi (salinan) dan tidak pernah memuat kunci inti.
func simulationResource(r *SimulateResource, org uuid.UUID) (*domain.ResourceRef, error) {
	if r == nil {
		return nil, nil
	}
	if r.Type == "" || r.ID == uuid.Nil {
		return nil, fmt.Errorf("%w: sumber daya simulasi", ErrInvalidCheck)
	}
	attrs, err := domain.NormalizeSimulationAttrs(r.Attrs)
	if err != nil {
		return nil, err
	}
	orgID := org
	return &domain.ResourceRef{
		Type:           r.Type,
		ID:             r.ID,
		OrganizationID: &orgID,
		OwnerID:        copyID(r.OwnerID),
		GroupID:        copyID(r.GroupID),
		Attrs:          attrs,
	}, nil
}

func copyID(p *uuid.UUID) *uuid.UUID {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
