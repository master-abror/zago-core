package testpg

import "os"

// ryukConnectionTimeout adalah batas tunggu container Ryuk (penjaga yang menghapus container tes
// bila proses tes mati mendadak) menjadi siap. Bawaan testcontainers 1 menit, dan pada Docker Desktop
// yang kewalahan (banyak paket tes memulai container bersamaan) itu terlampaui: `wait for reaper …:
// context deadline exceeded` (ADR-0017).
const ryukConnectionTimeout = "3m"

// ConfigureReaper menaikkan batas tunggu Ryuk menjadi ryukConnectionTimeout, KECUALI operator sudah
// menyetelnya sendiri (TESTCONTAINERS_RYUK_CONNECTION_TIMEOUT atau RYUK_CONNECTION_TIMEOUT). Ryuk
// sengaja tidak dimatikan: itu jaring pengaman agar container tidak bocor. Konfigurasi testcontainers
// dibaca sekali secara lazy saat container pertama dibuat, jadi fungsi ini harus dipanggil SEBELUM
// itu: dari testkit.Run (TestMain) dan di awal setiap fungsi yang membuat container.
func ConfigureReaper() {
	for _, k := range []string{"TESTCONTAINERS_RYUK_CONNECTION_TIMEOUT", "RYUK_CONNECTION_TIMEOUT"} {
		if os.Getenv(k) != "" {
			return
		}
	}
	_ = os.Setenv("TESTCONTAINERS_RYUK_CONNECTION_TIMEOUT", ryukConnectionTimeout)
}
