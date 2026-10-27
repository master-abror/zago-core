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

	// ErrInvalidCredentials: email tidak dikenal, tanpa credential password, password salah, atau
	// status akun yang tidak boleh dibedakan dari itu. SATU kesalahan untuk semua sebab agar
	// login bukan oracle enumerasi akun (docs/05 §18).
	ErrInvalidCredentials = errors.New("auth: email atau password salah")

	// ErrAccountSuspended dan ErrAccountLocked hanya dikembalikan SETELAH password terverifikasi
	// (docs/05 §18): sebelum itu setiap kegagalan adalah ErrInvalidCredentials.
	ErrAccountSuspended = errors.New("auth: akun ditangguhkan")
	ErrAccountLocked    = errors.New("auth: akun terkunci")

	// ErrNotOrganizationMember: user bukan anggota aktif organisasi tujuan (atau organisasinya
	// tidak ada / tidak aktif). Satu kesalahan untuk semuanya: tidak ada oracle keberadaan organisasi.
	ErrNotOrganizationMember = errors.New("auth: bukan anggota aktif organisasi")
)
