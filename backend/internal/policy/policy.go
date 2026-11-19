// Package policy adalah bahasa ekspresi policy (docs/06 §6, ADR-0022 butir 31–33): tokenizer dan
// parser tulis tangan dengan daftar putih fungsi. Tidak ada eval, reflection, maupun pemanggilan
// metode: ekspresi hanya dapat membaca nilai lewat Env dan menghasilkan true/false.
//
// Tata bahasa (prioritas dari rendah ke tinggi):
//
//	ekspr   := atau
//	atau    := dan ( "||" dan )*
//	dan     := banding ( "&&" banding )*
//	banding := unary ( ("==" | "!=" | "<" | "<=" | ">" | ">=" | "in") unary )?   // tidak berantai
//	unary   := "!" unary | primer
//	primer  := string | angka | "-" angka | true | false | null | daftar
//	         | akar "." atribut | fungsi "(" [ ekspr ( "," ekspr )* ] ")" | "(" ekspr ")"
//	daftar  := "[" [ ekspr ( "," ekspr )* ] "]"
//	akar    := resource | current_user | scope
//
// Semua penolakan bersifat gagal-tertutup: galat evaluasi tidak pernah menjadi true.
package policy

import (
	"errors"
	"fmt"
)

// Batas (docs/06 §6 butir 1: panjang, kedalaman, dan langkah evaluasi dibatasi).
const (
	MaxExpressionBytes = 2000 // panjang ekspresi sumber
	MaxTokens          = 400  // jumlah token
	MaxDepth           = 24   // kedalaman tanda kurung, "!", daftar, dan panggilan fungsi
	MaxSteps           = 1000 // langkah evaluasi (simpul dan perbandingan elemen daftar)
	MaxStringBytes     = 256  // string literal (setelah escape didekode)
	MaxIdentBytes      = 64   // pengenal
	MaxNumberBytes     = 24   // teks angka
	MaxListElements    = 64   // elemen daftar (literal maupun atribut)
	MaxAttrStringBytes = 4096 // string atribut yang dibaca dari Env
)

// Akar nama yang dikenal ekspresi.
const (
	RootResource    = "resource"
	RootCurrentUser = "current_user"
	RootScope       = "scope"
)

// Galat sentinel. Pesan hanya memuat posisi, nama, dan tipe, tidak pernah nilai atribut.
var (
	// ErrSyntax: ekspresi tidak sah menurut tata bahasa (ditolak saat ditulis).
	ErrSyntax = errors.New("policy: sintaks tidak valid")

	// ErrTooComplex: melampaui salah satu batas (panjang, token, kedalaman, langkah, ukuran).
	ErrTooComplex = errors.New("policy: ekspresi melebihi batas")

	// ErrEvaluation: kegagalan evaluasi (tipe tidak cocok, operand bukan boolean, tipe atribut tak didukung).
	ErrEvaluation = errors.New("policy: evaluasi gagal")

	// ErrNoResource: ekspresi membutuhkan sumber daya tetapi Env tidak memilikinya. Engine menunda
	// policy semacam ini ke pengecekan tingkat sumber daya bila Check.Resource nil (ADR-0022 butir 32).
	ErrNoResource = errors.New("policy: sumber daya tidak tersedia")

	// ErrAttributeUnavailable: akar atau atribut yang dirujuk tidak ada di Env. Dengan sumber daya
	// ada, ini adalah penolakan (fail-closed), bukan penundaan.
	ErrAttributeUnavailable = errors.New("policy: atribut tidak tersedia")
)

// syntaxErr membentuk galat sintaks dengan posisi byte. Jangan memasukkan nilai tak tepercaya selain
// pengenal (yang sudah dibatasi panjangnya) ke dalam pesan.
func syntaxErr(pos int, format string, args ...any) error {
	return fmt.Errorf("%w (posisi %d): %s", ErrSyntax, pos, fmt.Sprintf(format, args...))
}

func tooComplex(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrTooComplex, fmt.Sprintf(format, args...))
}

func evalErr(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrEvaluation, fmt.Sprintf(format, args...))
}
