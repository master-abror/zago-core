# ADR-0022 — M04: keputusan desain Authorization Engine (bagian 1: keputusan awal; bagian 2: T1b dan T2; bagian 3: T3a dan T3b)

**Status:** Diterima · **Tanggal:** 2026-10-30 · **Konteks:** awal M04 (docs/06, docs/00 B-04 dan I-01 sampai I-03). Bagian berikutnya
(bagian 2, 3, ...) ditambahkan per ◆CP/task; butir diberi nomor lanjut.

## Konteks
docs/06 menetapkan engine otorisasi (rantai, cache versi, policy, authority ceiling) tetapi menyisakan celah yang baru terlihat saat membaca
skema dan kode M03: identitas platform tidak punya jalur masuk organisasi (ADR-0020 butir 28), tabel `policies` tidak punya pengikat ke action,
`Bus` di `app/auth.go` masih instans sendiri (HANDOVER-M03 §8), dan penamaan role sistem selain `super-admin` belum ditetapkan.

## Keputusan
1. **Identitas platform memasuki organisasi lewat `switch-organization`.** Identitas tanpa keanggotaan boleh pindah ke organisasi berstatus `active`
   bila ENGINE (`Authorizer.Can`, bukan layanan auth) memutuskan `organization.read` pada scope `platform`. Tidak ada keanggotaan buatan.
   Penolakan tetap `403 permission_denied` seragam tanpa oracle keberadaan organisasi. Event keamanan `auth.organization.switched` membawa `via=platform`
   dan activity dicatat di organisasi tujuan. Di dalam sesi itu assignment `platform` tetap berlaku (docs/06 §5.2) sehingga `Decision.Level` = `platform`,
   tetapi setiap query tenant tetap memfilter `organization_id` sesi. Organisasi `suspended` tidak dimasuki; pengelolaannya lewat endpoint tingkat platform.
   Antarmuka: paket `auth` mendefinisikan `PlatformEntryChecker` yang diimplementasikan `authorization` (tidak ada impor melingkar). Dikerjakan di T3/T5.
2. **Policy terikat ke action lewat kolom baru `policies.actions text[]` (migrasi dikerjakan di T4; nomornya 034, BUKAN 021: lihat bagian 2 butir 10).** Isi: kode permission persis atau `resource.*`;
   tidak boleh kosong; divalidasi terhadap katalog saat ditulis. Policy `organization_id IS NULL` (global) berlaku untuk semua organisasi; policy organisasi
   hanya untuk organisasinya. Migrasi baru (bukan mengedit yang sudah di-tag); tes ERD, roundtrip, docs/03, docs/04, `docs/erd.md` ikut disinkronkan.
3. **Invalidasi cache dipanggil eksplisit di dalam transaksi pengubah (sebelum commit dan sesudah commit)**, bukan lewat event bus (menyimpang dari docs/06 §8.1
   yang menyebut handler in-process di bus): `Bus` di `app/auth.go` belum bersama, jadi kebenaran tidak boleh bergantung pada pengiriman event. Event outbox
   tetap boleh diterbitkan untuk konsumen lain. Versi kunci = pasangan (versi organisasi, versi platform) sehingga mengubah role sistem (global)
   menginvalidasi semua organisasi lewat penghitung `platform`. Dikerjakan di T3.
