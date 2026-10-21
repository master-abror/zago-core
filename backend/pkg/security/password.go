// Package security memuat primitif keamanan murni (tanpa HTTP/DB) yang dipakai lapisan
// autentikasi (docs/05). Parameter Argon2id hidup di satu tempat ini dan di config
// (docs/05 §4.1), tidak tersebar di service.
package security

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	// MaxPasswordBytes adalah batas atas panjang password (docs/05 §4.1). Input lebih panjang
	// ditolak SEBELUM hashing: password tanpa batas adalah DoS CPU/memori murah terhadap Argon2.
	MaxPasswordBytes = 256
	// DefaultMinPasswordLength adalah panjang minimum bawaan dalam karakter (docs/05 §4.1).
	DefaultMinPasswordLength = 12

	saltLen = 16 // byte, untuk hash baru
	keyLen  = 32 // byte, untuk hash baru

	// Batas yang diterima saat MEMVERIFIKASI hash dari penyimpanan: baris rusak/dimanipulasi
	// tidak boleh memaksa alokasi memori raksasa. Nilai di atas parameter bawaan dengan jauh.
	maxMemoryKiB   = 1 << 20 // 1 GiB
	maxTime        = 16
	maxParallelism = 64
	minSaltLen     = 8
	maxSaltLen     = 64
	minKeyLen      = 16
	maxKeyLen      = 64
	maxHashLen     = 512
)

var (
	// ErrPasswordTooLong: password melebihi MaxPasswordBytes.
	ErrPasswordTooLong = errors.New("security: password melebihi batas panjang")
	// ErrPasswordTooShort: password lebih pendek dari minimum kebijakan.
	ErrPasswordTooShort = errors.New("security: password lebih pendek dari minimum")
	// ErrMismatch: password tidak cocok dengan hash. Satu-satunya hasil "salah password".
	ErrMismatch = errors.New("security: password tidak cocok")
	// ErrInvalidHash: hash tersimpan tidak terbaca/di luar batas. BUKAN password salah —
	// pemanggil harus bisa membedakannya (data rusak, bukan kegagalan login biasa).
	ErrInvalidHash = errors.New("security: hash password tidak valid")
	// ErrInvalidParams: parameter Argon2id tidak aman/tidak sah.
	ErrInvalidParams = errors.New("security: parameter argon2id tidak valid")
)

// Argon2Params adalah parameter biaya Argon2id.
type Argon2Params struct {
	MemoryKiB   uint32 // memori dalam KiB
	Time        uint32 // jumlah iterasi
	Parallelism uint8  // jumlah thread
}

// DefaultArgon2Params mengembalikan parameter awal docs/05 §4.1: 64 MiB, t=3, p=2.
// Setel ulang lewat konfigurasi dengan benchmark di perangkat keras deploy (target 50–150 ms).
func DefaultArgon2Params() Argon2Params {
	return Argon2Params{MemoryKiB: 64 * 1024, Time: 3, Parallelism: 2}
}

// Validate memastikan parameter dalam rentang aman (dan memori minimal 8 KiB x paralelisme,
// batas bawah Argon2).
func (p Argon2Params) Validate() error {
	switch {
	case p.Parallelism == 0 || p.Parallelism > maxParallelism:
		return fmt.Errorf("%w: paralelisme harus 1..%d", ErrInvalidParams, maxParallelism)
	case p.Time == 0 || p.Time > maxTime:
		return fmt.Errorf("%w: waktu harus 1..%d", ErrInvalidParams, maxTime)
	case p.MemoryKiB < 8*uint32(p.Parallelism) || p.MemoryKiB > maxMemoryKiB:
		return fmt.Errorf("%w: memori harus minimal 8*paralelisme dan maksimal %d KiB", ErrInvalidParams, maxMemoryKiB)
	}
	return nil
}

// Argon2ParamsFromInts membangun parameter dari bilangan bulat biasa (mis. konfigurasi).
// Nilai di luar rentang ditolak SEBELUM konversi, jadi tidak ada overflow diam-diam.
func Argon2ParamsFromInts(memoryKiB, time, parallelism int) (Argon2Params, error) {
	if memoryKiB < 0 || memoryKiB > maxMemoryKiB || time < 0 || time > maxTime ||
		parallelism < 0 || parallelism > maxParallelism {
		return Argon2Params{}, fmt.Errorf("%w: nilai di luar rentang", ErrInvalidParams)
	}
	p := Argon2Params{MemoryKiB: uint32(memoryKiB), Time: uint32(time), Parallelism: uint8(parallelism)}
	return p, p.Validate()
}

