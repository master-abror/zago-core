// Pengalih state demo: <body data-states="normal,loading,..."> -> bilah bawah; state aktif dari ?state= (disalin ke body[data-state]).
(() => {
  const L = { normal: 'Normal', loading: 'Memuat', empty: 'Kosong', error: 'Galat umum', forbidden: 'Terlarang (403)', expired: 'Sesi habis', validation: 'Galat validasi',
    success: 'Sukses', csrf: '403 CSRF', paged: 'Halaman 2', archived: 'Terarsip', platform: 'Konteks Platform', viewer: 'Tanpa role.manage', slugtaken: '409 slug dipakai',
    invscope: '400 scope tidak valid', permnf: '422 permission tidak ada', grant: '403 melebihi wewenang', scope: '403 scope_mismatch', alone: 'Tanpa sesi lain', short: 'Terlalu pendek', long: 'Terlalu panjang', same: 'Sama dengan lama',
    wrongpw: 'Password lama salah', more: 'Memuat lagi', moreerr: 'Galat muat lagi', end: 'Akhir daftar', invalid: '401 kredensial', suspended: '403 ditangguhkan', locked: '423 dikunci', rate: '429 batas laju' };
  const list = (document.body.dataset.states || '').split(',').filter(Boolean);
  if (!list.length) return;
  const cur = new URLSearchParams(location.search).get('state') || list[0];
  document.body.dataset.state = cur;
document.querySelectorAll('[data-when]').forEach(e => { e.hidden = !e.dataset.when.split(' ').includes(cur); });
  const nav = document.createElement('nav');
  nav.className = 'z-states';
  nav.setAttribute('aria-label', 'Pengalih state (demo)');
  nav.innerHTML = list.map(s => `<a href="?state=${s}"${s === cur ? ' aria-current="true"' : ''}>${L[s] || s}</a>`).join('');
  document.body.append(nav);
})();
