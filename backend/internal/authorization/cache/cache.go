// Package cache menyimpan effective permissions di Redis dengan kunci berversi (docs/06 §8,
// ADR-0022 butir 3). Cache hanyalah salinan: PostgreSQL tetap sumber kebenaran, dan setiap galat
// Redis berarti "tidak ada cache" (hitung dari PostgreSQL), tidak pernah jawaban basi atau
// default-allow (docs/06 §8.2).
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel"
)

// Kunci Redis. Versi kunci efektif = pasangan (versi organisasi, versi platform); mengubah data
// platform (mis. role sistem) menaikkan penghitung platform dan mematikan semua kunci organisasi.
const keyVersionPrefix = "authz:ver:" // diikuti id organisasi

const keyVersionPlatform = "authz:ver:platform"

const keyEffectivePrefix = "authz:eff:"

// payloadVersion adalah versi format nilai yang disimpan; nilai berformat lain dibuang.
const payloadVersion = 1

// defaultTTL adalah jaring pengaman, bukan mekanisme utama (docs/06 §8.1).
const defaultTTL = 2 * time.Minute

// defaultTimeout membatasi satu operasi baca/tulis cache (ADR-0019).
const defaultTimeout = 250 * time.Millisecond

// defaultBackoff: setelah galat Redis, request langsung ke PostgreSQL selama ini.
const defaultBackoff = time.Second

// bumpTimeout dan afterCommitWait: invalidasi lebih penting daripada kecepatan.
const bumpTimeout = 2 * time.Second

const afterCommitWait = 2 * time.Second

// bumpScript menaikkan penghitung versi. Bila kunci hilang (mis. tergusur Redis), versi baru
// dimulai dari jam (ms epoch) +1 sehingga tidak pernah bertabrakan dengan versi lama yang mungkin
// masih punya nilai di bawahnya.
var bumpScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 1 then
  return redis.call('INCR', KEYS[1])
