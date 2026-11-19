package domain

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/permission"
	"github.com/master-abror/zago-core/backend/internal/policy"
)

// Status policy (kolom policies.status). Policy nonaktif tidak pernah dimuat engine.
const (
	// PolicyActive adalah policy yang dievaluasi engine dan dihitung pada batas jumlah.
	PolicyActive = "active"

	// PolicyDisabled adalah policy yang disimpan tetapi tidak dievaluasi dan tidak dihitung.
	PolicyDisabled = "disabled"
)

// protectedPolicyResource adalah resource permission yang tidak boleh menjadi sasaran policy mana
// pun: pengelolaan policy itu sendiri. Policy deny pada policy.manage akan mengunci pemiliknya
// dari mengubah atau menonaktifkan policy itu lagi; selama aksi ini tidak dapat dibatasi, pemilik
// permission policy.manage selalu dapat memulihkan keadaan.
const protectedPolicyResource = "policy"

var (
	// ErrPolicyNotFound menandai policy yang tidak ada ATAU bukan milik cakupan pemanggil
	// (organisasi pemanggil, atau global untuk pemanggil platform). Kedua keadaan sengaja tidak
	// dibedakan (tidak membocorkan keberadaan lintas tenant), seperti ErrRoleNotFound.
	ErrPolicyNotFound = errors.New("authorization: policy tidak ditemukan")

	// ErrPolicyCodeTaken menandai kode policy yang sudah dipakai pada cakupan yang sama
	// (uq_policies_org_code untuk organisasi, uq_policies_global_code untuk global).
	ErrPolicyCodeTaken = errors.New("authorization: kode policy sudah dipakai")

	// ErrInvalidPolicyStatus menandai status selain active atau disabled.
	ErrInvalidPolicyStatus = errors.New("authorization: status policy tidak sah")

	// ErrEmptyPolicyPatch menandai perubahan policy yang tidak memuat satu field pun.
	ErrEmptyPolicyPatch = errors.New("authorization: perubahan policy kosong")

	// ErrProtectedPolicyAction menandai policy yang menarget aksi pengelolaan policy (policy.* atau
	// kode ber-resource policy). Ditolak supaya policy tidak dapat mengunci pengelolanya sendiri.
	ErrProtectedPolicyAction = errors.New("authorization: aksi pengelolaan policy tidak boleh dibatasi policy")
)

// PolicyRecord adalah satu baris policies seperti dibaca dan ditulis jalur tulis policy.
// OrganizationID nil berarti policy global (berlaku di semua organisasi; hanya ditulis pemanggil
// platform).
type PolicyRecord struct {
	ID             uuid.UUID
	OrganizationID *uuid.UUID
	Code           string
	Name           string
	Description    string
	Effect         policy.Effect
	Expression     string
	Actions        []string
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Global melaporkan apakah policy berlaku di semua organisasi.
func (r PolicyRecord) Global() bool { return r.OrganizationID == nil }

// Draft mengubah record menjadi policy.Draft untuk policy.Validate. Actions disalin.
func (r PolicyRecord) Draft() policy.Draft {
	return policy.Draft{Code: r.Code, Name: r.Name, Description: r.Description, Effect: r.Effect, Expression: r.Expression, Actions: slices.Clone(r.Actions)}
}

// SameContent melaporkan apakah dua record punya isi yang sama (nama, deskripsi, efek, ekspresi,
// status, dan himpunan actions; urutan actions tidak dihitung). ID, kode, cakupan, dan waktu tidak
// dibandingkan. Dipakai untuk menolak tulis, audit, dan event yang tidak mengubah apa pun.
func (r PolicyRecord) SameContent(o PolicyRecord) bool {
	if r.Name != o.Name || r.Description != o.Description || r.Effect != o.Effect {
		return false
	}
	if r.Expression != o.Expression || r.Status != o.Status {
		return false
	}
	return slices.Equal(NormalizePolicyActions(r.Actions), NormalizePolicyActions(o.Actions))
}

// NormalizePolicyActions mengembalikan salinan actions yang terurut, supaya yang tersimpan
// deterministik. Masukan tidak diubah.
func NormalizePolicyActions(actions []string) []string {
	out := slices.Clone(actions)
	slices.Sort(out)
	return out
}

// ValidPolicyStatus melaporkan apakah s adalah PolicyActive atau PolicyDisabled.
func ValidPolicyStatus(s string) bool { return s == PolicyActive || s == PolicyDisabled }

// CheckPolicyActions menolak policy yang menarget aksi pengelolaan policy: `policy.*` atau kode
// mana pun yang resource-nya persis `policy` (policy.read, policy.manage). Kode field
// (`policy.field.x.read`) punya resource `policy.field.x` dan tidak terkena. Dipanggil SESUDAH
// policy.Validate; actions yang salah bentuk tetap ditolak di sini (gagal-tertutup).
func CheckPolicyActions(actions []string) error {
	for i, a := range actions {
		probe := a
		if base, wildcard := strings.CutSuffix(a, ".*"); wildcard {
			probe = base + ".x"
		}
		resource, _, err := permission.Split(probe)
		if err != nil {
			return fmt.Errorf("%w: actions[%d] salah bentuk", policy.ErrInvalidActions, i)
		}
		if resource == protectedPolicyResource {
			return ErrProtectedPolicyAction
		}
	}
	return nil
}
