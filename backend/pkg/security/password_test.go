package security_test

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/pkg/security"
)

// Parameter kecil agar tes cepat; perilaku yang diuji tidak bergantung pada ukuran parameter.
var fast = security.Argon2Params{MemoryKiB: 64, Time: 1, Parallelism: 1}

func newHasher(t *testing.T, p security.Argon2Params) *security.Argon2idHasher {
	t.Helper()
	h, err := security.NewArgon2idHasher(p, 2)
	require.NoError(t, err)
	return h
}

func TestHashProducesPHCStringWithParameters(t *testing.T) {
	h := newHasher(t, fast)
	hash, err := h.Hash(context.Background(), "correct horse battery staple")
	require.NoError(t, err)

	// salt 16 byte -> 22 karakter, kunci 32 byte -> 43 karakter (base64 tanpa padding).
	re := regexp.MustCompile(`^\$argon2id\$v=19\$m=64,t=1,p=1\$[A-Za-z0-9+/]{22}\$[A-Za-z0-9+/]{43}$`)
	require.Regexp(t, re, hash, "hash harus berformat PHC dan membawa parameternya")
	require.NotContains(t, hash, "correct horse", "password tidak boleh muncul di hash")

	again, err := h.Hash(context.Background(), "correct horse battery staple")
	require.NoError(t, err)
	require.NotEqual(t, hash, again, "salt acak: dua hash dari password yang sama harus berbeda")
}

func TestCompare(t *testing.T) {
	h := newHasher(t, fast)
	ctx := context.Background()
	hash, err := h.Hash(ctx, "Sup3r-Secret-Passphrase")
	require.NoError(t, err)

	require.NoError(t, h.Compare(ctx, hash, "Sup3r-Secret-Passphrase"))

	for name, wrong := range map[string]string{
		"password salah":         "Sup3r-Secret-Passphrasf",
		"beda huruf besar-kecil": "sup3r-secret-passphrase",
		"kosong":                 "",
		"awalan saja":            "Sup3r-Secret",
		"ditambah spasi":         "Sup3r-Secret-Passphrase ",
	} {
		t.Run(name, func(t *testing.T) {
			err := h.Compare(ctx, hash, wrong)
			require.ErrorIs(t, err, security.ErrMismatch)
		})
	}
}

func TestHashWithOldParametersStillVerifiesAndNeedsRehash(t *testing.T) {
	ctx := context.Background()
	old := newHasher(t, security.Argon2Params{MemoryKiB: 64, Time: 1, Parallelism: 1})
	oldHash, err := old.Hash(ctx, "pw-lama-yang-cukup-panjang")
	require.NoError(t, err)

	for name, p := range map[string]security.Argon2Params{
		"memori naik":      {MemoryKiB: 128, Time: 1, Parallelism: 1},
		"waktu naik":       {MemoryKiB: 64, Time: 2, Parallelism: 1},
		"paralelisme naik": {MemoryKiB: 64, Time: 1, Parallelism: 2},
	} {
		t.Run(name, func(t *testing.T) {
			cur := newHasher(t, p)
			require.NoError(t, cur.Compare(ctx, oldHash, "pw-lama-yang-cukup-panjang"),
				"hash berparameter lama harus tetap terverifikasi")
			require.True(t, cur.NeedsRehash(oldHash), "parameter berubah -> perlu rehash")

			fresh, err := cur.Hash(ctx, "pw-lama-yang-cukup-panjang")
			require.NoError(t, err)
			require.False(t, cur.NeedsRehash(fresh), "hash dengan parameter terkini tidak perlu rehash")
		})
	}
	require.False(t, old.NeedsRehash(oldHash))
}

func TestMalformedHashesFailClosed(t *testing.T) {
	h := newHasher(t, fast)
	ctx := context.Background()
	good, err := h.Hash(ctx, "pw")
	require.NoError(t, err)

	cases := map[string]string{
		"kosong":                    "",
		"bukan PHC":                 "plaintext-password",
		"algoritma argon2i":         strings.Replace(good, "$argon2id$", "$argon2i$", 1),
		"versi lama":                strings.Replace(good, "v=19", "v=16", 1),
		"memori melewati batas":     strings.Replace(good, "m=64", "m=4294967295", 1),
		"waktu nol":                 strings.Replace(good, "t=1", "t=0", 1),
		"paralelisme nol":           strings.Replace(good, "p=1", "p=0", 1),
		"paralelisme terlalu besar": strings.Replace(good, "p=1", "p=250", 1),
		"urutan parameter":          strings.Replace(good, "m=64,t=1,p=1", "t=1,m=64,p=1", 1),
		"parameter hilang":          strings.Replace(good, "m=64,t=1,p=1", "m=64,t=1", 1),
		"parameter bertanda":        strings.Replace(good, "t=1", "t=+1", 1),
		"salt bukan base64":         strings.Replace(good, "$"+strings.Split(good, "$")[4]+"$", "$!!!!$", 1),
		"salt terlalu pendek":       strings.Replace(good, "$"+strings.Split(good, "$")[4]+"$", "$QUJD$", 1),
		"kunci terlalu pendek":      good[:strings.LastIndex(good, "$")+1] + "QUJD",
		"kunci kosong":              good[:strings.LastIndex(good, "$")+1],
		"segmen berlebih":           good + "$extra",
		"tanpa awalan $":            good[1:],
		"terlalu panjang":           good + strings.Repeat("A", 1000),
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			err := h.Compare(ctx, bad, "pw")
			require.ErrorIs(t, err, security.ErrInvalidHash)
			require.NotErrorIs(t, err, security.ErrMismatch,
				"hash rusak bukan 'password salah' — pemanggil harus bisa membedakannya")
			require.True(t, h.NeedsRehash(bad), "hash tak terbaca harus dianggap perlu diganti")
		})
	}
}

