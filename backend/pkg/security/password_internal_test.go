package security

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var fastParams = Argon2Params{MemoryKiB: 64, Time: 1, Parallelism: 1}

func newTestHasher(t *testing.T, concurrency int) *Argon2idHasher {
	t.Helper()
	h, err := NewArgon2idHasher(fastParams, concurrency)
	require.NoError(t, err)
	return h
}

// Password terlalu panjang ditolak SEBELUM menunggu slot semaphore, apalagi sebelum hashing
// (docs/05 §4.1): bila semua slot terisi, panggilan tetap kembali segera dengan
// ErrPasswordTooLong, bukan menunggu sampai context habis.
func TestTooLongPasswordRejectedBeforeSemaphore(t *testing.T) {
	h := newTestHasher(t, 1)
	hash, err := h.Hash(context.Background(), "pw")
	require.NoError(t, err)

	h.sem <- struct{}{} // satu-satunya slot terisi
	t.Cleanup(func() { <-h.sem })

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	tooLong := strings.Repeat("a", MaxPasswordBytes+1)

	_, err = h.Hash(ctx, tooLong)
	require.ErrorIs(t, err, ErrPasswordTooLong)
	require.ErrorIs(t, h.Compare(ctx, hash, tooLong), ErrPasswordTooLong)
	require.ErrorIs(t, h.CompareDummy(ctx, tooLong), ErrPasswordTooLong)
}

// Menunggu slot menghormati context: login yang membanjir tidak menumpuk goroutine tanpa batas.
func TestWaitingForSlotHonoursContext(t *testing.T) {
	h := newTestHasher(t, 1)
	h.sem <- struct{}{}
	t.Cleanup(func() { <-h.sem })

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := h.Hash(ctx, "password-yang-cukup")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorIs(t, h.CompareDummy(ctx, "password-yang-cukup"), context.DeadlineExceeded)
}

// Konkurensi hashing dibatasi semaphore (docs/05 §4.1, 64 MiB x request serentak). Hook
// onDerive berjalan DI DALAM bagian kritis; tidur singkat di dalamnya hanya memperlebar jendela
// agar batas atas benar-benar teruji — properti yang diuji (max <= batas) tidak bergantung waktu.
func TestHashingConcurrencyIsBounded(t *testing.T) {
	const limit = 2
	h := newTestHasher(t, limit)
	hash, err := h.Hash(context.Background(), "pw-bagi-semua")
	require.NoError(t, err)

	var inFlight, maxInFlight atomic.Int32
	h.onDerive = func() {
		cur := inFlight.Add(1)
		for {
			m := maxInFlight.Load()
			if cur <= m || maxInFlight.CompareAndSwap(m, cur) {
				break
			}
		}
		time.Sleep(3 * time.Millisecond)
		inFlight.Add(-1)
	}

	var wg sync.WaitGroup
	for range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = h.Compare(context.Background(), hash, "salah-salah")
		}()
	}
	wg.Wait()

	require.LessOrEqual(t, int(maxInFlight.Load()), limit, "hashing serentak melewati batas semaphore")
	require.GreaterOrEqual(t, int(maxInFlight.Load()), 1)
	require.Zero(t, inFlight.Load())
}

// Email tak dikenal harus menempuh jalur yang sama dengan password salah (docs/05 §4.1, I-04):
// satu derivasi Argon2id penuh di bagian kritis yang sama, dengan parameter yang sama.
func TestDummyTakesSameGuardedPathAsWrongPassword(t *testing.T) {
	h := newTestHasher(t, 2)
	hash, err := h.Hash(context.Background(), "pw-asli-pengguna")
	require.NoError(t, err)

	var derives atomic.Int32
	h.onDerive = func() { derives.Add(1) }

	require.ErrorIs(t, h.Compare(context.Background(), hash, "pw-salah-sekali"), ErrMismatch)
	require.Equal(t, int32(1), derives.Load(), "password salah: tepat satu derivasi")

	derives.Store(0)
	require.ErrorIs(t, h.CompareDummy(context.Background(), "pw-salah-sekali"), ErrMismatch)
	require.Equal(t, int32(1), derives.Load(), "dummy: tepat satu derivasi, di bagian kritis yang sama")
}

func TestDummyHashUsesCurrentParametersAndIsNotConstantAcrossHashers(t *testing.T) {
	h1 := newTestHasher(t, 1)
	h2 := newTestHasher(t, 1)

	p, salt, key, err := parsePHC(h1.dummy)
	require.NoError(t, err)
	require.Equal(t, h1.params, p, "dummy memakai parameter yang sama dengan hash sungguhan")
	require.Len(t, salt, saltLen)
	require.Len(t, key, keyLen)
	require.False(t, h1.NeedsRehash(h1.dummy))

	require.NotEqual(t, h1.dummy, h2.dummy, "dummy dibuat acak per hasher, tidak ada nilai tetap di kode")
}

// Hash yang dibentuk lawan (mis. baris rusak) tidak boleh memicu alokasi memori raksasa.
func TestParsePHCRejectsOversizedParametersWithoutDeriving(t *testing.T) {
	h := newTestHasher(t, 1)
	good, err := h.Hash(context.Background(), "pw")
	require.NoError(t, err)
	var derives atomic.Int32
	h.onDerive = func() { derives.Add(1) }

	bad := strings.Replace(good, "m=64", "m=1048577", 1) // 1 GiB + 1 KiB
	err = h.Compare(context.Background(), bad, "pw")
	require.True(t, errors.Is(err, ErrInvalidHash), "got %v", err)
	require.Zero(t, derives.Load(), "parameter di luar batas ditolak sebelum derivasi")
}
