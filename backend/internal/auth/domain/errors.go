package domain

import "errors"

var (
	// ErrUnauthenticated: kredensial/sesi tidak ada, tidak dikenal, kedaluwarsa, atau dicabut.
	// Sengaja SATU kesalahan untuk semua sebab: pemanggil (dan klien) tidak boleh bisa
	// membedakan "token tidak pernah ada" dari "token sudah dicabut". Sebab sebenarnya hanya
	// masuk log debug.
	ErrUnauthenticated = errors.New("auth: tidak terautentikasi")

	// ErrSessionNotFound: sesi yang dituju tidak ada, bukan milik pemanggil, atau sudah dicabut.
	// Untuk operasi self-service atas sesi tertentu (404, tidak membocorkan keberadaan).
	ErrSessionNotFound = errors.New("auth: sesi tidak ditemukan")
)