func TestPasswordLongerThanLimitIsRejected(t *testing.T) {
	h := newHasher(t, fast)
	ctx := context.Background()
	hash, err := h.Hash(ctx, "pw")
	require.NoError(t, err)

	atLimit := strings.Repeat("a", security.MaxPasswordBytes)
	tooLong := atLimit + "a"

	_, err = h.Hash(ctx, atLimit)
	require.NoError(t, err, "tepat di batas masih sah")

	_, err = h.Hash(ctx, tooLong)
	require.ErrorIs(t, err, security.ErrPasswordTooLong)
	require.ErrorIs(t, h.Compare(ctx, hash, tooLong), security.ErrPasswordTooLong)
	require.ErrorIs(t, h.CompareDummy(ctx, tooLong), security.ErrPasswordTooLong)
}

func TestCompareDummyNeverSucceeds(t *testing.T) {
	h := newHasher(t, fast)
	for _, pw := range []string{"", "x", "password-yang-cukup-panjang", strings.Repeat("z", 256)} {
		err := h.CompareDummy(context.Background(), pw)
		require.ErrorIs(t, err, security.ErrMismatch)
	}
}

func TestCanceledContextIsHonoured(t *testing.T) {
	h := newHasher(t, fast)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := h.Hash(ctx, "pw")
	require.True(t, errors.Is(err, context.Canceled), "got %v", err)
	require.ErrorIs(t, h.CompareDummy(ctx, "pw"), context.Canceled)
}

func TestNewArgon2idHasherRejectsUnsafeParameters(t *testing.T) {
	for name, p := range map[string]security.Argon2Params{
		"semua nol":             {},
		"memori terlalu kecil":  {MemoryKiB: 4, Time: 1, Parallelism: 1},
		"memori < 8*p":          {MemoryKiB: 15, Time: 1, Parallelism: 2},
		"waktu nol":             {MemoryKiB: 64, Time: 0, Parallelism: 1},
		"memori melewati batas": {MemoryKiB: 1<<20 + 1, Time: 1, Parallelism: 1},
		"paralelisme nol":       {MemoryKiB: 64, Time: 1, Parallelism: 0},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := security.NewArgon2idHasher(p, 1)
			require.ErrorIs(t, err, security.ErrInvalidParams)
		})
	}
	_, err := security.NewArgon2idHasher(security.DefaultArgon2Params(), 1)
	require.NoError(t, err, "parameter bawaan docs/05 §4.1 harus sah")
}

func TestDefaultParamsMatchDoc05(t *testing.T) {
	p := security.DefaultArgon2Params()
	require.Equal(t, security.Argon2Params{MemoryKiB: 64 * 1024, Time: 3, Parallelism: 2}, p)
}

func TestPasswordPolicy(t *testing.T) {
	p := security.PasswordPolicy{} // 0 -> bawaan 12
	require.ErrorIs(t, p.Check("pendek"), security.ErrPasswordTooShort)
	require.ErrorIs(t, p.Check("sebelas-aja"), security.ErrPasswordTooShort) // 11 karakter
	require.NoError(t, p.Check("dua-belas-ok"))                              // 12 karakter
	// panjang dihitung dalam karakter (rune), bukan byte: 12 huruf non-ASCII = 24 byte.
	require.NoError(t, p.Check(strings.Repeat("é", 12)))
	require.ErrorIs(t, p.Check(strings.Repeat("é", 11)), security.ErrPasswordTooShort)
	// batas atas dalam byte (docs/05 §4.1).
	require.NoError(t, p.Check(strings.Repeat("a", security.MaxPasswordBytes)))
	require.ErrorIs(t, p.Check(strings.Repeat("a", security.MaxPasswordBytes+1)), security.ErrPasswordTooLong)
	// 129 karakter 2-byte = 258 byte > 256 meski jumlah karakternya kecil.
	require.ErrorIs(t, p.Check(strings.Repeat("é", 129)), security.ErrPasswordTooLong)

	custom := security.PasswordPolicy{MinLength: 16}
	require.ErrorIs(t, custom.Check("dua-belas-ok"), security.ErrPasswordTooShort)
	require.NoError(t, custom.Check("enam-belas-karakt"))
}

func TestArgon2ParamsFromInts(t *testing.T) {
	p, err := security.Argon2ParamsFromInts(19456, 2, 1)
	require.NoError(t, err)
	require.Equal(t, security.Argon2Params{MemoryKiB: 19456, Time: 2, Parallelism: 1}, p)

	for name, in := range map[string][3]int{
		"negatif":                {-1, 1, 1},
		"melewati 8 bit":         {64, 1, 256},    // uint8(256) akan menjadi 0 bila dikonversi tanpa cek
		"melewati 32 bit":        {1 << 33, 1, 1}, // uint32 akan membungkus bila dikonversi tanpa cek
		"nol":                    {0, 0, 0},
		"memori < 8*paralelisme": {15, 1, 2},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := security.Argon2ParamsFromInts(in[0], in[1], in[2])
			require.ErrorIs(t, err, security.ErrInvalidParams)
		})
	}
}
