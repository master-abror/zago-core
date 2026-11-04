package domain

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/permission"
)

// fieldSegment adalah segmen yang dicadangkan pada kode field permission
// `<tipe>.field.<nama>.<aksi>` (docs/06 §5.1, ADR-0022 butir 35).
const fieldSegment = "field"

// MaxDeniedFields membatasi jumlah nama kunci pada FieldDeniedError (ADR-0022 butir 40).
const MaxDeniedFields = 50

// FieldAction adalah aksi pada sebuah field: dibaca atau ditulis. Keduanya diperiksa terpisah
// (docs/06 §7).
type FieldAction string

const (
	// FieldRead: field tampil pada keluaran.
	FieldRead FieldAction = "read"

	// FieldWrite: field boleh diubah lewat masukan.
	FieldWrite FieldAction = "write"
)

func (a FieldAction) valid() bool {
	return a == FieldRead || a == FieldWrite
}

var (
	// ErrInvalidFieldCode menandai kode field permission yang tidak sah, atau kode berbentuk field
	// milik sebuah tipe dengan aksi selain read/write.
	ErrInvalidFieldCode = errors.New("authorization: kode field permission tidak sah")

	// ErrInvalidFieldRules menandai tipe sumber daya yang tidak sah atau aturan/filter yang belum
	// diinisialisasi (nilai nol). Selalu gagal-tertutup.
	ErrInvalidFieldRules = errors.New("authorization: aturan field tidak sah")

	// ErrFieldScopeMismatch: organisasi sumber daya berbeda dari organisasi scope. Galat, bukan
	// penapisan diam-diam (ADR-0022 butir 37).
	ErrFieldScopeMismatch = errors.New("authorization: sumber daya di luar scope")

	// ErrFieldTypeMismatch: tipe sumber daya berbeda dari tipe aturan yang dipakai.
	ErrFieldTypeMismatch = errors.New("authorization: tipe sumber daya tidak sama dengan tipe aturan field")

	// ErrFieldPermissionDenied dibungkus FieldDeniedError: ada field terbatas yang tidak boleh ditulis.
	ErrFieldPermissionDenied = errors.New("authorization: field tidak boleh ditulis")
)

// FieldDeniedError adalah hasil CheckWrite yang menolak. Pesannya sengaja tidak memuat nama kunci
// (berasal dari klien); nama ada di Fields untuk pemetaan HTTP dan audit.
type FieldDeniedError struct {
	// Fields berisi kunci terkirim yang ditolak, terurut, paling banyak MaxDeniedFields.
	Fields []string

	// Total adalah jumlah penuh kunci yang ditolak (bisa lebih besar dari len(Fields)).
	Total int
}

func (e *FieldDeniedError) Error() string {
	return ErrFieldPermissionDenied.Error()
}

// Is membuat errors.Is(err, ErrFieldPermissionDenied) bernilai true.
func (e *FieldDeniedError) Is(target error) bool {
	return target == ErrFieldPermissionDenied
}

// FieldCode membangun kode `<tipe>.field.<nama>.<aksi>` yang sah. Tipe boleh bertitik; nama satu segmen.
func FieldCode(resourceType, name string, action FieldAction) (string, error) {
	if !action.valid() {
		return "", fmt.Errorf("%w: aksi harus read atau write", ErrInvalidFieldCode)
	}
	code := resourceType + "." + fieldSegment + "." + name + "." + string(action)
	if _, _, err := permission.Split(code); err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidFieldCode, err)
	}
	parts := strings.Split(code, ".")
	n := len(parts)
	if n < 4 || parts[n-3] != fieldSegment || strings.Join(parts[:n-3], ".") != resourceType || parts[n-2] != name {
		return "", fmt.Errorf("%w: tipe atau nama bukan segmen yang sah", ErrInvalidFieldCode)
	}
	return code, nil
}

// caseShift adalah selisih huruf besar ke huruf kecil ASCII.
const caseShift = 'a' - 'A'

