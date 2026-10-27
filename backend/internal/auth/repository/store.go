package repository

import (
	"context"

	"github.com/master-abror/zago-core/backend/internal/kernel"
)

// Store membungkus Queries (kode hasil sqlc) agar setiap panggilan memakai transaksi pada ctx
// bila ada (kernel.DBFrom), selain itu pool. Service menulis sekali dan berjalan di dalam
// maupun di luar TxManager.WithinTx — perubahan state + security event + audit dalam SATU
// transaksi (CLAUDE.md aturan kode).
//
//	st := repository.NewStore(pool)
//	row, err := st.Q(ctx).GetSessionByTokenHash(ctx, hash)
type Store struct {
	db kernel.DBTX
}

// NewStore membuat Store di atas pool (atau koneksi apa pun yang memenuhi kernel.DBTX).
func NewStore(db kernel.DBTX) *Store { return &Store{db: db} }

// Q mengembalikan Queries yang terikat ke transaksi pada ctx, atau ke pool bila tidak ada.
// Hasilnya tidak boleh disimpan melewati akhir transaksi: panggil Q(ctx) per operasi.
func (s *Store) Q(ctx context.Context) *Queries { return New(kernel.DBFrom(ctx, s.db)) }
