# ADR-0022 — M04: keputusan desain Authorization Engine (bagian 1: keputusan awal; bagian 2: T1b dan T2; bagian 3: T3a dan T3b; bagian 4: T3c; bagian 5: T4a sampai T4b; bagian 6: T4c; bagian 7: T4d; bagian 8: T5a; bagian 9: T5b1)

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

## Bagian 5 — keputusan T4 (2026-11-02)

30. **Migrasi `034_policies_actions`** menambah `policies.actions text[] NOT NULL` tanpa default, constraint `ck_policies_actions` = `policy_actions_valid(actions)`, dan indeks GIN `idx_policies_actions`.
    Fungsi `policy_actions_valid` (SQL, `IMMUTABLE`) memaksa: satu dimensi, 1 sampai 50 elemen, tanpa NULL, tanpa duplikat, tiap elemen sepanjang paling banyak 255 karakter dan berbentuk kode permission (`permission.codePattern`)
    atau `<resource>.*` (bintang hanya sebagai segmen terakhir). Hasil yang tak tentu dipaksa `false` (`COALESCE`), sebab CHECK meloloskan NULL. Keanggotaan terhadap katalog (core di kode, modul di tabel `permissions`)
    tidak dapat menjadi FK pada array dan divalidasi aplikasi saat tulis. Migrasi menolak berjalan (`RAISE EXCEPTION`) bila `policies` tidak kosong: tidak ada cara aman menebak actions baris lama (policy deny tanpa action akan diam-diam berhenti menolak).
    Belum ada kode yang menulis `policies`, jadi guard itu tidak terpicu di instalasi mana pun saat ini. Pencarian policy untuk satu action memakai `actions && ARRAY[kode, 'resource.*']`, dengan `resource` = semua segmen sebelum titik terakhir (butir 5).
31. **Semantik efek policy: policy hanya membatasi, tidak pernah memberi izin** (docs/06 §2: tidak ada link yang memaksa ALLOW; docs/02 §16 dan docs/03 §16 tidak merinci `allow`). Hanya policy `active` yang berlaku; policy global (`organization_id IS NULL`) berlaku untuk semua organisasi, policy organisasi hanya untuk organisasinya.
    Untuk satu check, dari semua policy yang terikat ke action itu: (a) policy `deny` yang ekspresinya true = tolak (alasan `policy_denied`, kode policy ikut audit); (b) policy `allow` adalah SYARAT TAMBAHAN: semua policy `allow` yang berlaku harus true, bila salah satu false = tolak (alasan `policy_denied`);
    (c) galat evaluasi pada policy mana pun (deny maupun allow) = tolak (`policy_evaluation_failed`), tidak pernah dilewati. Hasilnya, policy `allow` setara dengan `deny` atas negasinya; permission tetap syarat pertama.
32. **Atribut sumber daya dan `Authorize` di middleware.** Middleware memakai `Resource` nil (butir 25), sehingga policy yang membutuhkan atribut sumber daya tidak dapat dievaluasi di sana dan menolaknya akan menutup rute selamanya. Aturan:
    dengan `Resource` nil, policy yang membutuhkan atribut sumber daya DITUNDA (tidak berlaku pada pengecekan kasar; dilaporkan sebagai tertunda pada `Decision`) dan wajib dievaluasi oleh pengecekan tingkat sumber daya (service/handler, docs/06 §9);
    dengan `Resource` ada, atribut yang hilang atau galat evaluasi = tolak (fail-closed). Evaluator membedakan "atribut tidak tersedia" dari "galat" (sentinel terpisah) dan keduanya diuji negatif. Keterbatasan yang dicatat: handler yang tidak melakukan pengecekan tingkat sumber daya tidak mendapat policy berbasis atribut.

