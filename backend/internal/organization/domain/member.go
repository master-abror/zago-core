package domain

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Status keanggotaan organisasi (docs/02 §9; CHECK pada migrasi 005). Keanggotaan yang
// dihapus tidak dihapus dari tabel: barisnya tetap ada dengan status removed (ADR-0023 butir 11).
const (
	// MemberInvited adalah keanggotaan yang diundang tetapi belum diterima.
	MemberInvited = "invited"

	// MemberActive adalah anggota aktif. Hanya status ini (bersama user aktif) yang dihitung
	// sebagai anggota oleh mesin otorisasi.
	MemberActive = "active"

	// MemberSuspended adalah keanggotaan yang ditangguhkan.
	MemberSuspended = "suspended"

	// MemberRemoved adalah keanggotaan yang dihapus secara logis.
	MemberRemoved = "removed"
)

// MaxCursorLength adalah panjang maksimum cursor daftar anggota yang diterima.
const MaxCursorLength = 200

// Galat domain anggota organisasi. Sentinel baru yang tidak didaftarkan di pemetaan galat HTTP
// jatuh ke 503 (ADR-0023 butir 14).
var (
	// ErrMemberNotFound menandai anggota yang tidak ada ATAU milik organisasi lain; pada
	// penghapusan, juga anggota yang sudah dihapus. Keadaan-keadaan itu sengaja tidak dapat
	// dibedakan (tidak membocorkan keberadaan lintas tenant).
	ErrMemberNotFound = errors.New("organization: anggota tidak ditemukan")

	// ErrMemberExists menandai pengguna yang sudah menjadi anggota (aktif, diundang, atau
	// ditangguhkan) organisasi itu.
	ErrMemberExists = errors.New("organization: pengguna sudah menjadi anggota")

	// ErrUserNotEligible menandai pengguna yang tidak ada, tidak aktif, atau sudah dihapus.
	// Ketiganya sengaja tidak dapat dibedakan.
	ErrUserNotEligible = errors.New("organization: pengguna tidak ditemukan atau tidak aktif")

	// ErrInvalidCursor menandai cursor daftar anggota yang rusak.
	ErrInvalidCursor = errors.New("organization: cursor tidak sah")
)

// Member adalah satu keanggotaan organisasi beserta identitas ringkas penggunanya. Alamat email
// sengaja tidak dimuat: email dibaca lewat jalur pengguna yang dilindungi field permission
// (ADR-0023 butir 12).
type Member struct {
	// ID adalah kunci baris keanggotaan.
	ID uuid.UUID

	// OrganizationID adalah organisasi pemilik keanggotaan.
	OrganizationID uuid.UUID

	// UserID adalah pengguna yang menjadi anggota.
	UserID uuid.UUID

	// Status adalah salah satu dari MemberInvited, MemberActive, MemberSuspended, MemberRemoved.
	Status string

	// JoinedAt adalah waktu bergabung; nil selama keanggotaan belum pernah aktif.
	JoinedAt *time.Time

	// InvitedBy adalah pengguna yang menambahkan atau mengundang; nil bila tidak diketahui.
	InvitedBy *uuid.UUID

	// CreatedAt adalah waktu pembuatan baris.
	CreatedAt time.Time

	// UpdatedAt adalah waktu perubahan terakhir.
	UpdatedAt time.Time

	// DisplayName adalah nama tampilan pengguna (users.display_name).
	DisplayName string

	// UserStatus adalah status akun pengguna (users.status), terpisah dari Status keanggotaan.
	UserStatus string
}

// NextCursor mengembalikan posisi baris ini sebagai cursor halaman berikutnya.
func (m Member) NextCursor() MemberCursor {
	return MemberCursor{CreatedAt: m.CreatedAt, ID: m.ID}
}

// MemberListQuery adalah permintaan daftar anggota satu organisasi.
type MemberListQuery struct {
	// Limit adalah ukuran halaman; selalu dipangkas ClampMemberLimit.
	Limit int

	// Cursor adalah cursor halaman berikutnya dari MemberPage.NextCursor; kosong = halaman awal.
	Cursor string

	// Filter adalah FilterOpen (bawaan bila kosong), FilterAll, atau salah satu status anggota.
	Filter string
}

// MemberPage adalah satu halaman daftar anggota.
type MemberPage struct {
	// Items adalah baris halaman ini; tidak pernah nil.
	Items []Member

	// NextCursor kosong bila tidak ada halaman berikutnya.
	NextCursor string
}

// ValidMemberStatus melaporkan apakah s adalah status keanggotaan yang dikenal.
func ValidMemberStatus(s string) bool {
	switch s {
	case MemberInvited, MemberActive, MemberSuspended, MemberRemoved:
		return true
	default:
		return false
	}
}

// MemberStatusesForFilter menerjemahkan filter daftar anggota ke himpunan status. Filter kosong
// sama dengan FilterOpen (semua selain removed); nilai tak dikenal tidak pernah menjadi
// "semua".
func MemberStatusesForFilter(filter string) ([]string, error) {
	switch filter {
	case "", FilterOpen:
		return []string{MemberInvited, MemberActive, MemberSuspended}, nil
	case FilterAll:
		return []string{MemberInvited, MemberActive, MemberSuspended, MemberRemoved}, nil
	case MemberInvited, MemberActive, MemberSuspended, MemberRemoved:
		return []string{filter}, nil
	default:
		return nil, ErrInvalidFilter
	}
}

// ClampMemberLimit memangkas ukuran halaman: nol atau negatif menjadi DefaultPageSize, di atas
// MaxPageSize menjadi MaxPageSize.
func ClampMemberLimit(limit int) int {
	if limit < 1 {
		return DefaultPageSize
	}
	if limit > MaxPageSize {
		return MaxPageSize
	}
	return limit
}

// MemberCursor adalah posisi pada urutan (created_at, id) daftar anggota. Ketelitiannya
// mikrodetik, sama dengan timestamptz PostgreSQL.
type MemberCursor struct {
	// CreatedAt adalah created_at baris terakhir halaman sebelumnya.
	CreatedAt time.Time

	// ID adalah id baris keanggotaan terakhir halaman sebelumnya.
	ID uuid.UUID
}

// Encode menghasilkan bentuk cursor untuk klien: base64url tanpa padding dari
// "<mikrodetik>.<uuid>". Bentuk ini buram bagi klien dan bukan kontrak.
func (c MemberCursor) Encode() string {
	raw := strconv.FormatInt(c.CreatedAt.UnixMicro(), 10) + "." + c.ID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeMemberCursor membaca cursor dari klien. String kosong berarti halaman awal (nil, nil).
// Bentuk apa pun selain hasil Encode ditolak dengan ErrInvalidCursor.
func DecodeMemberCursor(s string) (*MemberCursor, error) {
	if s == "" {
		return nil, nil
	}
	if len(s) > MaxCursorLength {
		return nil, ErrInvalidCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	micros, idText, ok := strings.Cut(string(raw), ".")
	if !ok {
		return nil, ErrInvalidCursor
	}
	us, err := strconv.ParseInt(micros, 10, 64)
	if err != nil || us < 0 || strconv.FormatInt(us, 10) != micros {
		return nil, ErrInvalidCursor
	}
	memberID, err := uuid.Parse(idText)
	if err != nil || memberID == uuid.Nil || memberID.String() != idText {
		return nil, ErrInvalidCursor
	}
	return &MemberCursor{CreatedAt: time.UnixMicro(us).UTC(), ID: memberID}, nil
}
