package domain

import (
	"errors"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/permission"
)

// Batas masukan role (ADR-0022 butir 53). Batas ini melindungi database dan audit dari masukan
// yang tidak masuk akal; bukan batas bisnis yang dinegosiasikan.
const (
	// MaxRoleNameLen adalah panjang maksimum nama role dalam karakter (roles.name varchar(150)).
	MaxRoleNameLen = 150

	// MaxRoleSlugLen adalah panjang maksimum slug role (roles.slug varchar(100)).
	MaxRoleSlugLen = 100

	// MaxRoleDescriptionLen adalah panjang maksimum deskripsi role dalam karakter.
	MaxRoleDescriptionLen = 1000

	// MaxRolePermissions adalah jumlah maksimum permission berbeda pada satu role.
	MaxRolePermissions = 500
)

var (
	// ErrInvalidRoleName menandai nama role yang kosong, terlalu panjang, atau memuat karakter
	// kontrol.
	ErrInvalidRoleName = errors.New("authorization: nama role tidak sah")

	// ErrInvalidRoleSlug menandai slug yang bukan huruf kecil, angka, dan tanda hubung tunggal di
	// antara segmen (tanpa spasi, garis bawah, atau tanda hubung di ujung).
	ErrInvalidRoleSlug = errors.New("authorization: slug role tidak sah")

	// ErrReservedRoleSlug menandai slug yang sama dengan slug role sistem. Role non-sistem tidak
	// boleh meminjam nama role sistem: administrator yang membaca daftar role akan tertipu.
	ErrReservedRoleSlug = errors.New("authorization: slug role dicadangkan untuk role sistem")

	// ErrInvalidRoleDescription menandai deskripsi yang terlalu panjang atau memuat karakter
	// kontrol selain baris baru dan tab.
	ErrInvalidRoleDescription = errors.New("authorization: deskripsi role tidak sah")

	// ErrTooManyPermissions menandai himpunan permission role yang melebihi MaxRolePermissions.
	ErrTooManyPermissions = errors.New("authorization: terlalu banyak permission pada satu role")

	// ErrRoleNotFound menandai role yang tidak ada ATAU bukan milik organisasi pemanggil. Kedua
	// keadaan sengaja tidak dibedakan (tidak membocorkan keberadaan lintas tenant).
	ErrRoleNotFound = errors.New("authorization: role tidak ditemukan")

	// ErrRoleSlugTaken menandai slug role yang sudah dipakai di organisasi (atau grup) itu.
	ErrRoleSlugTaken = errors.New("authorization: slug role sudah dipakai")

	// ErrUnknownPermission menandai kode permission berbentuk sah yang tidak ada di tabel
	// permissions. Role tidak boleh merujuk permission yang tidak terdaftar.
	ErrUnknownPermission = errors.New("authorization: permission tidak terdaftar")

	// ErrGroupNotFound menandai grup yang tidak ada, bukan milik organisasi pemanggil, atau tidak
	// aktif. Seperti ErrRoleNotFound, keadaan-keadaan itu tidak dibedakan.
	ErrGroupNotFound = errors.New("authorization: grup tidak ditemukan")
)

// roleStatusArchived adalah nilai roles.status untuk role yang diarsipkan.
const roleStatusArchived = "archived"

// RoleRecord adalah satu baris roles seperti dibaca dan ditulis jalur tulis role.
type RoleRecord struct {
	ID             uuid.UUID
	OrganizationID *uuid.UUID
	GroupID        *uuid.UUID
	Name           string
	Slug           string
	Description    string
	Type           string
	Status         string
}

// Ref mengubah record menjadi RoleRef untuk aturan domain (CheckRoleMutable dan sejenisnya).
func (r RoleRecord) Ref() RoleRef {
	return RoleRef{
		Slug:           r.Slug,
		Type:           r.Type,
		OrganizationID: r.OrganizationID,
		GroupID:        r.GroupID,
		Archived:       r.Status == roleStatusArchived,
	}
}

var roleSlugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// RoleDraft adalah masukan nama, slug, dan deskripsi sebuah role sebelum dinormalisasi.
type RoleDraft struct {
	Name        string
	Slug        string
	Description string
}

// NormalizeRoleDraft memvalidasi dan menormalkan masukan role: nama dan deskripsi dipangkas
// spasi ujungnya; slug TIDAK diubah (harus sudah huruf kecil, supaya tidak ada dua slug yang tampak
// sama). Slug yang sama dengan slug role sistem ditolak.
func NormalizeRoleDraft(d RoleDraft) (RoleDraft, error) {
	name := strings.TrimSpace(d.Name)
	if name == "" || utf8.RuneCountInString(name) > MaxRoleNameLen || hasForbiddenRunes(name, false) {
		return RoleDraft{}, ErrInvalidRoleName
	}
	if d.Slug == "" || len(d.Slug) > MaxRoleSlugLen || !roleSlugPattern.MatchString(d.Slug) {
		return RoleDraft{}, ErrInvalidRoleSlug
	}
	if isSystemRoleSlug(d.Slug) {
		return RoleDraft{}, ErrReservedRoleSlug
	}
	desc := strings.TrimSpace(d.Description)
	if utf8.RuneCountInString(desc) > MaxRoleDescriptionLen || hasForbiddenRunes(desc, true) {
		return RoleDraft{}, ErrInvalidRoleDescription
	}
	return RoleDraft{Name: name, Slug: d.Slug, Description: desc}, nil
}

// NormalizeRoleText memvalidasi nama dan deskripsi saja (perubahan role yang sudah ada; slug
// tidak dapat diubah). Aturannya sama dengan NormalizeRoleDraft.
func NormalizeRoleText(name, description string) (string, string, error) {
	n := strings.TrimSpace(name)
	if n == "" || utf8.RuneCountInString(n) > MaxRoleNameLen || hasForbiddenRunes(n, false) {
		return "", "", ErrInvalidRoleName
	}
	desc := strings.TrimSpace(description)
	if utf8.RuneCountInString(desc) > MaxRoleDescriptionLen || hasForbiddenRunes(desc, true) {
		return "", "", ErrInvalidRoleDescription
	}
	return n, desc, nil
}

func isSystemRoleSlug(slug string) bool {
	switch slug {
	case RoleSlugSuperAdmin, RoleSlugOrgAdmin, RoleSlugGroupAdmin, RoleSlugMember:
		return true
	}
	return false
}

// hasForbiddenRunes melaporkan UTF-8 tidak sah atau karakter kontrol. Bila allowLayout, baris
// baru, carriage return, dan tab diizinkan (untuk deskripsi).
func hasForbiddenRunes(s string, allowLayout bool) bool {
	if !utf8.ValidString(s) {
		return true
	}
	for _, r := range s {
		if !unicode.IsControl(r) {
			continue
		}
		if allowLayout && (r == '\n' || r == '\r' || r == '\t') {
			continue
		}
		return true
	}
	return false
}

// NormalizePermissionCodes memvalidasi bentuk kode permission yang diminta untuk sebuah role,
// membuang duplikat, dan mengurutkannya. Kode kosong, berspasi di ujung, terlalu panjang, atau
// salah bentuk memberi ErrInvalidGrant; lebih dari MaxRolePermissions kode berbeda memberi
// ErrTooManyPermissions. Keberadaan kode di tabel permissions diperiksa pemanggil di database.
// Hasilnya irisan baru (non-nil); masukan tidak diubah.
func NormalizePermissionCodes(codes []string) ([]string, error) {
	seen := make(map[string]struct{}, len(codes))
	out := make([]string, 0, len(codes))
	for _, c := range codes {
		if c == "" || c != strings.TrimSpace(c) || len(c) > maxCodeLen {
			return nil, ErrInvalidGrant
		}
		if _, _, err := permission.Split(c); err != nil {
			return nil, ErrInvalidGrant
		}
		if _, dup := seen[c]; dup {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
		if len(out) > MaxRolePermissions {
			return nil, ErrTooManyPermissions
		}
	}
	sort.Strings(out)
	return out, nil
}
