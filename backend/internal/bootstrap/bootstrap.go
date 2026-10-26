// Package bootstrap membuat Super Admin pertama (docs/04 §16 langkah 4, E:B-04, docs/21 M03 T6).
//
// Semua perubahan terjadi dalam SATU transaksi yang diserialkan advisory lock: platform row
// harus sudah ada, role Super Admin (sistem) di-upsert, user + kredensial password + assignment
// scope platform dibuat bila belum ada, lalu activity dan security event ditulis. Idempoten:
// pemanggilan berulang tidak menambah baris dan TIDAK PERNAH menimpa password yang sudah ada.
//
// Paket ini hanya butuh hak runtime (role app_user): tidak ada DDL dan tidak ada role migrator.
package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/mail"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/master-abror/zago-core/backend/pkg/id"
	"github.com/master-abror/zago-core/backend/pkg/security"
)

// SuperAdminSlug adalah slug role sistem Super Admin. M04 mengadopsi baris ini lewat upsert
// by slug saat menyinkronkan role sistem dari kode (E:B-04); bootstrap tidak mengisi permission.
const SuperAdminSlug = "super-admin"

// EventBootstrapAdmin adalah nama activity dan security event bootstrap.
const EventBootstrapAdmin = "auth.bootstrap_admin"

// bootstrapLock menserialkan bootstrap bersamaan (advisory lock per transaksi). Berbeda dari
// kunci seeder platform di dbmigrate (0x7a61676f01).
const bootstrapLock int64 = 0x7a61676f02 // "zago" + 02

const (
	superAdminName     = "Super Admin"
	superAdminDesc     = "Akses penuh tingkat platform. Dibuat oleh bootstrap-admin; permission disinkronkan oleh M04."
	generatedMinLength = 24
	maxEmailLength     = 320
	maxDisplayLength   = 200

	// Tanpa karakter yang mudah tertukar (0/O, 1/l/I) agar mudah disalin dari terminal.
	passwordAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
)

var (
	// ErrInvalidInput: email, nama tampilan, atau password ditolak SEBELUM database disentuh.
	ErrInvalidInput = errors.New("bootstrap: input tidak valid")
	// ErrNoPlatform: baris platform belum ada; jalankan seeder (migrate seed) lebih dulu.
	ErrNoPlatform = errors.New("bootstrap: baris platform belum ada; jalankan `make db-seed` lebih dulu")
	// ErrUserUnavailable: user dengan email itu ada tetapi bukan akun aktif; bootstrap tidak mengubahnya.
	ErrUserUnavailable = errors.New("bootstrap: user dengan email ini sudah ada tetapi tidak berstatus active; tidak diubah")
)

// Beginner dipenuhi *pgx.Conn dan *pgxpool.Pool.
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Params adalah masukan bootstrap.
type Params struct {
	// Email dinormalisasi ke huruf kecil (ck_users_email_lower).
	Email string
	// Password kosong berarti dibangkitkan acak (dikembalikan SEKALI di Result).
	Password string
	// DisplayName kosong berarti bagian lokal email.
	DisplayName string
}

// Result menjelaskan apa yang dikerjakan pemanggilan ini.
type Result struct {
	UserID            uuid.UUID
	Email             string
	UserCreated       bool
	RoleCreated       bool
	CredentialCreated bool
	AssignmentCreated bool
	// GeneratedPassword hanya terisi bila password dibangkitkan DAN tersimpan (transaksi commit).
	GeneratedPassword string
}

// Changed melaporkan apakah ada baris yang dibuat.
func (r Result) Changed() bool {
	return r.UserCreated || r.RoleCreated || r.CredentialCreated || r.AssignmentCreated
}

// Bootstrapper membuat Super Admin pertama.
type Bootstrapper struct {
	hasher security.PasswordHasher
	policy security.PasswordPolicy
}

// New membuat Bootstrapper. Hasher wajib.
func New(hasher security.PasswordHasher, policy security.PasswordPolicy) (*Bootstrapper, error) {
	if hasher == nil {
		return nil, errors.New("bootstrap: hasher password wajib")
	}
	return &Bootstrapper{hasher: hasher, policy: policy}, nil
}

// NormalizeEmail memangkas spasi, mengecilkan huruf, dan menolak apa pun selain alamat polos
// (tanpa nama tampilan atau kurung sudut).
func NormalizeEmail(raw string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" || len(s) > maxEmailLength {
		return "", fmt.Errorf("%w: email kosong atau lebih dari %d karakter", ErrInvalidInput, maxEmailLength)
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s || addr.Name != "" || strings.Count(s, "@") != 1 {
		return "", fmt.Errorf("%w: email bukan alamat yang sah", ErrInvalidInput)
	}
	return s, nil
}