33. **Bahasa ekspresi policy (`internal/policy`, T4b).** Tokenizer dan parser rekursif-turun tulis tangan; tanpa `eval`, reflection, atau pemanggilan metode. Tata bahasa lengkap ada di dokumentasi paket. Keputusan yang mengikat:
    (a) nilai hanya dapat dibaca lewat `akar.atribut` dengan tepat dua segmen dan akar `resource`, `current_user`, `scope`; pengenal yang berdiri sendiri dan akar lain ditolak (contoh di docs/06 §6 seperti `amount <= approval_limit` bersifat ilustratif, bentuk kanoniknya `resource.amount <= resource.approval_limit`);
    (b) operator `== != < <= > >= in && || !` dan tanda kurung; perbandingan tidak berantai; daftar putih fungsi murni `lower`, `len`, `startsWith`, `endsWith`, `contains` (fungsi yang butuh I/O seperti keanggotaan grup TIDAK ada di T4b; penambahannya wajib lewat ADR baru);
    (c) pengetikan ketat: `null` hanya sama dengan `null`; int dan float dibandingkan numerik; tipe berbeda lainnya, operand non-boolean, dan hasil akhir non-boolean adalah galat `ErrEvaluation` (bukan false), sehingga salah tipe tidak diam-diam meloloskan atau melewatkan policy; `in` memeriksa tipe semua elemen tanpa keluar dini;
    (d) `&&` dan `||` memakai hubung-singkat, sehingga operand yang tidak perlu tidak dievaluasi dan tidak membutuhkan sumber daya (dasar penundaan butir 32 lewat `ErrNoResource`; atribut yang hilang saat sumber daya ada adalah `ErrAttributeUnavailable` = tolak);
    (e) batas keras: ekspresi 2000 byte, 400 token, kedalaman 24, 1000 langkah evaluasi, string literal 256 byte, pengenal 64 byte, daftar 64 elemen, string atribut 4096 byte; pelanggaran = `ErrTooComplex`;
    (f) atribut dari `Env` dinormalisasi ke null/bool/int/float/string/daftar-skalar (termasuk `uuid.UUID` menjadi string); tipe lain adalah galat, tidak pernah dikonversi diam-diam;
    (g) pesan galat memuat posisi, nama, dan tipe saja, tidak pernah nilai atribut;
    (h) sifat yang dijaga uji fuzz dan uji acak: masukan apa pun tidak panik, tidak menggantung, galatnya hanya sentinel yang dikenal, dan `Eval` tidak pernah mengembalikan true bersama galat.

34. **Integrasi policy ke engine (T4b2).** (a) `Authorizer.Can` menerapkan policy hanya SESUDAH tenant, permission, dan cek grup lolos; policy tidak dimuat bila salah satunya gagal. Policy dimuat per check dari PostgreSQL tanpa cache (docs/06 §6 butir 4), satu kueri berindeks GIN, sehingga perubahan atau penonaktifan berlaku pada request berikutnya.
    (b) `PolicySource.ForAction(organizationID *uuid.UUID, action)` mengembalikan policy aktif milik organisasi ditambah policy global; organisasi nil (scope platform) hanya mendapat policy global. Kegagalan memuat adalah galat dari `Can` (bukan izin dan bukan "tanpa policy").
    (c) Env ekspresi: `current_user{id, organization_id}`, `scope{organization_id, active_group_id}`, dan `resource{type, id, organization_id, owner_id, group_id}` ditambah `Attrs`; atribut inti dari `ResourceRef` MENIMPA kunci bernama sama di `Attrs` sehingga modul tidak dapat memalsukan pemilik atau organisasi. Akar `resource` tidak ada bila `Check.Resource` nil (dasar penundaan butir 32).
    (d) `Decision` mendapat `PolicyCode` (policy penyebab penolakan) dan `DeferredPolicies` (policy tertunda); alasan baru `policy_denied` dan `policy_evaluation_failed`. Keputusan ditolak tidak membawa `Level`.
    (e) HTTP: `policy_denied` tampil ke klien sebagai `permission_denied` biasa (403, tanpa menyebut policy); `policy_evaluation_failed` adalah kode 403 tersendiri (docs/06 §12) supaya admin dapat membedakan policy rusak dari penolakan biasa. Kode policy hanya masuk metadata audit `authz.access_denied` (`policy_code`), tidak pernah ke klien.
    (f) `policy.MaxPoliciesPerCheck` = 100: lebih dari itu ditolak gagal-tertutup; endpoint tulis (T5) wajib menegakkan batas yang konsisten.
    (g) Perakitan produksi (`internal/app`) WAJIB memanggil `WithPolicies`; `Authorizer.PoliciesEnabled` ada hanya agar tes perakitan dapat menjaganya, karena lupa memanggilnya mematikan semua policy tanpa suara.

