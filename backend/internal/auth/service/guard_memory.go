package service

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// memGuard adalah fallback penjaga login saat Redis tak tersedia (ADR-0020 butir 51): semantik sama
// dengan skrip Redis, tetapi per proses. Tabel dibatasi max identitas; jendela yang sudah berakhir
// dibersihkan lebih dulu, dan hanya bila tabel masih penuh oleh entri AKTIF identitas baru ditolak
// (gagal tertutup; entri aktif tak pernah diusir). Kunci dan strike yang dipasang untuk akun yang
// sudah terlacak boleh melampaui max sedikit (dibersihkan saat berakhir).
type memGuard struct {
	mu  sync.Mutex
	max int
	m   map[string]memEntry
}

type memEntry struct {
	n   int
	exp time.Time
}

func newMemGuard(limit int) *memGuard { return &memGuard{max: limit, m: make(map[string]memEntry)} }

func (g *memGuard) purgeLocked(now time.Time) {
	for k, e := range g.m {
		if !e.exp.After(now) {
			delete(g.m, k)
		}
	}
}

// roomLocked: kunci bisa dilacak (sudah ada, atau ada tempat setelah pembersihan).
func (g *memGuard) roomLocked(now time.Time, key string) bool {
	if _, ok := g.m[key]; ok {
		return true
	}
	if len(g.m) < g.max {
		return true
	}
	g.purgeLocked(now)
	return len(g.m) < g.max
}

// retryWhenFullLocked: perkiraan menunggu sampai ada entri yang berakhir (minimal 1 detik).
func (g *memGuard) retryWhenFullLocked(now time.Time) time.Duration {
	wait := time.Duration(0)
	for _, e := range g.m {
		if d := e.exp.Sub(now); wait == 0 || d < wait {
			wait = d
		}
	}
	return max(wait, time.Second)
}

func (g *memGuard) ipBlocked(now time.Time, ip string, maxFailures int) (time.Duration, bool) {
	key := keyIPFail + ip
	g.mu.Lock()
	defer g.mu.Unlock()
	if e, ok := g.m[key]; ok {
		if e.exp.After(now) && e.n >= maxFailures {
			return e.exp.Sub(now), true
		}
		return 0, false
	}
	if !g.roomLocked(now, key) {
		return g.retryWhenFullLocked(now), true
	}
	return 0, false
}

func (g *memGuard) accountLocked(now time.Time, userID uuid.UUID) bool {
	uid := userID.String()
	g.mu.Lock()
	defer g.mu.Unlock()
	if e, ok := g.m[keyLock+uid]; ok && e.exp.After(now) {
		return true
	}
	// Akun belum terlacak dan tabel penuh oleh entri aktif: gagal tertutup (identitas baru ditolak).
	if _, ok := g.m[keyFail+uid]; !ok && !g.roomLocked(now, keyFail+uid) {
		return true
	}
	return false
}

func (g *memGuard) ipFailure(now time.Time, ip string, p GuardPolicy) {
	key := keyIPFail + ip
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.roomLocked(now, key) {
		return
	}
	e := g.m[key]
	if !e.exp.After(now) {
		e = memEntry{exp: now.Add(p.FailureWindow)}
	}
	e.n++
	g.m[key] = e
}

func (g *memGuard) recordFailure(now time.Time, userID uuid.UUID, p GuardPolicy) FailureResult {
	uid := userID.String()
	lk, fk, sk := keyLock+uid, keyFail+uid, keyStrike+uid
	g.mu.Lock()
	defer g.mu.Unlock()

	if e, ok := g.m[lk]; ok && e.exp.After(now) {
		return FailureResult{} // kunci aktif: tak dihitung, tak memperpanjang
	}
	if !g.roomLocked(now, fk) {
		return FailureResult{} // tak terlacak; accountLocked sudah gagal tertutup untuk identitas ini
	}
	e := g.m[fk]
	if !e.exp.After(now) {
		e = memEntry{exp: now.Add(p.FailureWindow)}
	}
	e.n++
	if e.n < p.MaxFailures {
		g.m[fk] = e
		return FailureResult{Failures: e.n}
	}

	s := g.m[sk]
	if !s.exp.After(now) {
		s = memEntry{}
	}
	s.n++
	s.exp = now.Add(guardStrikeTTL)
	dur := lockDuration(p, s.n)
	g.m[lk] = memEntry{n: 1, exp: now.Add(dur)}
	g.m[sk] = s
	delete(g.m, fk)
	return FailureResult{Failures: e.n, Locked: true, LockFor: dur, Strikes: s.n}
}

func (g *memGuard) success(userID uuid.UUID) {
	uid := userID.String()
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.m, keyFail+uid)
	delete(g.m, keyStrike+uid)
}