// squashKey melipat kunci ke bentuk ringkas: huruf ASCII kecil dan angka saja. Karakter lain
// (garis bawah, tanda hubung, spasi, titik, kontrol, dan semua byte non-ASCII) dibuang, sehingga
// `internalCost`, `INTERNAL_COST`, dan `internal-cost` berbentuk sama (ADR-0022 butir 38).
func squashKey(key string) string {
	var b strings.Builder
	b.Grow(len(key))
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteByte(c)
		case c >= 'A' && c <= 'Z':
			b.WriteByte(c + caseShift)
		}
	}
	return b.String()
}

func isASCII(key string) bool {
	for i := 0; i < len(key); i++ {
		if key[i] >= 0x80 {
			return false
		}
	}
	return true
}

func addCode(m map[string][]string, key, code string) {
	if !slices.Contains(m[key], code) {
		m[key] = append(m[key], code)
	}
}

// FieldRules adalah definisi field terbatas untuk SATU tipe sumber daya, diturunkan dari kode
// katalog. Tidak berubah setelah dibuat; nilai nol tidak sah dan gagal-tertutup.
type FieldRules struct {
	// resourceType adalah tipe yang aturan ini berlaku untuknya.
	resourceType string

	// read memetakan kunci ringkas ke kode baca yang SEMUANYA wajib dipegang.
	read map[string][]string

	// write memetakan kunci ringkas ke kode tulis yang SEMUANYA wajib dipegang.
	write map[string][]string

	// allRead adalah semua kode baca tipe ini (untuk kunci non-ASCII), terurut.
	allRead []string

	// allWrite adalah semua kode tulis tipe ini (untuk kunci non-ASCII), terurut.
	allWrite []string
}

// NewFieldRules membangun aturan field untuk resourceType dari kode katalog. Kode yang bukan kode
// field milik tipe ini (kode biasa, atau field tipe lain) diabaikan; kode yang bukan kode sah sama
// sekali, atau kode field tipe ini dengan aksi selain read/write, adalah galat (ADR-0022 butir 35):
// pemuat tidak boleh membiarkan field tanpa pembatasan karena definisinya rusak.
func NewFieldRules(resourceType string, codes []string) (FieldRules, error) {
	if _, _, err := permission.Split(resourceType + "." + fieldSegment + ".x.read"); err != nil {
		return FieldRules{}, fmt.Errorf("%w: tipe sumber daya tidak sah", ErrInvalidFieldRules)
	}
	rules := FieldRules{resourceType: resourceType}
	rules.read = make(map[string][]string)
	rules.write = make(map[string][]string)
	allRead := make(map[string]struct{})
	allWrite := make(map[string]struct{})
	for _, code := range codes {
		if _, _, err := permission.Split(code); err != nil {
			return FieldRules{}, fmt.Errorf("%w: %w", ErrInvalidFieldCode, err)
		}
		parts := strings.Split(code, ".")
		n := len(parts)
		if n < 4 || parts[n-3] != fieldSegment {
			continue
		}
		if strings.Join(parts[:n-3], ".") != resourceType {
			continue
		}
		key := squashKey(parts[n-2])
		switch FieldAction(parts[n-1]) {
		case FieldRead:
			addCode(rules.read, key, code)
			allRead[code] = struct{}{}
		case FieldWrite:
			addCode(rules.write, key, code)
			allWrite[code] = struct{}{}
		default:
			return FieldRules{}, fmt.Errorf("%w: aksi field harus read atau write", ErrInvalidFieldCode)
		}
	}
	for _, list := range rules.read {
		slices.Sort(list)
	}
	for _, list := range rules.write {
		slices.Sort(list)
	}
	rules.allRead = slices.Sorted(maps.Keys(allRead))
	rules.allWrite = slices.Sorted(maps.Keys(allWrite))
	return rules, nil
}

// ResourceType mengembalikan tipe yang aturan ini berlaku untuknya ("" untuk nilai nol).
func (r FieldRules) ResourceType() string {
	return r.resourceType
}

// Restricts melaporkan apakah kunci dibatasi untuk aksi itu.
func (r FieldRules) Restricts(key string, action FieldAction) bool {
	return len(r.required(action, key)) > 0
}

// required mengembalikan kode yang semuanya wajib dipegang untuk kunci dan aksi itu. Kunci
// non-ASCII mensyaratkan SEMUA kode terbatas tipe ini (ADR-0022 butir 38).
func (r FieldRules) required(action FieldAction, key string) []string {
	byKey, all := r.read, r.allRead
	if action == FieldWrite {
		byKey, all = r.write, r.allWrite
	}
	if !isASCII(key) {
		return all
	}
	return byKey[squashKey(key)]
}