## Konsekuensi bagian 5
- **Doc-sync tertunda (tambahan, kumulatif dengan bagian 1–4):** docs/06 §6 (semantik efek butir 31, penundaan butir 32, Env dan pengikat `actions` butir 34), §12 (`policy_evaluation_failed` = 403, butir 34e), dan §14 (tes policy yang sudah/belum terpenuhi); docs/08 §11 (status `policy_evaluation_failed`). docs/03 §16.1, docs/04 (§8 dan §14), dan `docs/erd.md` untuk migrasi 034 SUDAH diterapkan bersama migrasinya.
- Butir 2 (bagian 1) selesai di sisi skema, bahasa ekspresi, kombinasi efek, repositori, dan integrasi ke `Authorizer.Can` (T4a, T4b1, T4b2); `FieldGuard` menyusul (T4c).

## Bagian 6 — keputusan T4c: FieldGuard (2026-11-04)

35. **Sumber definisi field = katalog permission** (docs/06 §5.1). Field permission adalah kode `<tipe>.field.<nama>.<read|write>`; `<tipe>` adalah semua segmen sebelum `.field.<nama>.<aksi>` (boleh bertitik, mis. `organization.member`), `<nama>` adalah SATU segmen (`[a-z][a-z0-9_]*`).
    Kode diurai dari kanan, sehingga tidak ambigu: aksi = segmen terakhir, nama = kedua dari kanan, segmen ketiga dari kanan wajib `field`. Bentuk itu DICADANGKAN: kode mana pun yang segmen ketiga dari kanannya `field` adalah kode field. Kode berbentuk field milik sebuah tipe dengan aksi selain `read`/`write` adalah GALAT saat definisi tipe itu dimuat
    (gagal-tertutup: tidak pernah diabaikan, karena mengabaikannya membiarkan field tanpa pembatasan); penolakan saat deklarasi (manifest modul, M07) dan saat tulis permission adalah utang yang dicatat. Katalog core saat ini tidak memuat kode field, jadi belum ada field terbatas di instalasi mana pun. Tidak ada migrasi.
36. **Pembatasan per aksi, default tolak.** Sebuah field dibatasi untuk aksi X bila katalog mendeklarasikan kode aksi X untuk field itu. Field terbatas tidak dimiliki siapa pun kecuali kodenya diberikan (Super Admin menerima semua permission `is_system`); izin resource (`invoice.read`) TIDAK membuka field terbatas. Field tanpa kode tidak dibatasi (diatur izin resource).
    Baca dan tulis independen (docs/06 §7): modul yang ingin membatasi keduanya wajib mendeklarasikan kedua kode; yang hanya mendeklarasikan `read` tidak membatasi penulisan.
37. **Tanda tangan menyimpang dari docs/06 §7 (dua hal, disengaja).** (a) Penapisan mengembalikan galat: kegagalan memuat definisi atau himpunan izin tidak boleh menjadi "tanpa pembatasan" maupun peta kosong yang diam-diam. (b) `resourceType string` diganti `domain.ResourceRef`: `Type` harus sama dengan tipe aturan (`ErrFieldTypeMismatch`),
    organisasi sumber daya harus sama dengan organisasi scope, nil sama dengan nil (`ErrFieldScopeMismatch`, GALAT dan bukan penapisan diam-diam; konsisten dengan butir 22), dan izin level `group` hanya berlaku bila `ResourceRef.GroupID` = grup aktif terverifikasi (konsisten dengan `Can`). Level `platform` dan `organization` berlaku tanpa syarat grup; level `resource`/`own` tidak pernah berlaku.
    Bentuk: `NewFieldRules(tipe, kode)` (definisi tipe, tidak berubah), `NewFieldFilter(rules, effective, scope)` (dihitung sekali per request dan tipe), `FieldFilter.Apply(ref, obj)` mengembalikan peta BARU (masukan tidak diubah, tidak pernah nil saat sukses) dan `FieldFilter.CheckWrite(ref, obj)`. Nilai nol `FieldFilter`/`FieldRules` gagal-tertutup (`ErrInvalidFieldRules`).
