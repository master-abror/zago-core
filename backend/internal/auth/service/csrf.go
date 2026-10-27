package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"

	"github.com/google/uuid"
)

// csrfKeyLabel memisahkan kunci CSRF dari kunci lain yang diturunkan dari SESSION_SECRET
// (mis. cursor, ADR-0013): satu rahasia dasar, kunci independen per tujuan.
const csrfKeyLabel = "platform/auth/csrf/v1"

const csrfTokenLen = 43 // base64url tanpa padding untuk 32 byte HMAC-SHA256

// CSRF membuat dan memverifikasi token anti-CSRF yang terikat ke sesi (docs/05 §8):
//
//	token = base64url( HMAC-SHA256(kunci turunan SESSION_SECRET, id sesi) )
//
// Tanpa penyimpanan: token dihitung ulang dari id sesi. Karena id sesi berganti pada setiap
// rotasi (login, refresh, ganti password, ganti organisasi), token lama otomatis tidak berlaku.
type CSRF struct {
	key []byte
}

// NewCSRF membuat CSRF dari SESSION_SECRET (minimal 32 byte).
func NewCSRF(sessionSecret string) (*CSRF, error) {
	if len(sessionSecret) < 32 {
		return nil, errors.New("auth: SESSION_SECRET minimal 32 byte untuk CSRF")
	}
	m := hmac.New(sha256.New, []byte(sessionSecret))
	m.Write([]byte(csrfKeyLabel))
	return &CSRF{key: m.Sum(nil)}, nil
}

func (c *CSRF) mac(sessionID uuid.UUID) []byte {
	m := hmac.New(sha256.New, c.key)
	m.Write(sessionID[:])
	return m.Sum(nil)
}

// Token mengembalikan token CSRF untuk sebuah sesi.
func (c *CSRF) Token(sessionID uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString(c.mac(sessionID))
}

// Verify memeriksa token terhadap sesi dengan perbandingan waktu-konstan. Token kosong, salah
// panjang, bukan base64url, atau milik sesi lain ditolak.
func (c *CSRF) Verify(sessionID uuid.UUID, token string) bool {
	if len(token) != csrfTokenLen {
		return false
	}
	got, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil {
		return false
	}
	return hmac.Equal(got, c.mac(sessionID))
}