// Validate menormalisasi dan memeriksa Params tanpa menyentuh database. Pesan galat tidak
// pernah memuat password.
func (b *Bootstrapper) Validate(p Params) (Params, error) {
	email, err := NormalizeEmail(p.Email)
	if err != nil {
		return Params{}, err
	}
	display := strings.TrimSpace(p.DisplayName)
	if display == "" {
		display = email[:strings.IndexByte(email, '@')]
	}
	if len([]rune(display)) > maxDisplayLength {
		return Params{}, fmt.Errorf("%w: nama tampilan lebih dari %d karakter", ErrInvalidInput, maxDisplayLength)
	}
	if p.Password != "" {
		if err := b.policy.Check(p.Password); err != nil {
			return Params{}, fmt.Errorf("%w: %w", ErrInvalidInput, err)
		}
	}
	return Params{Email: email, Password: p.Password, DisplayName: display}, nil
}

// Run menjalankan bootstrap dalam satu transaksi. Pada galat apa pun hasilnya Result{} (tidak
// ada password untuk ditampilkan karena tidak ada yang tersimpan).
func (b *Bootstrapper) Run(ctx context.Context, db Beginner, p Params) (Result, error) {
	p, err := b.Validate(p)
	if err != nil {
		return Result{}, err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("bootstrap: mulai transaksi: %w", err)
	}
	res, err := b.apply(ctx, tx, p)
	if err != nil {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		return Result{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("bootstrap: commit: %w", err)
	}
	return res, nil
}

func (b *Bootstrapper) apply(ctx context.Context, tx pgx.Tx, p Params) (Result, error) {
	res := Result{Email: p.Email}

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", bootstrapLock); err != nil {
		return Result{}, fmt.Errorf("bootstrap: kunci advisory: %w", err)
	}

	var hasPlatform bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM platforms)").Scan(&hasPlatform); err != nil {
		return Result{}, fmt.Errorf("bootstrap: periksa platform: %w", err)
	}
	if !hasPlatform {
		return Result{}, ErrNoPlatform
	}

	roleID, created, err := ensureSuperAdminRole(ctx, tx)
	if err != nil {
		return Result{}, err
	}
	res.RoleCreated = created

	userID, created, err := ensureUser(ctx, tx, p)
	if err != nil {
		return Result{}, err
	}
	res.UserID = userID
	res.UserCreated = created

	generated, created, err := b.ensureCredential(ctx, tx, userID, p.Password)
	if err != nil {
		return Result{}, err
	}
	res.CredentialCreated = created
	res.GeneratedPassword = generated

	created, err = ensureAssignment(ctx, tx, roleID, userID)
	if err != nil {
		return Result{}, err
	}
	res.AssignmentCreated = created

	if res.Changed() {
		if err = writeAudit(ctx, tx, res); err != nil {
			return Result{}, err
		}
	}
	return res, nil
}