38. **Pencocokan kunci gagal-tertutup.** Kunci dilipat ke bentuk ringkas (huruf ASCII kecil dan angka saja; semua karakter lain dibuang) dan nama field yang dideklarasikan dilipat sama: `internalCost`, `INTERNAL_COST`, `internal-cost`, `internal cost`, dan `internal\u200bcost` cocok dengan `internal_cost`.
    Pencocokan berlebih menjatuhkan field (aman) dan tidak pernah meloloskannya. Bila dua field yang dideklarasikan berbentuk ringkas sama (mis. `ab` dan `a_b`), kunci itu harus memegang SEMUA kodenya. Kunci yang memuat byte non-ASCII dianggap dapat menunjuk field terbatas mana pun: pelipatan huruf Unicode pada dekoder JSON (mis. `ſ` menjadi `s`) tidak dapat ditiru dengan andal,
    sehingga kunci itu dijatuhkan/ditolak kecuali pemanggil memegang semua kode terbatas untuk aksi tersebut. Konsekuensi: serializer tipe yang punya field terbatas sebaiknya memakai kunci ASCII.
39. **Cakupan terbatas.** Hanya kunci tingkat atas yang ditapis; peta bersarang dan elemen daftar tidak ditelusuri, dan salinannya dangkal (nilai bersarang berbagi referensi dengan masukan). Modul yang membatasi field bersarang menerapkan filter per tipe bersarang. Keterbatasan ini dijaga tes supaya tidak berubah diam-diam.
40. **`CheckWrite` dan galatnya.** Mengembalikan `*FieldDeniedError` (`errors.Is` terhadap `ErrFieldPermissionDenied`) yang memuat nama kunci terkirim yang ditolak, terurut, paling banyak `MaxDeniedFields` (50) dengan `Total` jumlah penuh. Pesan galat TIDAK memuat nama kunci (berasal dari klien: tidak masuk log mentah).
    Transport (T4c3) memetakannya ke 403 `field_permission_denied` dan memutuskan apa yang diekspos. Pemanggil wajib memeriksa kunci MENTAH sebelum didekode ke struct (dekoder melipat huruf dan dapat memetakan kunci berbeda ke field yang sama); `DisallowUnknownFields` disarankan pada endpoint (T5).
41. **Definisi dimuat dari PostgreSQL dan tidak di-cache.** `repository.Fields.FieldCodes(tipe)` mengambil kode katalog berawalan `<tipe>.field.` dengan `starts_with`, BUKAN `LIKE` (garis bawah pada nama tipe adalah wildcard `LIKE`: tipe `a_b` akan ikut memuat `axb`). Tanpa filter modul atau status: kode field milik modul nonaktif tetap membatasi (gagal-tertutup).
    Hasil boleh superset (kode tipe bersarang `a.field.b` untuk tipe `a`) dan disaring ulang oleh `NewFieldRules`; urutan baris tidak dipakai. Definisi tidak di-cache, seperti policy: deklarasi baru berlaku seketika dan tidak ada jendela basi yang meloloskan field yang baru dibatasi. Biayanya satu kueri per pembuatan filter; ukuran nyata diukur di M15.
42. **`application.FieldGuard.For(ctx, identity, scope, tipe)`.** Urutan: user tidak kosong (`ErrInvalidCheck`), scope sah (`ErrInvalidScope`), organisasi Identity sama dengan organisasi scope (`ErrFieldScopeMismatch`, sama dengan langkah 2 `Can`), tipe sah (`ErrInvalidFieldRules`, sebelum menyentuh penyimpanan), definisi dimuat (galat pemuatan adalah galat) dan aturan dibangun (`ErrInvalidFieldCode`).
    Tipe tanpa field terbatas tidak memuat himpunan izin (`FieldRules.HasRestrictions`); selain itu himpunan izin diambil lewat `Authorizer.effective` yang sama dengan `Can` (cache berversi, lalu PostgreSQL) tanpa mengekspor metode baru. FieldGuard BUKAN gerbang izin resource (itu tetap `Authorize`/`Can`). Setiap kegagalan menghasilkan galat bersama `FieldFilter` nilai nol, yang gagal-tertutup (`ErrInvalidFieldRules`) bila pemanggil mengabaikan galat.
