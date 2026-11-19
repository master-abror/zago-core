package policy

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/master-abror/zago-core/backend/internal/permission"
)

// Batas tulis policy. MaxActionsPerPolicy sama dengan policy_actions_valid (migrasi 034); batas
// panjang kode dan nama mengikuti kolom policies (migrasi 015).
const (
	// MaxActionsPerPolicy adalah jumlah elemen actions per policy.
	MaxActionsPerPolicy = 50

	// MaxCodeLength adalah panjang kode policy.
	MaxCodeLength = 150

	// MaxNameRunes adalah panjang nama policy dalam karakter.
	MaxNameRunes = 150

	// MaxDescriptionRunes adalah panjang deskripsi policy dalam karakter.
	MaxDescriptionRunes = 1000

	// MaxPoliciesPerOrganization adalah jumlah policy AKTIF milik satu organisasi.
	MaxPoliciesPerOrganization = 50

	// MaxGlobalPolicies adalah jumlah policy AKTIF global (organization_id NULL). Jumlah kedua
	// batas ini tidak boleh melampaui MaxPoliciesPerCheck: itu yang menjamin sebuah check tidak
	// pernah gagal karena kebanyakan policy (dijaga TestWriteLimitsKeepEveryCheckWithinTheEvaluationLimit).
	MaxGlobalPolicies = 50
)

// Galat validasi tulis. Pesan tidak pernah memuat nilai yang dikirim klien (hanya posisi), supaya
// tidak ada isi mentah di log atau respons.
var (
	// ErrInvalidPolicyCode: kode policy tidak berbentuk segmen huruf kecil, angka, dan garis bawah
	// (dipisah titik) atau melebihi MaxCodeLength.
	ErrInvalidPolicyCode = errors.New("policy: kode policy tidak valid")

	// ErrInvalidName: nama kosong, berspasi di ujung, memuat karakter kontrol, bukan UTF-8, atau
	// melebihi MaxNameRunes.
	ErrInvalidName = errors.New("policy: nama policy tidak valid")

	// ErrInvalidDescription: deskripsi bukan UTF-8, memuat NUL, atau melebihi MaxDescriptionRunes.
	ErrInvalidDescription = errors.New("policy: deskripsi policy tidak valid")

	// ErrInvalidEffect: efek selain allow atau deny.
	ErrInvalidEffect = errors.New("policy: efek policy tidak valid")

	// ErrInvalidActions: actions kosong, lebih dari MaxActionsPerPolicy, ganda, atau salah bentuk.
	ErrInvalidActions = errors.New("policy: actions policy tidak valid")

	// ErrUnknownAction: sebuah action tidak ada di katalog permission. Policy yang terikat ke action
	// yang tidak ada tidak pernah berlaku; untuk policy deny itu berarti pembatasan yang diam-diam
	// tidak menolak apa pun, jadi ditolak saat ditulis.
	ErrUnknownAction = errors.New("policy: action tidak ada di katalog permission")

	// ErrCatalogUnavailable: katalog tidak diberikan; validasi tidak dapat meluluskan apa pun
	// (gagal-tertutup).
	ErrCatalogUnavailable = errors.New("policy: katalog permission tidak tersedia")

	// ErrTooManyPolicies: batas policy aktif untuk cakupan itu tercapai.
	ErrTooManyPolicies = errors.New("policy: jumlah policy aktif melebihi batas")
)

// policyCodePattern: segmen huruf kecil, angka, dan garis bawah, diawali huruf, dipisah titik;
// satu segmen cukup. Bentuk ini sengaja sama dengan segmen kode permission.
var policyCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)

// ActionCatalog adalah katalog permission yang menjadi acuan actions: kode core dari kode, kode
// modul dari tabel permissions. Diimplementasikan repositori (T5b4); domain tidak membaca database.
type ActionCatalog interface {
	// HasCode melaporkan apakah kode permission persis ada di katalog.
	HasCode(code string) bool

	// HasResource melaporkan apakah ada kode di katalog yang resource-nya persis resource itu
	// (semua segmen sebelum titik terakhir, permission.Split).
	HasResource(resource string) bool
}