func ensureSuperAdminRole(ctx context.Context, tx pgx.Tx) (roleID uuid.UUID, created bool, err error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO roles (id, name, slug, description, role_type)
		VALUES ($1, $2, $3, $4, 'system')
		ON CONFLICT (slug) WHERE organization_id IS NULL DO NOTHING`,
		id.NewID(), superAdminName, SuperAdminSlug, superAdminDesc)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("bootstrap: role Super Admin: %w", err)
	}
	err = tx.QueryRow(ctx, "SELECT id FROM roles WHERE organization_id IS NULL AND slug = $1", SuperAdminSlug).Scan(&roleID)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("bootstrap: baca role Super Admin: %w", err)
	}
	return roleID, tag.RowsAffected() == 1, nil
}

func ensureUser(ctx context.Context, tx pgx.Tx, p Params) (userID uuid.UUID, created bool, err error) {
	var status string
	var deleted bool
	err = tx.QueryRow(ctx, "SELECT id, status, deleted_at IS NOT NULL FROM users WHERE email = $1", p.Email).
		Scan(&userID, &status, &deleted)
	switch {
	case err == nil:
		if deleted || status != "active" {
			return uuid.Nil, false, ErrUserUnavailable
		}
		return userID, false, nil
	case errors.Is(err, pgx.ErrNoRows):
		userID = id.NewID()
		if _, err = tx.Exec(ctx,
			"INSERT INTO users (id, display_name, email, status) VALUES ($1, $2, $3, 'active')",
			userID, p.DisplayName, p.Email); err != nil {
			return uuid.Nil, false, fmt.Errorf("bootstrap: buat user: %w", err)
		}
		return userID, true, nil
	default:
		return uuid.Nil, false, fmt.Errorf("bootstrap: cari user: %w", err)
	}
}

// ensureCredential membuat kredensial password HANYA bila user belum punya yang aktif. Password
// yang dibangkitkan dikembalikan hanya bila benar-benar disimpan.
func (b *Bootstrapper) ensureCredential(ctx context.Context, tx pgx.Tx, userID uuid.UUID, password string) (generated string, created bool, err error) {
	var exists bool
	err = tx.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM credentials WHERE user_id = $1 AND credential_type = 'password' AND revoked_at IS NULL)",
		userID).Scan(&exists)
	if err != nil {
		return "", false, fmt.Errorf("bootstrap: periksa kredensial: %w", err)
	}
	if exists {
		return "", false, nil
	}

	plain := password
	if plain == "" {
		if plain, err = b.generatePassword(); err != nil {
			return "", false, err
		}
		generated = plain
	}
	hash, err := b.hasher.Hash(ctx, plain)
	if err != nil {
		return "", false, fmt.Errorf("bootstrap: hash password: %w", err)
	}
	if _, err = tx.Exec(ctx,
		"INSERT INTO credentials (id, user_id, credential_type, secret_hash) VALUES ($1, $2, 'password', $3)",
		id.NewID(), userID, hash); err != nil {
		return "", false, fmt.Errorf("bootstrap: simpan kredensial: %w", err)
	}
	return generated, true, nil
}

func ensureAssignment(ctx context.Context, tx pgx.Tx, roleID, userID uuid.UUID) (created bool, err error) {
	var exists bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM role_assignments
			WHERE role_id = $1 AND subject_type = 'user' AND subject_id = $2
			  AND scope_type = 'platform' AND revoked_at IS NULL)`,
		roleID, userID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("bootstrap: periksa assignment: %w", err)
	}
	if exists {
		return false, nil
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO role_assignments (id, role_id, subject_type, subject_id, scope_type)
		VALUES ($1, $2, 'user', $3, 'platform')`,
		id.NewID(), roleID, userID); err != nil {
		return false, fmt.Errorf("bootstrap: buat assignment platform: %w", err)
	}
	return true, nil
}

// writeAudit menulis activity dan security event pada transaksi yang sama dengan perubahan
// state. Metadata hanya berisi penanda boolean: tanpa email, tanpa password.
func writeAudit(ctx context.Context, tx pgx.Tx, res Result) error {
	meta, err := json.Marshal(map[string]any{
		"user_created":       res.UserCreated,
		"role_created":       res.RoleCreated,
		"credential_created": res.CredentialCreated,
		"assignment_created": res.AssignmentCreated,
		"password_generated": res.GeneratedPassword != "",
	})
	if err != nil {
		return fmt.Errorf("bootstrap: metadata audit: %w", err)
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO activities (id, actor_type, action, resource_type, resource_id, scope_type, result, metadata)
		VALUES ($1, 'system', $2, 'user', $3, 'platform', 'success', $4::jsonb)`,
		id.NewID(), EventBootstrapAdmin, res.UserID, meta); err != nil {
		return fmt.Errorf("bootstrap: tulis activity: %w", err)
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO security_events (id, user_id, event_type, result, metadata)
		VALUES ($1, $2, $3, 'success', $4::jsonb)`,
		id.NewID(), res.UserID, EventBootstrapAdmin, meta); err != nil {
		return fmt.Errorf("bootstrap: tulis security event: %w", err)
	}
	return nil
}

// generatePassword membangkitkan password acak dari crypto/rand dengan sampling seragam.
// Panjang minimal 24 karakter dan tidak kurang dari minimum kebijakan.
func (b *Bootstrapper) generatePassword() (string, error) {
	n := generatedMinLength
	if b.policy.MinLength > n {
		n = b.policy.MinLength
	}
	if n > security.MaxPasswordBytes {
		n = security.MaxPasswordBytes
	}
	limit := big.NewInt(int64(len(passwordAlphabet)))
	out := make([]byte, n)
	for i := range out {
		v, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", fmt.Errorf("bootstrap: bangkitkan password: %w", err)
		}
		out[i] = passwordAlphabet[v.Int64()]
	}
	pw := string(out)
	if err := b.policy.Check(pw); err != nil {
		return "", fmt.Errorf("bootstrap: password acak ditolak kebijakan: %w", err)
	}
	return pw, nil
}