43. **Pembungkus satu objek.** `FieldGuard.Filter` dan `FieldGuard.CheckWrite` memanggil `For` per panggilan (satu muat definisi dan, bila perlu, satu muat izin). Jalur daftar memanggil `For` SEKALI per request dan tipe, lalu `FieldFilter.Apply` per objek.
44. **Pemetaan HTTP galat field (`authzhttp.FieldError`).** `ErrFieldPermissionDenied` menjadi 403 `field_permission_denied` (kode sendiri); `ErrFieldScopeMismatch` menjadi 403 `scope_mismatch`; `ErrInvalidCheck`, `ErrInvalidScope`, `ErrInvalidFieldRules`, `ErrInvalidFieldCode`, `ErrFieldTypeMismatch`, dan err nil menjadi 500 (kesalahan pemrograman atau definisi katalog rusak);
    selain itu (PostgreSQL atau Redis gagal) menjadi 503. Tidak ada galat yang menjadi izin. Penyebab hanya masuk log, tidak pernah respons. Respons `field_permission_denied` TIDAK memuat nama field: nama berasal dari klien dan menggemakannya menjadikan respons alat untuk menebak field terbatas; nama dicatat di log/audit oleh handler pemakai pertama (T5/M05), dipotong dan disanitasi.
    KETERBATASAN TERBUKA: status 403 versus sukses tetap menjadi oracle keberadaan field terbatas bagi penulis yang menebak nama kunci. Itu melekat pada persyaratan docs/06 §7 (menolak dengan kode sendiri); mitigasinya adalah rate limit dan audit penolakan (T5, M15), bukan menyembunyikan kode.
45. **Perakitan.** `newAuthzStack` merakit `FieldGuard` (Authorizer yang sama setelah `WithPolicies`, `repository.Fields` di atas pool yang sama) dan menyimpannya di `authzStack.fieldGuard`; dijaga tes `TestAuthzStackWiresFieldGuard` (dengan dan tanpa Redis). Tidak ada handler yang memakainya sampai T5/M05 (belum ada endpoint dengan payload bertipe), sehingga pemakaian ujung-ke-ujung lewat HTTP belum terbukti.

## Konsekuensi bagian 6
- **Doc-sync tertunda (tambahan, kumulatif dengan bagian 1–5):** docs/06 §7 (tanda tangan, sumber definisi, per aksi, pencocokan kunci, cakupan), §13 (letak kode di `authorization/domain`), §14 (tes field: sudah/belum), docs/08 §11 (kode `field_permission_denied` sudah tercantum; tambahkan status 403 dan catatan bahwa respons tidak memuat nama field, butir 44).
- **Keterbatasan yang dicatat (tidak ada jaminan absolut):** penapisan hanya tingkat atas (butir 39); bukti "absen dari REST/WebSocket/export" baru dapat diberikan pada level fungsi sampai ada konsumen bertipe (T5, M05, M08); kunci non-ASCII pada tipe ber-field-terbatas dijatuhkan kecuali semua kode dipegang (butir 38);
  validasi deklarasi kode field di manifest modul belum ada (butir 35, M07).

## Bagian 7 — keputusan T4d: retry terbatas sinkron katalog saat start (2026-11-06)

