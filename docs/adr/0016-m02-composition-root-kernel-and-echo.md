# ADR-0016 — M02/T5a+T5c: toolkit HTTP kernel di composition root dan endpoint dev `_kernel/echo`

**Status:** Diterima · **Tanggal:** 2026-10-10 · **Keputusan oleh:** pemilik proyek (persetujuan atas usulan sesi M02, 2026-10-10)

## Konteks
T4 menyelesaikan toolkit `kernel/httpx` (envelope, cursor, idempotency, rate limit) tetapi belum ada yang memasangnya, belum ada
klien Redis di composition root, dan belum ada bukti ujung-ke-ujung lewat `httpserver.NewHandler`. Handover parsial 2 §9 meminta
dua keputusan (gerbang endpoint, parameter `OUTBOX_*`); satu keputusan lagi muncul dari docs/14 §5 (port `/metrics`).

## Keputusan
1. **Satu klien Redis.** `app.Resource` mendapat field `Redis redis.UniversalClient`; `infra.Deps.Redis` mengisinya. `RunAPI`
   mengambilnya dari Resource dan membagikannya ke idempotency, rate limiter, dan komponen berikutnya. Tidak ada klien Redis lain di
   proses API. Tanpa klien (fake di tes), toolkit tidak dirakit dan dicatat sebagai peringatan.
2. **`httpserver.KernelServices`** (`NewKernelServices`) merakit satu `Responder`, `CursorCodec` (kunci turunan `SESSION_SECRET`,
   ADR-0013), `Idempotency`, `RateLimiter`, dan `AuditRecorder` opsional; diteruskan lewat `Options.Kernel`. `Subject` idempotency =
   user id aktor pada context; `Route` = pola chi (bukan path mentah), sehingga middleware idempotency dipasang **per-endpoint**
   (`r.With`) — pada saat itu pola chi sudah final.
3. **`audit.Recorder` dipasang di API** pada pool runtime (`app_user`) dengan `WithSystemLog(writer)`; kegagalan perekaman masuk
   `system_logs` (`audit.write_failed`, ADR-0011). Tanpa pool, audit nil dan endpoint tidak merekam.
4. **Gerbang echo**: `/api/v1/_kernel/echo` hanya terdaftar bila `Options.DevEndpoints` **dan** `Options.Kernel` ada. Composition root
   menyalakannya lewat `Config.DevEndpointsEnabled()` = `ENVIRONMENT` ∈ {`development`, `test`}. **Staging dan production tidak
   pernah.** (Handover menulis `APP_ENV != production`; variabel nyata adalah `ENVIRONMENT`, dan staging sengaja dikecualikan karena
   biasanya terpapar jaringan.) Saat aktif, API mencatat satu peringatan saat start.
5. **Aktor di echo**: header `X-Dev-Actor: <uuid>` dibaca hanya oleh middleware `devActor` pada rute echo (UUID tidak valid atau nol →
   400 `validation_failed`). Sebelum autentikasi M03 ini satu-satunya cara memberi aktor; karena ikut gerbang di atas, ia tidak ada di
   staging/production. Tanpa header, request ber-idempotency mendapat 401 `authentication_required`.
6. **Bentuk endpoint** (semuanya lewat envelope docs/08):
   - `POST /api/v1/_kernel/echo` — idempotent (`Idempotency-Key` wajib), rate limit, body `{"message"}` (1–200 karakter setelah dipangkas),
     201 `{message, request_id, actor_id}`, merekam activity `kernel.echo` (metadata hanya `message_length`, bukan isi pesan).
   - `GET /api/v1/_kernel/echo/items` — paginasi cursor atas 25 item tetap (`item-001..025`, terbaru dulu), filter `?q=` (prefiks),
     limit bawaan 10, maksimum 20; tanpa database.
   - Batas laju `EchoRule`: 30 permintaan/menit per IP klien (`ip:<ClientIP>`), non-sensitif (fail open saat Redis mati).
7. **`OUTBOX_*` belum diekspos lewat env.** Default `kernel.RelayConfig` dipakai; env ditambahkan saat ada kebutuhan nyata (docs/10 §8 tidak
   mencantumkannya). Dicatat di sini agar M08/M15 tahu ini disengaja.
8. **`/metrics`** (T5d) memakai port terpisah lewat env baru `METRICS_PORT` (bawaan `0` = nonaktif; `.env.example` mengisi 9090 untuk
   dev), tanpa publish port di compose. Dikerjakan di T5d; keputusan dicatat di sini.

## Konsekuensi
- Tes sandbox-able memakai Redis sungguhan (`testkit.Redis`); tes yang menulis `activities` memakai PostgreSQL sungguhan
  (`echo_audit_test.go`). Tidak ada mock database.
- `NewHandler` kini menerima `Kernel`/`DevEndpoints`; semua pemanggil lama tetap valid (nol value = perilaku lama).
- Echo bukan API produk: tidak masuk OpenAPI publik (T5e) dan boleh dihapus tanpa dampak setelah endpoint nyata memakai toolkit
  yang sama (M03+).
