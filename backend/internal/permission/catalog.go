package permission

import "slices"

// Def adalah satu entri katalog permission core.
type Def struct {
	// Code berbentuk <resource>.<action> (docs/06 §5.1).
	Code string

	// Description tampil di editor role dan simulator.
	Description string
}

// Kode permission core (docs/06 §5.4, E:B-04, ADR-0022 butir 5). Konstanta tanpa tipe, jadi dapat
// dipakai langsung sebagai argumen Authorize("...") maupun definisi role. Menambah kode berarti
// menambah konstanta, entri di catalog di bawah, dan tes katalog (yang sengaja menyematkan daftar).

// Organisasi.
const (
	OrganizationRead         = "organization.read"
	OrganizationCreate       = "organization.create"
	OrganizationUpdate       = "organization.update"
	OrganizationArchive      = "organization.archive"
	OrganizationMemberManage = "organization.member.manage"
)

// Pengguna.
const (
	UserRead   = "user.read"
	UserCreate = "user.create"
	UserUpdate = "user.update"
	UserDelete = "user.delete"
)

// Profil (scope own).
const (
	ProfileRead   = "profile.read"
	ProfileUpdate = "profile.update"
)

// Grup.
const (
	GroupRead          = "group.read"
	GroupCreate        = "group.create"
	GroupUpdate        = "group.update"
	GroupDelete        = "group.delete"
	GroupManageMembers = "group.manage_members"
)

// Role.
const (
	RoleRead   = "role.read"
	RoleManage = "role.manage"
	RoleAssign = "role.assign"
)

// Permission.
const (
	PermissionRead     = "permission.read"
	PermissionAssign   = "permission.assign"
	PermissionSimulate = "permission.simulate"
)

// Policy.
const (
	PolicyRead   = "policy.read"
	PolicyManage = "policy.manage"
)

// Modul.
const (
	ModuleRead   = "module.read"
	ModuleManage = "module.manage"
	ModuleEnable = "module.enable"
)

// Pengaturan.
const (
	SettingsRead   = "settings.read"
	SettingsUpdate = "settings.update"
)

// Aktivitas.
const (
	ActivityReadOwn   = "activity.read_own"
	ActivityReadGroup = "activity.read_group"
	ActivityReadAll   = "activity.read_all"
	ActivityExport    = "activity.export"
)

// Log sistem.
const (
	SystemLogRead = "system_log.read"
)

// Notifikasi dan inbox (scope own).
const (
	NotificationRead = "notification.read"
	InboxRead        = "inbox.read"
)

// Percakapan dan pesan.
const (
	ConversationRead   = "conversation.read"
	ConversationCreate = "conversation.create"
	ConversationManage = "conversation.manage"
	MessageSend        = "message.send"
	MessageModerate    = "message.moderate"
)

// catalog adalah katalog core dalam urutan deklarasi (urutan dipakai sinkron dan tes).
var catalog = []Def{
	{OrganizationRead, "Melihat organisasi"},
	{OrganizationCreate, "Membuat organisasi (tingkat platform)"},
	{OrganizationUpdate, "Mengubah organisasi"},
	{OrganizationArchive, "Mengarsipkan organisasi (tingkat platform)"},
	{OrganizationMemberManage, "Mengelola keanggotaan organisasi"},
	{UserRead, "Melihat pengguna"},
	{UserCreate, "Membuat pengguna"},
	{UserUpdate, "Mengubah pengguna"},
	{UserDelete, "Menghapus pengguna"},
	{ProfileRead, "Melihat profil sendiri"},
	{ProfileUpdate, "Mengubah profil sendiri"},
	{GroupRead, "Melihat grup"},
	{GroupCreate, "Membuat grup"},
	{GroupUpdate, "Mengubah grup"},
	{GroupDelete, "Menghapus grup"},
	{GroupManageMembers, "Mengelola anggota grup"},
	{RoleRead, "Melihat role"},
	{RoleManage, "Membuat, mengubah, dan mengarsipkan role"},
	{RoleAssign, "Menetapkan dan mencabut role"},
	{PermissionRead, "Melihat katalog permission"},
	{PermissionAssign, "Mengatur permission pada role"},
	{PermissionSimulate, "Menjalankan simulator permission"},
	{PolicyRead, "Melihat policy"},
	{PolicyManage, "Membuat dan mengubah policy"},
	{ModuleRead, "Melihat modul"},
	{ModuleManage, "Mengelola siklus hidup modul (tingkat platform)"},
	{ModuleEnable, "Mengaktifkan dan menonaktifkan modul pada organisasi"},
	{SettingsRead, "Melihat pengaturan"},
	{SettingsUpdate, "Mengubah pengaturan"},
	{ActivityReadOwn, "Melihat aktivitas sendiri"},
	{ActivityReadGroup, "Melihat aktivitas grup"},
	{ActivityReadAll, "Melihat semua aktivitas (organisasi, atau platform bila diberikan di scope platform)"},
	{ActivityExport, "Mengekspor aktivitas"},
	{SystemLogRead, "Melihat log sistem"},
	{NotificationRead, "Melihat notifikasi sendiri"},
	{InboxRead, "Melihat inbox sendiri"},
	{ConversationRead, "Melihat percakapan"},
	{ConversationCreate, "Membuat percakapan"},
	{ConversationManage, "Mengelola percakapan"},
	{MessageSend, "Mengirim pesan"},
	{MessageModerate, "Memoderasi pesan"},
}

// Catalog mengembalikan salinan katalog permission core.
func Catalog() []Def { return slices.Clone(catalog) }
