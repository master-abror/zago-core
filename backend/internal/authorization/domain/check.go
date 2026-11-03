package domain

import (
	"github.com/google/uuid"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
)

// DecisionLevel adalah scope yang memberi izin (docs/06 §3). Hanya keputusan ber-level platform
// yang boleh memakai metode repositori lintas-tenant (docs/10 §13, docs/12 §6).
type DecisionLevel string

const (
	LevelPlatform     DecisionLevel = "platform"
	LevelOrganization DecisionLevel = "organization"
	LevelGroup        DecisionLevel = "group"
	LevelResource     DecisionLevel = "resource"
	LevelOwn          DecisionLevel = "own"
)

// Valid melaporkan apakah level dikenal (huruf persis).
func (l DecisionLevel) Valid() bool { return l.rank() > 0 }

// BroaderThan melaporkan apakah l lebih luas dari other: platform > organization > group >
// resource > own. Level tak dikenal adalah yang tersempit, sehingga tidak pernah menggantikan
// level sah.
func (l DecisionLevel) BroaderThan(other DecisionLevel) bool { return l.rank() > other.rank() }

func (l DecisionLevel) rank() int {
	switch l {
	case LevelPlatform:
		return 5
	case LevelOrganization:
		return 4
	case LevelGroup:
		return 3
	case LevelResource:
		return 2
	case LevelOwn:
		return 1
	default:
		return 0
	}
}

// Alasan keputusan (Decision.Reason) untuk simulator dan jejak audit. Nilainya stabil dan tidak
// memuat data pengguna.
const (
	ReasonGranted       = "granted"
	ReasonNoPermission  = "no_permission"
	ReasonScopeMismatch = "scope_mismatch"

	// ReasonPolicyDenied: permission ada, tetapi sebuah policy menolak (ADR-0022 butir 31).
	ReasonPolicyDenied = "policy_denied"

	// ReasonPolicyEvaluationFailed: sebuah policy tidak dapat dievaluasi; ditolak gagal-tertutup.
	ReasonPolicyEvaluationFailed = "policy_evaluation_failed"
)

// ResourceRef menunjuk sumber daya yang sedang diperiksa (docs/06 §3). OrganizationID milik
// sumber daya SELALU dibandingkan dengan scope.
type ResourceRef struct {
	Type           string
	ID             uuid.UUID
	OrganizationID *uuid.UUID
	OwnerID        *uuid.UUID
	GroupID        *uuid.UUID
	Attrs          map[string]any
}

// Check adalah satu pertanyaan otorisasi, eksplisit tanpa apa pun yang implisit (docs/06 §3).
// Resource nil untuk aksi bertipe list/create.
type Check struct {
	Identity authdomain.Identity
	Scope    ScopeContext
	Action   string
	Resource *ResourceRef
}

// Decision adalah jawaban engine. Allowed=false adalah keadaan nol sehingga keputusan yang tidak
// diisi selalu berarti ditolak.
type Decision struct {
	Allowed bool
	Reason  string
	Level   DecisionLevel

	// PolicyCode adalah kode policy yang menolak; terisi hanya bila Reason adalah ReasonPolicy*.
	// Kode ditulis admin, bukan data pengguna, sehingga aman masuk ke jejak audit.
	PolicyCode string

	// DeferredPolicies adalah kode policy yang butuh sumber daya tetapi Check.Resource nil; policy
	// itu WAJIB dievaluasi oleh pengecekan tingkat sumber daya (ADR-0022 butir 32).
	DeferredPolicies []string
}
