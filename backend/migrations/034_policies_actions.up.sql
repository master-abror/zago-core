-- 034_policies_actions.up.sql
-- ADR-0022 butir 2 dan 10: policy terikat ke action lewat policies.actions text[].
-- Isi: kode permission persis (invoice.read) atau semua action satu resource (invoice.*).
-- Tidak boleh kosong; keanggotaan terhadap katalog (core di kode, modul di tabel permissions)
-- divalidasi aplikasi saat tulis, karena array tidak dapat menjadi foreign key.

-- Tidak ada cara aman menebak actions untuk baris lama (policy tanpa action = policy deny yang diam-diam
-- berhenti menolak). Belum ada kode yang menulis policies; bila tabel ternyata berisi, gagal keras.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM policies) THEN
        RAISE EXCEPTION 'policies tidak kosong: isi actions tiap baris harus ditentukan manual sebelum migrasi 034';
    END IF;
END
$$;

-- Pertahanan bentuk di database (aplikasi tetap memvalidasi terhadap katalog). Bentuk elemen mengikuti
-- permission.codePattern, ditambah varian <resource>.* ; satu dimensi, 1..50 elemen, tanpa NULL, tanpa duplikat.
-- COALESCE: CHECK meloloskan NULL, jadi hasil yang tak tentu harus dipaksa menjadi false.
CREATE FUNCTION policy_actions_valid(arr text[])
RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS $$
    SELECT COALESCE(
        arr IS NOT NULL
        AND array_ndims(arr) = 1
        AND cardinality(arr) BETWEEN 1 AND 50
        AND NOT EXISTS (
            SELECT 1 FROM unnest(arr) AS a(v)
             WHERE v IS NULL
                OR length(v) > 255
                OR v !~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*\.(\*|[a-z][a-z0-9_]*)$'
        )
        AND cardinality(arr) = (SELECT count(DISTINCT v) FROM unnest(arr) AS a(v)),
        false)
$$;

ALTER TABLE policies
    ADD COLUMN actions text[] NOT NULL,
    ADD CONSTRAINT ck_policies_actions CHECK (policy_actions_valid(actions));

-- pencarian policy untuk sebuah action: actions && ARRAY[kode, 'resource.*']
CREATE INDEX idx_policies_actions ON policies USING gin (actions);
