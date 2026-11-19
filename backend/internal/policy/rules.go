package policy

import (
	"errors"
	"sort"
	"strings"
)

// Effect adalah efek policy (docs/03 §16). Keduanya hanya MEMBATASI permission yang sudah terbukti
// (ADR-0022 butir 31): tidak ada policy yang dapat memberi izin.
type Effect string

const (
	// EffectDeny menolak bila ekspresinya true.
	EffectDeny Effect = "deny"

	// EffectAllow adalah syarat tambahan: menolak bila ekspresinya false.
	EffectAllow Effect = "allow"
)

// MaxPoliciesPerCheck membatasi policy yang dievaluasi untuk satu check. Lebih dari itu ditolak
// (gagal-tertutup) supaya biaya satu check tetap terbatas walau jumlah policy tak dibatasi di tempat lain.
const MaxPoliciesPerCheck = 100

// Policy adalah satu policy aktif. Actions berisi kode persis atau "resource.*".
type Policy struct {
	Code       string
	Effect     Effect
	Expression string
	Actions    []string
}

// Verdict adalah hasil gabungan evaluasi policy. Nilai nol sengaja BUKAN izin.
type Verdict uint8

const (
	// VerdictFailed: galat evaluasi (atau input tak sah). Ditolak.
	VerdictFailed Verdict = iota

	// VerdictDenied: sebuah policy deny bernilai true, atau policy allow bernilai false. Ditolak.
	VerdictDenied

	// VerdictPermit: tidak ada policy yang menolak (policy yang ditunda tidak dihitung).
	VerdictPermit
)

// Outcome adalah hasil Evaluate. Code adalah policy pertama (urut kode) yang menyebabkan Denied atau
// Failed; Deferred adalah kode policy yang ditunda karena membutuhkan sumber daya yang belum dimuat.
type Outcome struct {
	Verdict  Verdict
	Code     string
	Deferred []string
}

// Permits melaporkan apakah tidak ada policy yang menolak.
func (o Outcome) Permits() bool {
	return o.Verdict == VerdictPermit
}

// ActionMatches melaporkan apakah salah satu pola cocok dengan action: kode persis, atau "resource.*"
// dengan resource = semua segmen action sebelum titik terakhir (organization.* TIDAK mencakup
// organization.member.manage; yang mencakupnya adalah organization.member.*).
func ActionMatches(patterns []string, action string) bool {
	cut := strings.LastIndexByte(action, '.')
	for _, p := range patterns {
		if p == action {
			return true
		}
		if cut > 0 && strings.HasSuffix(p, ".*") && p[:len(p)-2] == action[:cut] {
			return true
		}
	}
	return false
}

// Evaluate menerapkan ADR-0022 butir 31–32 pada policies untuk satu action:
//
//   - policy yang actions-nya tidak mencakup action diabaikan (policies boleh superset);
//   - deny true = Denied; allow false = Denied; deny dan allow tidak punya prioritas satu atas lain,
//     keduanya hanya membatasi, sehingga urutan tidak mengubah apakah hasilnya izin;
//   - galat evaluasi pada policy mana pun (ekspresi tak dapat diurai, tipe salah, atribut hilang
//     saat sumber daya ada, batas terlampaui, efek tak dikenal) = Failed;
//   - ErrNoResource (Env tanpa sumber daya) menunda policy itu ke pengecekan tingkat sumber daya;
//   - lebih dari MaxPoliciesPerCheck policy relevan = Failed.
//
// Policy dievaluasi menurut urutan kode supaya policy yang dilaporkan deterministik.
func Evaluate(policies []Policy, action string, env Env) Outcome {
	relevant := make([]Policy, 0, len(policies))
	for _, p := range policies {
		if ActionMatches(p.Actions, action) {
			relevant = append(relevant, p)
		}
	}
	if len(relevant) > MaxPoliciesPerCheck {
		return Outcome{Verdict: VerdictFailed}
	}
	sort.SliceStable(relevant, func(i, j int) bool { return relevant[i].Code < relevant[j].Code })

	out := Outcome{Verdict: VerdictPermit}
	for _, p := range relevant {
		if p.Effect != EffectDeny && p.Effect != EffectAllow {
			return Outcome{Verdict: VerdictFailed, Code: p.Code, Deferred: out.Deferred}
		}
		prog, err := Parse(p.Expression)
		if err != nil {
			return Outcome{Verdict: VerdictFailed, Code: p.Code, Deferred: out.Deferred}
		}
		ok, err := prog.Eval(env)
		if errors.Is(err, ErrNoResource) {
			out.Deferred = append(out.Deferred, p.Code)
			continue
		}
		if err != nil {
			return Outcome{Verdict: VerdictFailed, Code: p.Code, Deferred: out.Deferred}
		}
		if (p.Effect == EffectDeny && ok) || (p.Effect == EffectAllow && !ok) {
			return Outcome{Verdict: VerdictDenied, Code: p.Code, Deferred: out.Deferred}
		}
	}
	return out
}