46. **Sinkron katalog saat start diulang terbatas sebelum `ExitFailed`.** (a) Kebijakan (`defaultSyncRetry`): 5 percobaan total, jeda 1, 2, 4, lalu 8 detik (15 detik tidur; tidak ada jeda sesudah percobaan terakhir), dan tiap percobaan dibatasi 30 detik supaya koneksi yang menggantung tidak menahan start tanpa batas dan retry sempat jalan. Terburuk ±165 detik (dependensi menggantung); database yang menolak koneksi ±15 detik. Tanpa jitter: advisory lock sinkron sudah menserialkan instance.
    (b) Aman diulang: tiap percobaan adalah satu transaksi utuh yang di-rollback bila gagal dan sinkronnya idempoten (advisory lock + konvergensi); bump versi platform sebelum dan sesudah commit (butir 28) tetap benar bila percobaan sebelumnya gagal saat commit.
    (c) Yang TIDAK diulang: `permission.ErrCodeConflict` (kode core dimiliki modul: deterministik), kegagalan merakit syncer (`errSyncSetup`: kesalahan kode), dan pembatalan konteks induk (SIGTERM saat start berhenti seketika; operasi tidak dijalankan bila konteks sudah batal). Galat yang tidak dikenal DIULANG (gagal-aman: ruginya paling banyak jeda total sebelum `ExitFailed`). Klasifikasi galat PostgreSQL permanen (mis. kelas SQLSTATE 28 dan 42) tidak dibuat; galat itu diulang sampai habis.
    (d) Tetap gagal-tertutup: sesudah percobaan habis galat percobaan terakhir dikembalikan ("setelah 5 percobaan: ...", penyebab tetap dapat ditelusuri lewat `errors.Is`) dan `RunAPI` keluar `ExitFailed` sebelum port di-bind; API tidak pernah melayani request dengan katalog atau role sistem yang belum sinkron. Butir 11 tetap berlaku; yang berubah hanya gangguan dependensi yang bersifat sementara. Setiap kegagalan yang akan diulang dicatat level warn (nomor percobaan, jeda, galat); keberhasilan sesudah retry dicatat level info.
    (e) Implementasi: `retryStartup` generik (`internal/app/startup_retry.go`) dan pembungkus `syncAuthorizationWithRetry`; `syncAuthorization` tetap SATU percobaan. `RunAPI` memakai `syncRetryPolicy()` (variabel fungsi; hanya `export_test.go` yang menggantinya dengan kebijakan tanpa jeda sungguhan).

## Konsekuensi bagian 7
- **KETERBATASAN TERBUKA:** selama retry berjalan port belum di-bind, jadi `/health/*` belum menjawab; ±15 detik (database menolak koneksi) sampai ±165 detik (menggantung) dapat melebihi batas startup probe sebuah orkestrator. Memisahkan "bind dan health dulu, sinkron di latar dengan readiness 503" mengubah urutan butir 7 (autentikasi dirakit sesudah sinkron) dan belum diputuskan; dicatat untuk M15 (operabilitas).
- Retry hanya untuk sinkron katalog. Langkah start lain (pembukaan pool tidak memblokir menunggu server siap; `newAuthzStack` tanpa I/O) tidak membutuhkannya.
- Kebijakan produksi dipin tes (`TestDefaultSyncRetryPinsTheBoundedBackoff`); alur ujung-ke-ujung dengan jeda sungguhan tidak diuji (tes `RunAPI` memakai kebijakan instan).
- **Doc-sync tertunda (tambahan, kumulatif dengan bagian 1–6):** docs/10 §11 dan §15 (perilaku start: retry terbatas lalu keluar `ExitFailed`; catatan batas startup probe).

## Bagian 8 — keputusan T5a: authority ceiling dan aturan assignment (2026-11-06)

47. **Aturan assignment murni di `authorization/domain` (`ValidateAssignment`), melengkapi butir 9.** Bentuk scope (`GrantTarget`: platform, organization, group; resource/own tidak dapat menjadi target lewat API karena engine tidak mengevaluasinya) dan bentuk role (docs/02 §12 aturan 3) divalidasi; role yang diarsipkan ditolak.
    Kecocokan role dan scope: tabel `systemTemplateLevels` memberi SATU scope per role sistem (`super-admin` platform; `organization-admin` dan `member` organization; `group-admin` group); role sistem yang tidak ada di tabel ditolak (gagal-tertutup) dan tes `TestSystemTemplateTableMatchesTheRoleCatalog` menjaga bahwa setiap role di `internal/role` punya baris dan slug domain sama dengan `internal/role`. Role non-sistem tidak pernah di platform dan hanya di dalam boundary-nya: role organisasi/custom tanpa grup pada organisasinya atau grup di dalamnya; role yang dimiliki grup (tipe group, atau custom dengan grup) hanya pada grup itu.
    Keputusan baru (alasan: assignment yang tidak pernah berlaku atau yang mengunci platform menyesatkan atau berbahaya): (a) subjek `service` ditolak karena `Effective` tidak mengevaluasinya; subjek pada scope platform hanya `user` (itu satu-satunya yang dievaluasi `Effective` pada scope platform); subjek `organization` harus organisasi scope itu sendiri (itu satu-satunya yang cocok di `Effective`); (b) assignment platform TIDAK boleh punya `expires_at` (super-admin yang kedaluwarsa diam-diam dapat meninggalkan platform tanpa administrator tanpa ada pencabutan yang dapat diperiksa), dan masa berlaku harus di masa depan.
    Mengubah role sistem lewat API (permission, status, hapus) ditolak seluruhnya (`CheckRoleMutable`); membuat role bertipe `system` lewat API ditolak (`ValidateRoleBoundary`); tipe yang boleh dibuat adalah organization, group, dan custom, dan boundary-nya menjadi target authority ceiling.
    Pencabutan (`CheckRevocation`): assignment super-admin platform yang aktif tidak boleh dicabut bila hitungan yang diberikan pemanggil <= 1; hitungan negatif ditolak; role custom berslug `super-admin` bukan super-admin (tipe system dan scope platform wajib).