// PasswordHasher memverifikasi dan membuat hash password. Berbeda dari sketsa docs/05 §4.1,
// semua operasi mahal menerima context agar antrean semaphore bisa dibatalkan (ADR-0020).
type PasswordHasher interface {
	// Hash mengembalikan string PHC.
	Hash(ctx context.Context, password string) (string, error)
	// Compare mengembalikan nil bila cocok, ErrMismatch bila salah, ErrInvalidHash bila hash rusak.
	Compare(ctx context.Context, hash, password string) error
	// CompareDummy menjalankan verifikasi penuh terhadap hash palsu lalu SELALU mengembalikan
	// ErrMismatch: dipakai untuk email tak dikenal agar waktu responsnya setara (I-04).
	CompareDummy(ctx context.Context, password string) error
	// NeedsRehash benar bila parameter hash berbeda dari parameter terkini atau hash tak terbaca.
	NeedsRehash(hash string) bool
}

// Argon2idHasher mengimplementasikan PasswordHasher dengan Argon2id.
type Argon2idHasher struct {
	params Argon2Params
	sem    chan struct{} // semaphore konkurensi hashing
	dummy  string        // PHC dengan parameter terkini, dibuat acak per instans

	// onDerive, bila non-nil, dipanggil di dalam bagian kritis sebelum setiap derivasi. Hanya
	// untuk tes (mengamati konkurensi dan jalur); nil di produksi.
	onDerive func()
}

var _ PasswordHasher = (*Argon2idHasher)(nil)

// NewArgon2idHasher membuat hasher. concurrency <= 0 berarti jumlah CPU (docs/05 §4.1).
// Satu hash dummy dibuat saat konstruksi dengan parameter yang sama.
func NewArgon2idHasher(p Argon2Params, concurrency int) (*Argon2idHasher, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if concurrency <= 0 {
		concurrency = runtime.NumCPU()
	}
	h := &Argon2idHasher{params: p, sem: make(chan struct{}, concurrency)}

	// Dummy: password acak yang tak pernah diketahui siapa pun; yang dibutuhkan hanya biaya
	// verifikasinya. Tidak ada nilai tetap di kode sumber.
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("security: gagal membuat dummy hash: %w", err)
	}
	d, err := h.hashLocked(string(secret))
	if err != nil {
		return nil, err
	}
	h.dummy = d
	return h, nil
}

// Params mengembalikan parameter terkini (untuk log startup/diagnostik).
func (h *Argon2idHasher) Params() Argon2Params { return h.params }

