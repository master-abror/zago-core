package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/master-abror/zago-core/backend/internal/policy"
)

var (
	// ErrSimulationTargetNotFound menandai target simulasi yang tidak ada, bukan anggota aktif
	// organisasi pemanggil, tidak aktif, atau (untuk pemanggil level group) bukan anggota aktif
	// grup aktif pemanggil. Keadaan-keadaan itu sengaja tidak dibedakan: respons simulasi bukan
	// alat menebak id atau keanggotaan lintas organisasi (ADR-0022 butir 69).
	ErrSimulationTargetNotFound = errors.New("authorization: target simulasi tidak ditemukan")

	// ErrInvalidSimulationAttrs menandai atribut sumber daya simulasi yang bentuknya tidak sah:
	// terlalu banyak, nama bukan pengenal ekspresi, nilai bukan skalar atau daftar skalar, string
	// atau daftar melewati batas, atau angka tidak hingga.
	ErrInvalidSimulationAttrs = errors.New("authorization: atribut sumber daya simulasi tidak sah")

	// ErrReservedSimulationAttr menandai atribut yang memakai nama inti sumber daya (type, id,
	// organization_id, owner_id, group_id). Engine menimpa nama-nama itu dari ResourceRef, jadi
	// menerimanya diam-diam akan menyesatkan penanya.
	ErrReservedSimulationAttr = errors.New("authorization: nama atribut sumber daya simulasi dicadangkan")
)

const (
	// MaxSimulationAttrs membatasi jumlah atribut sumber daya pada satu simulasi.
	MaxSimulationAttrs = 32

	// MaxSimulationAttrKeyLen membatasi panjang nama atribut (byte).
	MaxSimulationAttrKeyLen = 64

	// MaxSimulationAttrStringBytes membatasi panjang satu nilai string (byte). Lebih ketat dari
	// policy.MaxAttrStringBytes supaya satu simulasi tidak dapat membawa muatan besar.
	MaxSimulationAttrStringBytes = 1024
)

// reservedSimulationAttrs adalah kunci inti yang ditimpa engine dari ResourceRef (policyEnv).
var reservedSimulationAttrs = map[string]struct{}{
	"type":            {},
	"id":              {},
	"organization_id": {},
	"owner_id":        {},
	"group_id":        {},
}

// NormalizeSimulationAttrs memeriksa atribut sumber daya yang dikirim penanya simulasi dan
// mengembalikan salinan baru (nilai daftar juga disalin), atau nil bila kosong. Aturan
// (gagal-tertutup; semuanya menolak): paling banyak MaxSimulationAttrs atribut; nama berbentuk
// pengenal ekspresi policy (huruf, angka, garis bawah; tidak diawali angka; maksimal
// MaxSimulationAttrKeyLen byte) dan bukan nama inti; nilai berupa null, bool, string
// (maksimal MaxSimulationAttrStringBytes byte), angka hingga, atau daftar skalar sebanyak-banyaknya
// policy.MaxListElements (tanpa daftar bersarang atau objek). Pesan galat tidak memuat nilai
// atau nama yang dikirim klien.
func NormalizeSimulationAttrs(attrs map[string]any) (map[string]any, error) {
	if len(attrs) == 0 {
		return nil, nil
	}
	if len(attrs) > MaxSimulationAttrs {
		return nil, fmt.Errorf("%w: lebih dari %d atribut", ErrInvalidSimulationAttrs, MaxSimulationAttrs)
	}
	out := make(map[string]any, len(attrs))
	for key, raw := range attrs {
		if !validSimulationAttrKey(key) {
			return nil, fmt.Errorf("%w: nama atribut", ErrInvalidSimulationAttrs)
		}
		if _, reserved := reservedSimulationAttrs[key]; reserved {
			return nil, ErrReservedSimulationAttr
		}
		v, err := normalizeSimulationAttrValue(raw)
		if err != nil {
			return nil, err
		}
		out[key] = v
	}
	return out, nil
}

// validSimulationAttrKey mengikuti pengenal pada leksikon ekspresi policy: huruf ASCII atau garis
// bawah di awal, lalu huruf, angka, atau garis bawah.
func validSimulationAttrKey(key string) bool {
	if key == "" || len(key) > MaxSimulationAttrKeyLen {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		letter := c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		digit := c >= '0' && c <= '9'
		if !letter && (i == 0 || !digit) {
			return false
		}
	}
	return true
}

func normalizeSimulationAttrValue(raw any) (any, error) {
	list, isList := raw.([]any)
	if !isList {
		return normalizeSimulationScalar(raw)
	}
	if len(list) > policy.MaxListElements {
		return nil, fmt.Errorf("%w: daftar lebih dari %d elemen", ErrInvalidSimulationAttrs, policy.MaxListElements)
	}
	out := make([]any, 0, len(list))
	for _, item := range list {
		if _, nested := item.([]any); nested {
			return nil, fmt.Errorf("%w: daftar bersarang", ErrInvalidSimulationAttrs)
		}
		v, err := normalizeSimulationScalar(item)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func normalizeSimulationScalar(raw any) (any, error) {
	switch x := raw.(type) {
	case nil:
		return nil, nil
	case bool:
		return x, nil
	case string:
		if len(x) > MaxSimulationAttrStringBytes {
			return nil, fmt.Errorf("%w: string lebih dari %d byte", ErrInvalidSimulationAttrs, MaxSimulationAttrStringBytes)
		}
		return x, nil
	case int:
		return int64(x), nil
	case int64:
		return x, nil
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil, fmt.Errorf("%w: angka tidak hingga", ErrInvalidSimulationAttrs)
		}
		return x, nil
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i, nil
		}
		f, err := x.Float64()
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("%w: angka tidak sah", ErrInvalidSimulationAttrs)
		}
		return f, nil
	}
	return nil, fmt.Errorf("%w: tipe nilai %T", ErrInvalidSimulationAttrs, raw)
}