48. **Semantik authority ceiling (`CheckCeiling`, docs/06 §5.3).** Sebuah permission tercakup bila pemberi memegangnya pada level yang mencakup target: target platform butuh level platform; target organization butuh platform atau organization; target group butuh platform atau organization, atau level group HANYA bila grup target adalah grup aktif terverifikasi pada scope pemberi.
    Batas scope: target platform hanya dari scope platform; target tenant hanya dari scope organisasi yang sama. Akibatnya Super Admin tidak dapat memberi di organisasi dari scope platform: ia masuk dulu lewat `switch-organization` (butir 27), dan tidak ada jalur lintas-tenant. Pemeriksaan batas scope berlaku juga untuk daftar kode kosong. Himpunan pemberi yang kosong atau kode yang tidak dikenal berarti tidak tercakup. Kode tidak sah (kosong, berspasi di ujung, lebih dari 255 karakter) memberi `ErrInvalidGrant` dan didahulukan atas hasil ceiling.
    Galat rinci `CeilingError` memuat kode yang tidak dimiliki, terurut tanpa duplikat, paling banyak 50 dengan `Total` penuh; pesan galat hanya memuat hitungan (kode berasal dari klien, tidak masuk log mentah). Pemetaan HTTP dikerjakan di T5c.
49. **Kewajiban bagi pemanggil (T5b, T5c; domain tidak dapat membuktikannya).** (a) Himpunan permission pemberi untuk `CheckCeiling` dihitung dari PostgreSQL DI DALAM transaksi pemberi (`repository.Effective` pada transaksi itu), BUKAN dari cache: cache basi tidak boleh membuka eskalasi. (b) Keberadaan grup, subjek, dan organisasi serta kepemilikan grup oleh organisasi diverifikasi terhadap database di transaksi yang sama (domain hanya memeriksa bentuk).
    (c) Hitungan untuk `CheckRevocation` = assignment super-admin platform yang aktif DAN dapat dipakai (belum dicabut, belum kedaluwarsa, user aktif), dihitung di bawah advisory lock supaya dua pencabutan serentak tidak sama-sama melihat "masih ada satu lagi". (d) Izin endpoint (`role.manage`, `role.assign`, `permission.assign`) tetap dicek oleh `Authorize`; ceiling adalah syarat tambahan, bukan pengganti. (e) Undangan dengan role praset diperiksa terhadap otoritas pengundang saat penerimaan (docs/06 §5.3; M05).

## Konsekuensi bagian 8
- Belum ada pemakai: tidak ada repositori tulis atau endpoint sampai T5b/T5c. Tes bersifat murni (tanpa database) dan menutup kasus negatif lintas-tenant, eskalasi level group ke organisasi, platform dari scope tenant, kode tidak sah, dan super-admin terakhir.
- **Doc-sync tertunda (tambahan, kumulatif dengan bagian 1–7):** docs/06 §5.3 (semantik cakupan per level, batas scope, Super Admin masuk lewat switch-organization), §5.5 (tabel scope template sebagai kode, subjek yang didukung), docs/02 §15 (subjek `service` belum didukung; assignment platform tanpa masa berlaku).
- **Keterbatasan yang dicatat (tidak ada jaminan absolut):** pemeriksaan ceiling bergantung pada himpunan pemberi yang benar dari database (butir 49a); grup milik organisasi tidak dapat dibuktikan di domain (butir 49b); level group di `Effective` dihitung per grup aktif sehingga pemberi level group hanya dapat memberi di grup aktifnya.

