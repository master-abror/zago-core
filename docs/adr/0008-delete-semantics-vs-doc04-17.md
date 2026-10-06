# ADR-0008 — Semantik penghapusan: DDL menang atas teks 04 §17

**Status:** Diterima · **Tanggal:** 2026-10-05

## Konteks
04 §17 (Cascades) menyebut penghapusan organisasi "cascade ke groups, memberships", dan penghapusan group "diblokir selama roles
**atau role_assignments** masih merujuknya". DDL di 04 §5–§8 berkata lain: `groups.organization_id` dan
`organization_memberships.organization_id` adalah `ON DELETE RESTRICT`; `role_assignments.scope_id` polimorfik (tanpa FK).

## Keputusan
**DDL adalah sumber kebenaran**; 04 §17 disinkronkan dengannya (doc-sync M01). Perilaku yang diuji dan berlaku:
- Hapus organisasi: **cascade** ke `roles`, `role_assignments`, `module_installations`, `conversations`, `invitations`;
  **diblokir (RESTRICT)** selama masih ada `groups` atau `organization_memberships`. Jalur normal adalah soft delete (02 §36, 03 §49).
  `users`, `activities`, `security_events`, `outbox_events` tidak tersentuh.
- Hapus group: cascade ke `group_memberships` dan `invitations` ber-`group_id` itu; **diblokir** oleh `roles.group_id` dan oleh sub-group.
- `role_assignments` ber-`scope_type='group'` **tidak** diblokir database. Aplikasi (layanan group, M05) wajib mencabutnya saat
  group diarsipkan/dihapus. Tidak ditambahkan trigger di M01 (itu keputusan desain baru); dicatat sebagai utang di HANDOVER-M01.
