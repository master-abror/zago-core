package permission

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidFieldDeclaration: kode memakai segmen `field` yang dicadangkan (kode field permission
// `<tipe>.field.<nama>.<aksi>`, ADR-0022 butir 35) tetapi aksinya bukan read atau write.
var ErrInvalidFieldDeclaration = errors.New("permission: deklarasi kode field tidak valid")

// fieldSegment adalah segmen yang dicadangkan pada posisi ketiga dari kanan.
const fieldSegment = "field"

// ValidateDeclaration memeriksa sebuah kode yang AKAN DIDEKLARASIKAN ke katalog (core lewat Sync,
// modul lewat registrasi di M07): bentuk kode (Split, galatnya membungkus ErrInvalidCode) dan
// aturan segmen `field` yang dicadangkan. Kode dengan segmen ketiga dari kanan `field` adalah kode
// field permission dan aksinya HARUS read atau write; aksi lain (export, delete, ...) ditolak
// dengan ErrInvalidFieldDeclaration supaya definisi field yang rusak tidak pernah masuk katalog.
// Pemuat FieldGuard tetap gagal-tertutup bila menemukan kode seperti itu (ADR-0022 butir 35);
// pemeriksaan ini mencegahnya masuk sejak awal. Kode dengan `field` di posisi lain bukan kode
// field dan tidak dibatasi.
func ValidateDeclaration(code string) error {
	if _, _, err := Split(code); err != nil {
		return err
	}
	parts := strings.Split(code, ".")
	n := len(parts)
	if n < 4 || parts[n-3] != fieldSegment {
		return nil
	}
	switch parts[n-1] {
	case "read", "write":
		return nil
	default:
		return fmt.Errorf("%w: aksi field harus read atau write: %q", ErrInvalidFieldDeclaration, truncate(code))
	}
}