4. **Pembuka kunci akun administratif (HANDOVER-M03 §9 #9) ditunda ke M05** (bagian administrasi user). Sementara: `redis-cli DEL auth:lock:<user_id>`.
5. **Katalog permission core = docs/06 §5.4 persis (41 kode)**, dideklarasikan di `internal/permission/catalog.go`. `resource` = semua segmen sebelum titik terakhir,
   `action` = segmen terakhir (`organization.member.manage` → resource `organization.member`, action `manage`). Namespace core = segmen pertama kode katalog;
   modul tidak boleh mendeklarasikan kode di namespace itu (M07). docs/08 tidak memuat kode per rute, jadi tidak ada penambahan.
6. **Role sistem** (global, `role_type='system'`, organization/group NULL, ditetapkan oleh kode): `super-admin` ("Super Admin"), `organization-admin`
   ("Organization Admin"), `group-admin` ("Group Admin"), `member` ("Member"). Set default = docs/06 §5.5; `super-admin` = SEMUA permission core `is_system`
   (termasuk yang sudah tidak ada di katalog, supaya tidak kehilangan hak diam-diam).
7. **Semantik sinkron startup** (`authorization/application.Syncer`, satu transaksi, advisory lock `0x7a61676f03`, hanya hak `app_user`):
   upsert permission by `code` (tidak pernah menghapus; kode core yang dimiliki modul = `ErrCodeConflict` dan semuanya di-rollback; permission core yang
   hilang dari katalog dilaporkan sebagai orphan); upsert role sistem by slug (mengadopsi baris minimal bootstrap-admin; nama, deskripsi, dan status `active`
   dikembalikan bila menyimpang); set permission CORE tiap role sistem dikonvergensikan persis ke default (kelebihan dicabut, kekurangan ditambah);
   permission modul pada role sistem tidak disentuh. Satu activity `authz.catalog_synced` (`actor_type='system'`, scope platform, metadata hanya hitungan)
   hanya bila ada perubahan. Tanpa event outbox (belum ada konsumen; invalidasi cache dipasang di T3).
8. **Struktur paket:** `internal/permission` (katalog, kode, sinkron permission), `internal/role` (definisi dan sinkron role sistem; mengimpor `permission`),
   `internal/authorization/application` (Syncer; satu-satunya yang menggabungkan keduanya, sejalan dengan docs/06 §13). Repositori memakai pgx langsung
   (pola `bootstrap`); blok sqlc ditambahkan hanya bila query menjadi besar dan butuh `make generate`.
9. **Aturan assignment (ditegakkan di T5, tercatat di sini):** `super-admin` hanya boleh ditetapkan pada scope `platform` dan sebaliknya hanya `super-admin`
   pada scope `platform`; template lain hanya pada scope yang dinyatakan docs/06 §5.5; permission role sistem tidak dapat diubah lewat API (klon sebagai role custom);
   assignment platform `super-admin` terakhir yang aktif tidak boleh dicabut.

## Konsekuensi
- Wiring sinkron ke `RunAPI` dikerjakan sebagai commit terpisah setelah T1 terbukti (tes `app` memakai pool tiruan).
- **Doc-sync tertunda (kumulatif M04):** docs/06 (§4.1 jalur platform, §5.5 slug role, §6 pengikat policy, §8.1 invalidasi eksplisit dan versi pasangan),
  docs/03 + docs/04 + `docs/erd.md` (migrasi 021), docs/05 (switch-organization untuk identitas platform), docs/00 B-04 (katalog dan role sinkron sebagai dibangun),
  ADR-0020 butir 28 (catatan: dilanjutkan ADR-0022 butir 1).

---

## Bagian 2 — keputusan T1b dan T2 (2026-10-31)

10. **Koreksi nomor migrasi: `policies.actions` = migrasi `034`, bukan `021`.** `backend/migrations` sudah memuat `001`–`033` (`021_password_reset_tokens`,
    nomor tertinggi `033_grants`). Butir 2, prompt sesi, dan handover yang menyebut "migrasi 021" harus dibaca sebagai "migrasi berikutnya yang bebas (saat ini 034)".
    Tes ERD, roundtrip, docs/03, docs/04, dan `docs/erd.md` ikut disinkronkan di T4. Periksa lagi nomor bebas sebelum membuat berkas (jangan menebak).
11. **Syncer dipasang di `RunAPI` (T1b), bukan di worker.** `internal/app/authz.go` (`syncAuthorization`, `logSync`) dipanggil sekali, SESUDAH dependensi dibuka dan
    SEBELUM `AttachSystemLog`, audit, dan rakitan auth. Hanya bila pool nyata ada (`appPool != nil`; tes dengan fake dilewati). Galat sinkron = `ExitFailed` sebelum port di-bind;
    orphan = log `warn` (tidak dihapus). **Konsekuensi yang disengaja:** API tidak start bila database tidak terjangkau saat startup (sebelumnya hanya `/health/ready` 503);
    compose menjalankan API setelah `migrate` selesai, sehingga aman di stack ini; deployment lain harus memastikan database siap sebelum API. **Risiko terbuka:** pada failover database yang lambat, pod yang baru start akan crash-loop (docs/10 §11 menolak me-restart pod karena kegagalan database; §15: permintaan gagal 503). Mitigasi yang diusulkan, belum dikerjakan: retry terbatas dengan backoff sebelum `ExitFailed`. Terbukti di stack nyata:
    41 permission + 4 role + 103 relasi pada start pertama, `sudah sinkron` dan satu activity `authz.catalog_synced` pada restart.
12. **Header `X-Active-Group-ID` adalah petunjuk yang dibuang, bukan galat, bila tidak sah** (docs/06 §4.2). Hanya SATU nilai berbentuk UUID kanonik 36 karakter berstrip (huruf
    besar diterima) dan bukan UUID nol yang diteruskan ke verifikasi; header ganda, kosong, `urn:`, kurung kurawal, tanpa strip, spasi di ujung, dan UUID nol dibuang TANPA menyentuh database.
    Identitas platform (tanpa organisasi) mengabaikan header. Verifikasi dilakukan pada SETIAP request (tanpa cache), sehingga pencabutan keanggotaan berlaku pada request berikutnya.
    Nilai header tidak masuk log (hanya alasan pembuangan, level debug).
13. **Verifikasi grup lebih ketat dari docs/06 §4.2:** satu kueri `EXISTS` (`authorization/repository.Groups.IsActiveGroupMember`) mensyaratkan keanggotaan grup `active`, grup `active`
    dan `deleted_at IS NULL`, `organization_id` cocok pada `group_memberships` DAN `groups`, serta keanggotaan organisasi user `active` (keanggotaan grup tidak berlaku bila
    keanggotaan organisasinya ditangguhkan atau dicabut). Status organisasi itu sendiri tidak diperiksa di sini (ditegakkan `Authenticate`/sesi dan, di T3, engine).
14. **Kontrak middleware `Scope` (`authorization/transport/http`, paket `authzhttp`).** Urutan rute terlindungi: `Authenticate → CSRF → RequireAuth → Scope → Authorize(action, T3) → handler`.
    Tanpa Identity: 401 `authentication_required` (fail-closed, salah urutan pemasangan tidak melanjutkan request). Galat resolusi (infrastruktur): 503 `service_unavailable` tanpa
    detail ke klien, log `error`. `ScopeContext` disimpan sebagai salinan (`WithScope`/`ScopeFromContext`); `ok=false` berarti middleware belum berjalan dan pemanggil menolak.
    `scope_mismatch` (handler tenant dengan scope platform) BELUM didaftarkan di `httpx`; didaftarkan bersama `Authorize` di T3. `Scope` belum dipasang di rute mana pun (belum ada
    rute yang membutuhkannya) — pemasangannya di T3/T5.
15. **Struktur paket nyata (melengkapi butir 8).** `authorization/domain` (`ScopeContext`; T3 menambah `Check`/`Decision`), `authorization/application` (Syncer + `ScopeResolver` + port
    `GroupMembershipChecker`), `authorization/repository` (pgx langsung), `authorization/transport/http` (`authzhttp`). `authorization` boleh mengimpor `auth/domain` (`Identity`);
    `auth` tidak boleh mengimpor `authorization` (antarmuka `PlatformEntryChecker` didefinisikan di `auth`, T3/T5). Fixture tes grup ada di `testkit` (`AddOrganizationMember`, `NewGroup`, `AddGroupMember`).

## Konsekuensi bagian 2
- **Doc-sync tertunda (tambahan, kumulatif dengan bagian 1):** docs/06 §4.2 (header dibuang, bentuk kanonik, pengetatan butir 13) dan §13 (struktur paket nyata); docs/10 §3, §11, §15 (urutan start API: sinkron katalog
  sebelum melayani request, API gagal start tanpa DB; risiko dan mitigasi retry); docs/18 §7 (fixture grup); koreksi "migrasi 021" → 034 di docs/03, docs/04, `docs/erd.md` (dikerjakan bersama T4).
- Konsekuensi bagian 1 yang menyebut "wiring sinkron ke RunAPI dikerjakan sebagai commit terpisah" SELESAI di T1b (butir 11).

## Bagian 3 — keputusan T3a dan T3b (2026-11-01)

16. **Scope `platform` hanya berlaku untuk subjek `user`** (`authorization/repository.Effective`). Assignment platform dengan subjek `group`, `organization`, atau `service` DIABAIKAN.
    Rumus docs/06 §5.2 yang literal membuat keanggotaan grup atau organisasi bisa menjadi jalur menuju otoritas platform; ini ditutup. Penegakan saat penulisan (T5, butir 9) tetap berlaku,
    tetapi engine tidak boleh bergantung padanya (pertahanan berlapis).
17. **Cabang tenant effective permissions lebih ketat dari docs/06 §5.2.** Assignment ber-scope `organization`/`group` berlaku hanya bila SEMUA terpenuhi: belum dicabut, belum kedaluwarsa,
    role `active`; `role_assignments.organization_id` = organisasi scope; organisasi `active` dan `deleted_at IS NULL`; user anggota organisasi `active`; role global (sistem) atau milik
    organisasi yang sama; scope `organization` dengan `scope_id` kosong atau organisasi itu sendiri; scope `group` dengan `scope_id` = grup aktif scope DAN grup itu `active` di organisasi scope;
    subjek = user itu sendiri, organisasi scope, atau grup yang user-nya anggota `active` (grup `active`, tidak terhapus, di organisasi scope). Subjek `service` tidak pernah berlaku untuk user.
    Identitas platform tanpa keanggotaan hanya mendapat grant platform (bukan grant organisasi) walau masuk ke organisasi lewat `switch-organization`.
18. **Himpunan effective permissions hanya memuat level `platform`, `organization`, `group`; level terluas menang per kode** (`platform` > `organization` > `group`; `domain.EffectivePermissions`,
    `EffectiveBuilder`, `ErrInvalidGrant`). `Decision.Level` = level terluas yang memberi kode (konsisten dengan butir 1). Assignment ber-scope `resource` dan `own` TIDAK masuk himpunan
    (docs/06 §5.2: tidak di-cache) dan **belum dievaluasi saat pengecekan: ditolak, gagal tertutup**. Pekerjaan tersisa: T3b2 (kueri terpisah di `Can`) atau milestone pertama yang punya
    sumber daya ber-pemilik. Filter "permission modul nonaktif tidak dapat diberikan" (docs/06 §14, Cache) juga belum ada (butuh tabel enablement modul, M07).
19. **Cache (`authorization/cache`).** Kunci versi `authz:ver:{organization_id}` dan `authz:ver:platform` (tanpa TTL); kunci efektif
    `authz:eff:{org|-}:{verOrg}:{verPlatform}:{user}:{grup|-}`; nilai JSON `{"v":1,"g":{kode:level}}` yang divalidasi lewat `EffectiveFromGrants` saat dibaca (nilai rusak, versi format lain,
    atau level `resource`/`own` = miss yang ditimpa). TTL default 2 menit (jaring pengaman; docs/06 §8.1 menyebut "beberapa menit"), batas satu operasi Redis 250 ms (ADR-0019), setelah galat
    Redis dilewati 1 detik (backoff). `Lookup` TIDAK PERNAH mengembalikan galat: galat Redis, versi rusak (bukan bilangan bulat positif), atau backoff = `Lookup{}` (Key kosong: hitung dari PostgreSQL
    dan jangan menulis). Versi dibaca SEBELUM menghitung dan dipakai untuk `Store` (hasil yang dihitung sebelum bump tidak pernah dibaca setelahnya).
    **Penghitung yang hilang (tergusur) dimulai dari jam (ms epoch), bukan dari 1** (pembaca: `SET NX now`; bump: skrip Lua `EXISTS→INCR else SET now+1`), supaya entri efektif lama tidak hidup kembali.
    Batas: jam mundur lebih dari 1 ms atau dua kejadian pada milidetik yang sama (diabaikan).
20. **Invalidasi (`Cache.InvalidateInTx`).** Dipanggil DI DALAM transaksi pengubah SESUDAH perubahan ditulis: bump sebelum commit (galat dikembalikan, transaksi dibatalkan: fail-closed) dan sekali lagi
    lewat `kernel.AfterCommit` (galat hanya di-log level error; entri basi dibatasi TTL). Penghitung target: `Targets{Platform, Organizations}` (dedup, UUID nol diabaikan). Tanpa transaksi pada ctx
    kedua bump berjalan langsung. **Keterbatasan terbuka:** bila Redis gagal tepat antara bump sebelum dan sesudah commit, pencabutan bisa tertunda sampai TTL (2 menit); tidak ada jaminan absolut.
21. **`Syncer.WithInvalidator(inv, log)`**: bila `SyncResult.Changed()`, bump platform sekali sebelum dan sekali sesudah commit; kegagalan bump hanya di-log (start API tidak boleh mati karena Redis,
    docs/10 §11/§15). **Belum terpasang di `RunAPI`** (sinkron saat start masih tanpa invalidator): pemasangannya bagian T3c, bersama perakitan `cache.Cache` di app.
22. **`Authorizer.Can(ctx, Check) (Decision, error)` mengikuti docs/06 §9** (satu parameter `Check` yang memuat `Identity`, `Scope`, `Action`, `Resource`), bukan tanda tangan tiga parameter di prompt/handover.
    Urutan, tiap langkah hanya dapat menolak: (1) `Check` salah bentuk (user nol, aksi bukan `resource.action` sah) = `ErrInvalidCheck`; scope melanggar invarian = `domain.ErrInvalidScope`; keduanya GALAT, bukan
    keputusan; (2) organisasi Identity = organisasi scope = organisasi sumber daya (nil sama dengan nil), selain itu `Decision{Allowed:false, Reason:"scope_mismatch"}` SEBELUM menyentuh cache/database,
    juga untuk level platform; (3) effective set dari cache atau PostgreSQL (galat PostgreSQL = galat, `Decision` nol, tidak pernah ALLOW); (4) kode tidak ada = `no_permission`; izin level `group` pada
    `Resource` hanya berlaku bila `Resource.GroupID` = grup aktif. Alasan keputusan: `granted`, `no_permission`, `scope_mismatch` (`domain.Reason*`).
23. **Pekerjaan T3c yang diwariskan:** middleware `Authorize(action)` memetakan `Decision` ke 403 `permission_denied` (audit DENY dalam transaksinya sendiri, docs/06 §9) dan `scope_mismatch` ke kode error yang
    didaftarkan di `httpx`; galat `Can` = 503 (infrastruktur) atau 500 (`ErrInvalidCheck`/`ErrInvalidScope`, kesalahan pemrograman) , diputuskan di T3c (menyelesaikan utang partial-2 §8 tentang `ErrInvalidScope`);
    rakit `cache.Cache` + `Authorizer` + `Syncer.WithInvalidator` di `internal/app`; `PlatformEntryChecker` (butir 1) dan `switch-organization` identitas platform.

## Konsekuensi bagian 3
- **Doc-sync tertunda (tambahan, kumulatif dengan bagian 1–2):** docs/06 §5.2 (platform hanya subjek user, pengetatan butir 17, level terluas, resource/own belum), §8.1 (kunci memuat versi platform, versi dari jam, TTL 2 menit, skrip bump, perilaku saat bump gagal),
  §9 (galat vs keputusan, urutan `Can`), §14 (tes yang sudah/belum terpenuhi); docs/10 §11/§15 (invalidasi gagal tidak mematikan start); docs/18 §7 (fixture role/permission/assignment di `testkit`).
- Butir 3 (bagian 1) "dikerjakan di T3" **sebagian selesai**: mekanisme dan Syncer ada (butir 19–21); pemakaian oleh endpoint pengubah (role, assignment, keanggotaan) ada di T5; pemasangan di app di T3c.

## Bagian 4 — keputusan T3c (2026-11-02)

24. **`scope_mismatch` didaftarkan di paket `authzhttp`** (`ScopeMismatch`, status 403), mengikuti konvensi `kernel/httpx/errors.go`: kode domain didaftarkan oleh paket pemiliknya lewat `httpx.Register`; registrinya tetap satu.
    `permission_denied` (403) sudah ada di `httpx` dan dipakai apa adanya. Kode berbeda dari `permission_denied` supaya UI dan pencarian audit dapat membedakannya; keduanya 403.
25. **Middleware `Authorize(action)`** (dipasang sesudah `Scope`; memakai `Resource` nil, pemeriksaan sumber daya tertentu dilakukan handler lewat `Authorizer.Can`). Gagal tertutup di setiap cabang:
    tanpa Identity = 401 `authentication_required` (engine tidak dipanggil); Identity ada tetapi Scope tidak = 500 `internal_error` (salah urutan middleware adalah kesalahan server, bukan kesalahan klien yang dapat diperbaiki dengan login ulang; engine tidak dipanggil);
    **galat `Can` selalu menang atas `Decision`** (juga bila `Decision{Allowed:true}` ikut dikembalikan): `ErrInvalidCheck`/`domain.ErrInvalidScope` = 500 (kesalahan pemrograman), galat lain (infrastruktur) = 503; galat tidak direkam sebagai penolakan;
    `Decision` ditolak = 403 `scope_mismatch` bila alasannya `scope_mismatch`, selain itu (termasuk Decision nol dan alasan tak dikenal) 403 `permission_denied` seragam tanpa menyebut aksi atau alasan ke klien.
    Setiap penolakan direkam sebagai activity `authz.access_denied` dengan `result=denied` (perekam menulisnya di transaksi terpisah, docs/10 §4), metadata `required_action`, `reason` (hanya `no_permission`/`scope_mismatch`; selain itu `unspecified`), `method`, dan scope request
    (grup aktif terverifikasi > organisasi > platform). Tanpa perekam audit, penolakan tetap berlaku. `Authorize` panik saat dipasang bila `Deps.Authorizer` nil atau aksi tak sah (kesalahan wiring gagal saat start, bukan saat request pertama).
26. **`domain.ErrInvalidScope` dari resolver di middleware `Scope` = 500**, bukan 503 (menyelesaikan utang partial-2 §8): Identity yang melanggar invarian scope adalah cacat data atau kode, bukan gangguan sementara yang pantas dicoba ulang.
27. **`PlatformEntryChecker` dan masuk organisasi lewat identitas platform.** Antarmuka `PlatformEntryChecker.CanEnterOrganizations(ctx, userID) (bool, error)` didefinisikan di `auth/service` (tanpa impor `authorization`); implementasinya `authorization/application.PlatformEntry` memanggil `Authorizer.Can` dengan
    Identity tanpa organisasi, scope platform, aksi `organization.read`, dan hanya menghitung keputusan `Allowed` ber-level platform (izin tingkat organisasi atau grup bukan tiket masuk; Identity ber-organisasi akan ditolak `Can` dengan `scope_mismatch`, sebab itu organisasi sesi sengaja tidak dipakai).
    `SwitchOrganization`: jalur anggota (`HasActiveMembership`) tidak berubah dan tidak memanggil checker; hanya bila bukan anggota, **engine ditanya LEBIH DULU, baru organisasi diperiksa `active` dan belum terhapus** (`Store.OrganizationIsActive`),
    supaya pengguna biasa mengalami kerja yang sama untuk organisasi yang ada maupun tidak (tanpa selisih waktu yang membocorkan keberadaan organisasi). Semua penolakan (bukan anggota, tanpa izin platform, organisasi tak ada/suspended/archived/terhapus, tanpa checker) = `ErrNotOrganizationMember` yang sama = 403 `permission_denied`.
    Galat engine atau database naik sebagai galat (500 lewat handler, sama dengan galat database lain di endpoint itu), bukan penolakan seragam dan bukan izin. Checker nil = identitas platform tidak dapat masuk (fail-closed).
    Event keamanan `auth.organization.switched` dan activity `auth.switch_organization` membawa `via=platform` pada jalur ini (jalur anggota tidak berubah); `Decision.Level` tetap platform; tidak ada keanggotaan yang dibuat.
28. **Perakitan di `internal/app`:** `newAuthzStack(db, rdb, log)` merakit `cache.Cache` hanya bila ada Redis (tanpa Redis: cache dan invalidator berupa interface nil, `Authorizer` tetap bekerja dari PostgreSQL), `Authorizer`, dan `PlatformEntry`. `RunAPI` merakitnya sesudah dependensi dibuka dan sebelum sinkron katalog,
    lalu `syncAuthorization` memakai `Syncer.WithInvalidator` (butir 21: bump platform sebelum dan sesudah commit bila katalog berubah; kegagalan hanya di-log). `PlatformEntry` dipasang ke `AuthService` lewat `AuthDeps.PlatformEntry` (tanpa pool: interface nil, fail-closed).
    Middleware `authzhttp` BELUM dirakit di app dan belum dipasang di rute mana pun (perakitannya di T5 bersama rute pertama).
29. **Jawaban pertanyaan terbuka partial-3 §9:** T3b2 (scope `resource`/`own`) ditunda ke milestone pertama yang punya sumber daya ber-pemilik (engine tetap gagal tertutup); TTL/timeout/backoff cache tetap konstanta sampai M15 (tanpa env baru);
    galat engine di `Authorize` = 503/500 sesuai butir 25; retry sinkron saat start BELUM diputuskan dan tetap terbuka (partial-4 §9).

## Konsekuensi bagian 4
- **Doc-sync tertunda (tambahan, kumulatif dengan bagian 1–3):** docs/05 (switch-organization untuk identitas platform: urutan engine-lalu-organisasi, `via=platform`, penolakan seragam); docs/06 §9 (perilaku `Authorize`, activity `authz.access_denied`), §12 (`scope_mismatch` didaftarkan di paket pemilik); docs/08 §11 (`scope_mismatch` 403); docs/10 §3 (urutan start API dengan `authzStack`).
- Butir 3 (bagian 1) "dikerjakan di T3" kini **sebagian besar selesai**: mekanisme, Syncer, dan pemasangan Syncer di app ada; pemakaian oleh endpoint pengubah (role, assignment, keanggotaan) ada di T5.
- Keterbatasan yang dicatat (tidak ada jaminan absolut): sesi hasil masuk-lewat-platform tetap hidup sampai habis walau assignment platform dicabut (`Authenticate` tidak memeriksa ulang hak per request; setiap request terguard tetap diputuskan ulang oleh engine); audit penolakan menulis satu baris per request tanpa rate limit per pengguna.