// Draft adalah policy yang akan ditulis (dibuat atau diubah).
type Draft struct {
	Code        string
	Name        string
	Description string
	Effect      Effect
	Expression  string
	Actions     []string
}

// Validate memeriksa Draft sebelum ditulis, dengan urutan: katalog tersedia; kode; nama;
// deskripsi; efek; ekspresi (Parse: galatnya membungkus ErrSyntax atau ErrTooComplex); actions.
// Actions harus 1 sampai MaxActionsPerPolicy elemen tanpa duplikat; tiap elemen adalah kode
// permission persis yang ada di katalog atau `<resource>.*` dengan resource yang punya kode di
// katalog (organization.* hanya sah bila ada kode ber-resource organization; kode
// organization.member.manage dicakup organization.member.*). Draft tidak diubah. Keunikan kode dan
// batas jumlah policy (CheckPolicyCount) diperiksa repositori di dalam transaksi tulis.
func Validate(d Draft, cat ActionCatalog) error {
	if cat == nil {
		return ErrCatalogUnavailable
	}
	if len(d.Code) > MaxCodeLength || !policyCodePattern.MatchString(d.Code) {
		return ErrInvalidPolicyCode
	}
	if !validName(d.Name) {
		return ErrInvalidName
	}
	if !validDescription(d.Description) {
		return ErrInvalidDescription
	}
	if d.Effect != EffectAllow && d.Effect != EffectDeny {
		return ErrInvalidEffect
	}
	if _, err := Parse(d.Expression); err != nil {
		return err
	}
	return validateActions(d.Actions, cat)
}

func validName(name string) bool {
	if name == "" || name != strings.TrimSpace(name) || !utf8.ValidString(name) {
		return false
	}
	if utf8.RuneCountInString(name) > MaxNameRunes {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validDescription(desc string) bool {
	if !utf8.ValidString(desc) || strings.ContainsRune(desc, 0) {
		return false
	}
	return utf8.RuneCountInString(desc) <= MaxDescriptionRunes
}

func validateActions(actions []string, cat ActionCatalog) error {
	if len(actions) == 0 || len(actions) > MaxActionsPerPolicy {
		return fmt.Errorf("%w: jumlah harus 1 sampai %d", ErrInvalidActions, MaxActionsPerPolicy)
	}
	seen := make(map[string]struct{}, len(actions))
	for i, a := range actions {
		if len(a) > permission.MaxCodeLength {
			return fmt.Errorf("%w: actions[%d] terlalu panjang", ErrInvalidActions, i)
		}
		if _, dup := seen[a]; dup {
			return fmt.Errorf("%w: actions[%d] ganda", ErrInvalidActions, i)
		}
		seen[a] = struct{}{}

		if resource, wildcard := strings.CutSuffix(a, ".*"); wildcard {
			if _, _, err := permission.Split(resource + ".x"); err != nil {
				return fmt.Errorf("%w: actions[%d] salah bentuk", ErrInvalidActions, i)
			}
			if !cat.HasResource(resource) {
				return fmt.Errorf("%w: actions[%d]", ErrUnknownAction, i)
			}
			continue
		}
		if _, _, err := permission.Split(a); err != nil {
			return fmt.Errorf("%w: actions[%d] salah bentuk", ErrInvalidActions, i)
		}
		if !cat.HasCode(a) {
			return fmt.Errorf("%w: actions[%d]", ErrUnknownAction, i)
		}
	}
	return nil
}

// CheckPolicyCount memeriksa batas jumlah policy aktif sebelum satu policy aktif DITAMBAH (dibuat
// aktif, atau dipindah dari disabled ke active). active adalah jumlah policy aktif yang sudah ada
// pada cakupan itu (satu organisasi, atau global bila global true), dihitung repositori di dalam
// transaksi tulis di bawah advisory lock supaya dua penulisan serentak tidak sama-sama lolos.
// Hitungan negatif adalah masukan tidak masuk akal dan ditolak.
func CheckPolicyCount(active int, global bool) error {
	limit := MaxPoliciesPerOrganization
	if global {
		limit = MaxGlobalPolicies
	}
	if active < 0 || active >= limit {
		return ErrTooManyPolicies
	}
	return nil
}