// Hash membuat hash PHC baru dengan salt acak.
func (h *Argon2idHasher) Hash(ctx context.Context, password string) (string, error) {
	if err := checkLength(password); err != nil {
		return "", err
	}
	release, err := h.acquire(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	if h.onDerive != nil {
		h.onDerive()
	}
	return h.hashLocked(password)
}

// hashLocked membuat hash tanpa semaphore (dipakai konstruktor dan Hash).
func (h *Argon2idHasher) hashLocked(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("security: gagal membuat salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, h.params.Time, h.params.MemoryKiB, h.params.Parallelism, keyLen)
	return encodePHC(h.params, salt, key), nil
}

// Compare memverifikasi password terhadap hash PHC dengan perbandingan waktu-konstan.
func (h *Argon2idHasher) Compare(ctx context.Context, hash, password string) error {
	if err := checkLength(password); err != nil {
		return err
	}
	p, salt, key, err := parsePHC(hash)
	if err != nil {
		return err
	}
	return h.verify(ctx, p, salt, key, password)
}

// CompareDummy: lihat PasswordHasher.
func (h *Argon2idHasher) CompareDummy(ctx context.Context, password string) error {
	if err := checkLength(password); err != nil {
		return err
	}
	p, salt, key, err := parsePHC(h.dummy)
	if err != nil { // tak mungkin: dummy dibuat sendiri; tetap fail-closed
		return err
	}
	if err := h.verify(ctx, p, salt, key, password); err != nil && !errors.Is(err, ErrMismatch) {
		return err
	}
	return ErrMismatch // apa pun hasil perbandingannya, dummy tidak pernah sukses
}

func (h *Argon2idHasher) verify(ctx context.Context, p Argon2Params, salt, key []byte, password string) error {
	release, err := h.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	if h.onDerive != nil {
		h.onDerive()
	}
	got := argon2.IDKey([]byte(password), salt, p.Time, p.MemoryKiB, p.Parallelism, uint32(len(key)))
	if subtle.ConstantTimeCompare(got, key) != 1 {
		return ErrMismatch
	}
	return nil
}

// NeedsRehash: lihat PasswordHasher.
func (h *Argon2idHasher) NeedsRehash(hash string) bool {
	p, salt, key, err := parsePHC(hash)
	if err != nil {
		return true
	}
	return p != h.params || len(salt) != saltLen || len(key) != keyLen
}

// acquire mengambil slot semaphore atau gagal bila context selesai lebih dulu.
func (h *Argon2idHasher) acquire(ctx context.Context) (release func(), err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case h.sem <- struct{}{}:
		return func() { <-h.sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func checkLength(password string) error {
	if len(password) > MaxPasswordBytes {
		return ErrPasswordTooLong
	}
	return nil
}

// ---- PHC ----

func encodePHC(p Argon2Params, salt, key []byte) string {
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.MemoryKiB, p.Time, p.Parallelism, b64.EncodeToString(salt), b64.EncodeToString(key))
}

func invalidHash(reason string) error { return fmt.Errorf("%w: %s", ErrInvalidHash, reason) }

// parsePHC mengurai string PHC argon2id secara ketat; apa pun di luar format/batas ditolak.
func parsePHC(hash string) (Argon2Params, []byte, []byte, error) {
	var zero Argon2Params
	if hash == "" || len(hash) > maxHashLen {
		return zero, nil, nil, invalidHash("panjang")
	}
	parts := strings.Split(hash, "$")
	// "", "argon2id", "v=19", "m=..,t=..,p=..", salt, key
	if len(parts) != 6 || parts[0] != "" {
		return zero, nil, nil, invalidHash("format")
	}
	if parts[1] != "argon2id" {
		return zero, nil, nil, invalidHash("algoritma")
	}
	if parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return zero, nil, nil, invalidHash("versi")
	}
	p, ok := parseParams(parts[3])
	if !ok {
		return zero, nil, nil, invalidHash("parameter")
	}
	if p.Validate() != nil {
		return zero, nil, nil, invalidHash("parameter di luar batas")
	}
	b64 := base64.RawStdEncoding.Strict()
	salt, err := b64.DecodeString(parts[4])
	if err != nil || len(salt) < minSaltLen || len(salt) > maxSaltLen {
		return zero, nil, nil, invalidHash("salt")
	}
	key, err := b64.DecodeString(parts[5])
	if err != nil || len(key) < minKeyLen || len(key) > maxKeyLen {
		return zero, nil, nil, invalidHash("kunci")
	}
	return p, salt, key, nil
}

func parseParams(s string) (Argon2Params, bool) {
	f := strings.Split(s, ",")
	if len(f) != 3 {
		return Argon2Params{}, false
	}
	m, ok1 := parseUint(f[0], "m=", 32)
	t, ok2 := parseUint(f[1], "t=", 32)
	par, ok3 := parseUint(f[2], "p=", 8)
	if !ok1 || !ok2 || !ok3 {
		return Argon2Params{}, false
	}
	return Argon2Params{MemoryKiB: uint32(m), Time: uint32(t), Parallelism: uint8(par)}, true
}

func parseUint(field, prefix string, bits int) (uint64, bool) {
	v, ok := strings.CutPrefix(field, prefix)
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseUint(v, 10, bits)
	return n, err == nil
}

// ---- kebijakan ----

// PasswordPolicy adalah kebijakan password (docs/05 §4.1): panjang minimum tanpa aturan
// komposisi. MinLength dihitung dalam karakter (rune); 0 berarti DefaultMinPasswordLength.
type PasswordPolicy struct {
	MinLength int
}

// Check mengembalikan ErrPasswordTooLong atau ErrPasswordTooShort, atau nil bila lolos.
func (p PasswordPolicy) Check(password string) error {
	if err := checkLength(password); err != nil {
		return err
	}
	minLen := p.MinLength
	if minLen <= 0 {
		minLen = DefaultMinPasswordLength
	}
	if utf8.RuneCountInString(password) < minLen {
		return ErrPasswordTooShort
	}
	return nil
}