// FieldFilter menapis dan memeriksa field untuk satu (Identity, ScopeContext, tipe), dengan
// himpunan izin yang sudah dihitung. Aman dipakai bersamaan; nilai nol gagal-tertutup.
type FieldFilter struct {
	// rules adalah definisi field terbatas tipe ini.
	rules FieldRules

	// effective adalah himpunan izin pemanggil pada scope ini.
	effective EffectivePermissions

	// scope adalah scope request yang sudah divalidasi.
	scope ScopeContext
}

// NewFieldFilter membuat filter. Scope harus sah (ErrInvalidScope) dan aturan harus terinisialisasi
// (ErrInvalidFieldRules).
func NewFieldFilter(rules FieldRules, effective EffectivePermissions, scope ScopeContext) (FieldFilter, error) {
	if rules.resourceType == "" {
		return FieldFilter{}, fmt.Errorf("%w: aturan belum diinisialisasi", ErrInvalidFieldRules)
	}
	valid, err := NewScope(scope.OrganizationID, scope.ActiveGroupID)
	if err != nil {
		return FieldFilter{}, err
	}
	return FieldFilter{rules: rules, effective: effective, scope: valid}, nil
}

func sameOptionalID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (f FieldFilter) checkRef(ref ResourceRef) error {
	if f.rules.resourceType == "" {
		return ErrInvalidFieldRules
	}
	if ref.Type != f.rules.resourceType {
		return ErrFieldTypeMismatch
	}
	if !sameOptionalID(ref.OrganizationID, f.scope.OrganizationID) {
		return ErrFieldScopeMismatch
	}
	return nil
}

// held melaporkan apakah code dipegang untuk sumber daya ini. Izin level group hanya berlaku bila
// sumber daya milik grup aktif (sama dengan Authorizer.Can, ADR-0022 butir 22).
func (f FieldFilter) held(code string, ref ResourceRef) bool {
	level, ok := f.effective.Level(code)
	if !ok {
		return false
	}
	switch level {
	case LevelPlatform, LevelOrganization:
		return true
	case LevelGroup:
		active, hasActive := f.scope.Group()
		return hasActive && ref.GroupID != nil && *ref.GroupID == active
	default:
		return false
	}
}

func (f FieldFilter) allowed(action FieldAction, ref ResourceRef, key string) bool {
	for _, code := range f.rules.required(action, key) {
		if !f.held(code, ref) {
			return false
		}
	}
	return true
}

// Apply mengembalikan salinan BARU obj tanpa field terbatas yang tidak boleh dibaca: kunci itu
// absen (bukan null atau tersamar). Masukan tidak diubah; hasil tidak pernah nil saat sukses.
// Hanya kunci tingkat atas yang ditapis dan salinannya dangkal (ADR-0022 butir 39). Galat bila
// tipe atau organisasi sumber daya tidak cocok, atau filter belum diinisialisasi.
func (f FieldFilter) Apply(ref ResourceRef, obj map[string]any) (map[string]any, error) {
	if err := f.checkRef(ref); err != nil {
		return nil, err
	}
	out := make(map[string]any, len(obj))
	for key, value := range obj {
		if f.allowed(FieldRead, ref, key) {
			out[key] = value
		}
	}
	return out, nil
}

// CheckWrite menolak (FieldDeniedError) bila obj memuat field terbatas yang tidak boleh ditulis.
// Hak tulis diperiksa terpisah dari hak baca. Periksa kunci MENTAH dari klien sebelum didekode ke
// struct (ADR-0022 butir 40).
func (f FieldFilter) CheckWrite(ref ResourceRef, obj map[string]any) error {
	if err := f.checkRef(ref); err != nil {
		return err
	}
	var denied []string
	for key := range obj {
		if !f.allowed(FieldWrite, ref, key) {
			denied = append(denied, key)
		}
	}
	if len(denied) == 0 {
		return nil
	}
	slices.Sort(denied)
	total := len(denied)
	if total > MaxDeniedFields {
		denied = denied[:MaxDeniedFields]
	}
	return &FieldDeniedError{Fields: denied, Total: total}
}