end
redis.call('SET', KEYS[1], ARGV[1])
return tonumber(ARGV[1])
`)

// Options mengatur Cache. Nilai nol berarti default.
type Options struct {
	TTL     time.Duration
	Timeout time.Duration
	Backoff time.Duration
	Now     func() time.Time
	Log     *slog.Logger
}

// Cache adalah cache effective permissions berversi.
type Cache struct {
	rdb     redis.UniversalClient
	ttl     time.Duration
	timeout time.Duration
	backoff time.Duration
	now     func() time.Time
	log     *slog.Logger

	downUntil atomic.Int64 // UnixNano; selama belum lewat, Redis dilewati
}

// New membuat Cache di atas rdb.
func New(rdb redis.UniversalClient, opts Options) *Cache {
	c := &Cache{rdb: rdb, ttl: opts.TTL, timeout: opts.Timeout, backoff: opts.Backoff, now: opts.Now, log: opts.Log}
	if c.ttl <= 0 {
		c.ttl = defaultTTL
	}
	if c.timeout <= 0 {
		c.timeout = defaultTimeout
	}
	if c.backoff <= 0 {
		c.backoff = defaultBackoff
	}
	if c.now == nil {
		c.now = time.Now
	}
	if c.log == nil {
		c.log = slog.New(slog.DiscardHandler)
	}
	return c
}

// Lookup adalah hasil pencarian. Key kosong berarti cache tidak tersedia: hitung dari PostgreSQL
// dan JANGAN menulis. Key terisi dengan Hit=false berarti miss yang boleh diisi lewat Store
// (kunci memuat versi yang dibaca SEBELUM menghitung, jadi hasil yang dihitung sesudah perubahan
// data tidak pernah disimpan di bawah versi yang lebih baru dari datanya).
type Lookup struct {
	Effective domain.EffectivePermissions
	Hit       bool
	Key       string
}

// Lookup mencari effective permissions userID pada scope. Tidak pernah mengembalikan galat.
func (c *Cache) Lookup(ctx context.Context, userID uuid.UUID, scope domain.ScopeContext) Lookup {
	if c.isDown() {
		return Lookup{}
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	orgVer, platVer, err := c.versions(ctx, scope.OrganizationID)
	if err != nil {
		c.fail("baca versi", err)
		return Lookup{}
	}
	key := effectiveKey(userID, scope, orgVer, platVer)

	raw, err := c.rdb.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return Lookup{Key: key}
	}
	if err != nil {
		c.fail("baca cache", err)
		return Lookup{}
	}
	eff, ok := decode(raw)
	if !ok {
		return Lookup{Key: key} // nilai rusak atau format lain: anggap miss dan timpa
	}
	return Lookup{Effective: eff, Hit: true, Key: key}
}

// Store menyimpan eff di bawah kunci hasil Lookup. Upaya terbaik: galat hanya dicatat.
func (c *Cache) Store(ctx context.Context, l Lookup, eff domain.EffectivePermissions) {
	if l.Key == "" || c.isDown() {
		return
	}
	payload, err := encode(eff)
	if err != nil {
		c.log.Error("authz cache: encode gagal", "error", err)
		return
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	if err := c.rdb.Set(ctx, l.Key, payload, c.ttl).Err(); err != nil {
		c.fail("tulis cache", err)
	}
}

// Targets menyebut penghitung versi yang harus dinaikkan.
type Targets struct {
	Platform      bool
	Organizations []uuid.UUID
}

// BumpOrganization menaikkan versi organisasi. Galat dikembalikan (pemanggil memutuskan).
func (c *Cache) BumpOrganization(ctx context.Context, organizationID uuid.UUID) error {
	if organizationID == uuid.Nil {
		return errors.New("authorization cache: organisasi kosong")
	}
	return c.bump(ctx, keyVersionPrefix+organizationID.String())
}

// BumpPlatform menaikkan versi platform (menginvalidasi semua organisasi).
func (c *Cache) BumpPlatform(ctx context.Context) error {
	return c.bump(ctx, keyVersionPlatform)
}

// InvalidateInTx dipanggil DI DALAM transaksi pengubah, SESUDAH perubahan data ditulis
// (ADR-0022 butir 3, docs/06 §8.1): menaikkan versi sekarang (sebelum commit; bila gagal galat
// dikembalikan sehingga transaksi dibatalkan, fail-closed) dan sekali lagi tepat setelah commit
// lewat kernel.AfterCommit. Kegagalan bump sesudah commit hanya dicatat: perubahan sudah
// permanen, dan entri basi yang mungkin tersisa dibatasi TTL. Tanpa transaksi pada ctx kedua
// bump berjalan langsung.
func (c *Cache) InvalidateInTx(ctx context.Context, t Targets) error {
	keys := t.keys()
	for _, k := range keys {
		if err := c.bump(ctx, k); err != nil {
			return err
		}
	}
	if len(keys) == 0 {
		return nil
	}
	kernel.AfterCommit(ctx, func() {
		post, cancel := context.WithTimeout(context.WithoutCancel(ctx), afterCommitWait)
		defer cancel()
		for _, k := range keys {
			if err := c.bump(post, k); err != nil {
				c.log.Error("authz cache: invalidasi sesudah commit gagal; entri basi dibatasi TTL",
					"key", k, "ttl", c.ttl.String(), "error", err)
			}
		}
	})
	return nil
}

func (t Targets) keys() []string {
	var out []string
	if t.Platform {
		out = append(out, keyVersionPlatform)
	}
	seen := map[uuid.UUID]bool{}
	for _, o := range t.Organizations {
		if o == uuid.Nil || seen[o] {
			continue
		}
		seen[o] = true
		out = append(out, keyVersionPrefix+o.String())
	}
	return out
}

func (c *Cache) bump(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bumpTimeout)
	defer cancel()
	next := c.now().UnixMilli() + 1
	if err := bumpScript.Run(ctx, c.rdb, []string{key}, next).Err(); err != nil {
		return fmt.Errorf("authorization cache: naikkan versi %s: %w", key, err)
	}
	return nil
}

// versions membaca versi organisasi (0 bila scope tanpa organisasi) dan platform. Penghitung yang
// belum ada diinisialisasi dari jam (ms epoch) dengan SET NX agar versi baru tidak bertabrakan
// dengan versi yang pernah dipakai sebelum kunci hilang.
func (c *Cache) versions(ctx context.Context, org *uuid.UUID) (orgVer, platVer int64, err error) {
	if org != nil {
		if orgVer, err = c.version(ctx, keyVersionPrefix+org.String()); err != nil {
			return 0, 0, err
		}
	}
	if platVer, err = c.version(ctx, keyVersionPlatform); err != nil {
		return 0, 0, err
	}
	return orgVer, platVer, nil
}

func (c *Cache) version(ctx context.Context, key string) (int64, error) {
	s, err := c.rdb.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		if err = c.rdb.SetNX(ctx, key, c.now().UnixMilli(), 0).Err(); err != nil {
			return 0, err
		}
		s, err = c.rdb.Get(ctx, key).Result()
	}
	if err != nil {
		return 0, err
	}
	v, perr := strconv.ParseInt(s, 10, 64)
	if perr != nil || v <= 0 {
		return 0, fmt.Errorf("versi %s tidak sah", key)
	}
	return v, nil
}

func effectiveKey(userID uuid.UUID, scope domain.ScopeContext, orgVer, platVer int64) string {
	org, grp := "-", "-"
	if scope.OrganizationID != nil {
		org = scope.OrganizationID.String()
	}
	if scope.ActiveGroupID != nil {
		grp = scope.ActiveGroupID.String()
	}
	return keyEffectivePrefix + org + ":" + strconv.FormatInt(orgVer, 10) + ":" + strconv.FormatInt(platVer, 10) +
		":" + userID.String() + ":" + grp
}

type payload struct {
	V      int                             `json:"v"`
	Grants map[string]domain.DecisionLevel `json:"g"`
}

func encode(eff domain.EffectivePermissions) ([]byte, error) {
	return json.Marshal(payload{V: payloadVersion, Grants: eff.Grants()})
}

// decode memvalidasi lewat EffectiveFromGrants: kode atau level tak sah menolak seluruh nilai.
func decode(raw []byte) (domain.EffectivePermissions, bool) {
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil || p.V != payloadVersion {
		return domain.EffectivePermissions{}, false
	}
	eff, err := domain.EffectiveFromGrants(p.Grants)
	if err != nil {
		return domain.EffectivePermissions{}, false
	}
	return eff, true
}

func (c *Cache) isDown() bool { return c.now().UnixNano() < c.downUntil.Load() }

// fail menandai Redis tidak sehat selama backoff supaya request berikutnya tidak menunggu
// timeout lagi, dan mencatat sekali per peralihan sehat → tidak sehat.
func (c *Cache) fail(op string, err error) {
	now := c.now().UnixNano()
	prev := c.downUntil.Swap(now + c.backoff.Nanoseconds())
	if prev <= now {
		c.log.Warn("authz cache: Redis bermasalah; menghitung dari PostgreSQL", "op", op, "error", err)
	}
}
