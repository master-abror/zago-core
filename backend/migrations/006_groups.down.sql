-- 006_groups.down.sql
DROP TABLE groups;  -- trigger trg_groups_hierarchy ikut terhapus bersama tabel
DROP FUNCTION check_group_hierarchy();
