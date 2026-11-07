package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/kernel"
)

// Kelas kunci advisory untuk jalur tulis otorisasi (ADR-0022 butir 54). Kunci dua-int
// (kelas, objek) berada di ruang kunci yang terpisah dari kunci bigint tunggal milik seeder,
// bootstrap, dan Syncer, sehingga tidak mungkin bertabrakan dengan mereka.
const (
	// lockClassOrganization menjadi kelas kunci per organisasi; objeknya hash id organisasi.
	lockClassOrganization int32 = 0x7a61676f

	// lockClassPlatform menjadi kelas kunci tunggal untuk semua tulis scope platform.
	lockClassPlatform int32 = 0x7a61676e
)

// ErrNoTransaction menandai pemanggilan jalur tulis tanpa transaksi pada context: kunci advisory
// tingkat transaksi langsung lepas di luar transaksi, jadi tulis tanpa transaksi tidak diserialkan.
var ErrNoTransaction = errors.New("authorization: operasi tulis harus berjalan di dalam transaksi")

// LockOrganization mengambil kunci advisory tingkat transaksi untuk organisasi: semua tulis role,
// assignment, dan policy dalam satu organisasi diserialkan di sini, sehingga himpunan pemberi
// yang dihitung untuk authority ceiling tidak dapat berubah antara dihitung dan dipakai.
// Wajib dipanggil di dalam transaksi (kernel.TxManager.WithinTx); di luar transaksi kunci
// langsung dilepas dan tidak memberi perlindungan apa pun, jadi fungsi menolaknya (ErrNoTransaction).
// Tabrakan hash antar organisasi hanya menambah serialisasi, tidak pernah mengurangi isolasi.
func LockOrganization(ctx context.Context, organizationID uuid.UUID) error {
	if organizationID == uuid.Nil {
		return errors.New("authorization: kunci organisasi: id kosong")
	}
	tx, ok := kernel.TxFromContext(ctx)
	if !ok {
		return ErrNoTransaction
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1::int4, hashtext($2::text))",
		lockClassOrganization, organizationID.String()); err != nil {
		return fmt.Errorf("authorization: kunci organisasi: %w", err)
	}
	return nil
}

// LockPlatform mengambil kunci advisory tingkat transaksi untuk scope platform: menjaga hitungan
// super-admin aktif dan hitungan policy global agar pemeriksaan-lalu-tulis tidak berpacu.
// Aturan transaksi sama dengan LockOrganization.
func LockPlatform(ctx context.Context) error {
	tx, ok := kernel.TxFromContext(ctx)
	if !ok {
		return ErrNoTransaction
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1::int4, 0)", lockClassPlatform); err != nil {
		return fmt.Errorf("authorization: kunci platform: %w", err)
	}
	return nil
}
