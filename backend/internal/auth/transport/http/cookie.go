package authhttp

import (
	"errors"
	"math"
	"net/http"
	"strings"
	"time"
)

// hostPrefix dipasang di depan nama cookie bila Secure menyala (docs/05 §8): peramban menolak
// cookie __Host- yang tanpa Secure, dengan Domain, atau dengan Path selain "/", sehingga cookie
// sesi tidak bisa ditimpa dari subdomain.
const hostPrefix = "__Host-"

// Cookie mengatur cookie sesi: HttpOnly, SameSite=Lax, Path=/, tanpa Domain.
type Cookie struct {
	name   string
	secure bool
}

// NewCookie membuat Cookie dari COOKIE_NAME dan COOKIE_SECURE. Dengan secure=true nama efektif
// menjadi "__Host-"+baseName; dengan secure=false (pengembangan lewat http) tanpa prefiks,
// karena peramban menolak __Host- tanpa Secure.
func NewCookie(baseName string, secure bool) (Cookie, error) {
	if baseName == "" {
		return Cookie{}, errors.New("authhttp: nama cookie kosong")
	}
	if strings.HasPrefix(baseName, "__") {
		return Cookie{}, errors.New("authhttp: nama cookie tidak boleh berawalan \"__\" (prefiks ditambahkan otomatis)")
	}
	for _, r := range baseName {
		if !isTokenChar(r) {
			return Cookie{}, errors.New("authhttp: nama cookie hanya boleh huruf, angka, '-' dan '_'")
		}
	}
	name := baseName
	if secure {
		name = hostPrefix + baseName
	}
	return Cookie{name: name, secure: secure}, nil
}

func isTokenChar(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_'
}

// Name mengembalikan nama efektif cookie.
func (c Cookie) Name() string { return c.name }

// Token membaca nilai cookie sesi; ok=false bila tidak ada atau kosong.
func (c Cookie) Token(r *http.Request) (string, bool) {
	ck, err := r.Cookie(c.name)
	if err != nil || ck.Value == "" {
		return "", false
	}
	return ck.Value, true
}

// Set menulis cookie sesi yang berlaku sampai expiresAt (batas absolut sesi): cookie tidak
// pernah hidup lebih lama dari sesinya.
func (c Cookie) Set(w http.ResponseWriter, token string, expiresAt, now time.Time) {
	maxAge := int(math.Ceil(expiresAt.Sub(now).Seconds()))
	if maxAge < 1 {
		c.Clear(w)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: c.name, Value: token, Path: "/", MaxAge: maxAge, Expires: expiresAt.UTC(),
		HttpOnly: true, Secure: c.secure, SameSite: http.SameSiteLaxMode,
	})
}

// Clear menghapus cookie sesi di peramban.
func (c Cookie) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: c.name, Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(0, 0),
		HttpOnly: true, Secure: c.secure, SameSite: http.SameSiteLaxMode,
	})
}