## Bagian 9 — keputusan T5b1: validasi tulis policy dan deklarasi kode field (2026-11-06)

50. **Validasi tulis policy (`policy.Validate`, `policy.Draft`, `policy.CheckPolicyCount`).** Urutan: katalog tersedia (nil = `ErrCatalogUnavailable`, gagal-tertutup); kode policy `^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$` paling panjang 150 (segmen yang sama dengan kode permission); nama tanpa spasi di ujung, tanpa karakter kontrol, UTF-8, paling banyak 150 karakter; deskripsi UTF-8 tanpa NUL, paling banyak 1000 karakter; efek persis `allow` atau `deny`; ekspresi lolos `Parse` (galat membungkus `ErrSyntax` atau `ErrTooComplex`); actions 1..50 elemen tanpa duplikat.
    Keputusan baru: tiap action HARUS ada di katalog. Kode persis harus dikenal (`ActionCatalog.HasCode`); `<resource>.*` hanya sah bila ada kode ber-resource persis itu (`HasResource`), sehingga `organization.*` ditolak bila katalog hanya punya `organization.member.manage` (pola itu tidak akan pernah cocok). Alasan: policy deny yang terikat ke action yang salah ketik adalah pembatasan yang diam-diam tidak menolak apa pun. Galat tidak pernah memuat nilai klien, hanya posisi (`actions[3]`).
    Batas jumlah: `MaxPoliciesPerOrganization` (50) ditambah `MaxGlobalPolicies` (50) tidak melampaui `MaxPoliciesPerCheck` (100), sehingga sebuah check tidak pernah gagal karena kebanyakan policy; dijaga tes. Yang dihitung adalah policy AKTIF; memindahkan policy dari disabled ke active juga menambah hitungan. Repositori (T5b4) menghitung di dalam transaksi tulis di bawah advisory lock dan memetakan pelanggaran `uq_policies_org_code` dan `uq_policies_global_code` menjadi galat kode ganda; `ActionCatalog` diimplementasikan repositori (kode core dari kode, kode modul dari tabel `permissions`).
51. **Validasi deklarasi kode field (`permission.ValidateDeclaration`).** Kode dengan segmen `field` di posisi ketiga dari kanan adalah kode field permission dan aksinya HARUS `read` atau `write`; aksi lain memberi `ErrInvalidFieldDeclaration`. `permission.Sync` (katalog core) memakainya menggantikan pemeriksaan bentuk saja, dan registrasi modul (M07) wajib memakainya. Pemuat `FieldGuard` tetap gagal-tertutup (butir 35); ini mencegah definisi rusak masuk katalog. Letaknya di `permission` (bukan `authorization/domain`) karena `domain` mengimpor `permission`.
52. **Merevisi butir 46(c): galat deterministik tambahan tidak diulang.** `permission.ErrInvalidCode` dan `permission.ErrInvalidFieldDeclaration` (cacat kode katalog yang kini dapat membuat `Sync` gagal saat start) tidak di-retry, seperti `ErrCodeConflict`.

## Konsekuensi bagian 9
- Validasi policy murni; belum ada pemanggil (T5b4 menulis repositori policy dan mengimplementasikan `ActionCatalog`).
- **Doc-sync tertunda (tambahan, kumulatif dengan bagian 1–8):** docs/06 §6 (policy hanya membatasi, `actions` wajib ada di katalog, batas jumlah), docs/07 §7 (registrasi modul memakai `ValidateDeclaration`).
- **Keterbatasan:** keanggotaan actions terhadap katalog hanya berlaku saat tulis; permission modul yang kemudian dihapus dari katalog meninggalkan policy yang tidak pernah cocok (tidak berbahaya: policy hanya membatasi, dan tidak ada action yang cocok berarti tidak ada yang dibatasi); pembersihannya adalah urusan M07.
