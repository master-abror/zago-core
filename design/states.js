// Pengalih state demo: <body data-states="normal,loading,..."> -> bilah bawah; state aktif dari ?state= (disalin ke body[data-state]).
(() => {
  const L = { normal: 'Normal', loading: 'Memuat', empty: 'Kosong', error: 'Galat umum', forbidden: 'Terlarang (403)', expired: 'Sesi habis', validation: 'Galat validasi',
    success: 'Sukses', allow: 'ALLOW', csrf: '403 CSRF', paged: 'Halaman 2', archived: 'Terarsip', platform: 'Konteks Platform', viewer: 'Tanpa role.manage', slugtaken: '409 slug dipakai',
    invscope: '400 scope tidak valid', permnf: '422 permission tidak ada', grant: '403 melebihi wewenang', scope: '403 scope_mismatch', alone: 'Tanpa sesi lain', short: 'Terlalu pendek', long: 'Terlalu panjang', same: 'Sama dengan lama',
    wrongpw: 'Password lama salah', more: 'Memuat lagi', moreerr: 'Galat muat lagi', end: 'Akhir daftar', invalid: '401 kredensial', suspended: '403 ditangguhkan', locked: '423 dikunci', rate: '429 batas laju',
    notfound: '404 peran tidak ada', system: 'Peran sistem', saveerr: 'Galat simpan',
    revoked: 'Dicabut', lapsed: 'Kedaluwarsa', all: 'Semua status', badcursor: '400 cursor tidak valid', badfilter: '400 filter tidak valid', nofilter: 'Tanpa hasil filter',
    revlast: '409 super-admin terakhir', revgone: '404/409 sudah dicabut', revgrant: '403 cabut melebihi wewenang', revcsrf: '403 CSRF (cabut)', reverr: 'Galat cabut',
    subjnf: '400 user tidak ada', grpnf: '400 grup tidak ada', expinv: '400 kedaluwarsa tidak valid', exists: '409 sudah ada', rolearch: '409 role terarsip',
    rolenf: '404 role tidak ada', idem: '409 masih diproses', idemconf: '422 kunci bentrok',
    active: 'Hanya aktif', disabled: 'Hanya nonaktif', edit: 'Ubah policy', platedit: 'Ubah global (Platform)', exprsyntax: '400 ekspresi tidak valid', exprcomplex: '400 ekspresi terlalu kompleks', actprot: '400 aksi dilindungi', actunk: '400 aksi tidak ada', codetaken: '409 kode dipakai', limit: '409 batas 50 policy', emptypatch: '400 tanpa perubahan', globalro: 'Global (baca-saja)', pviewer: 'Tanpa policy.manage', pnotfound: '404 policy tidak ada', saved: 'Perubahan tersimpan', tglimit: '409 batas (aktifkan)', tggone: '404 tidak ada (ubah status)', tgcsrf: '403 CSRF (ubah status)', tgerr: 'Galat ubah status', tgscope: '403 scope_mismatch (grup)',
    noperm: 'DENY no_permission', scopemis: 'DENY scope_mismatch', policy: 'DENY policy_denied', polfail: 'DENY policy_evaluation_failed', nopolread: 'DENY tanpa policy.read', deferred: 'ALLOW + policy tertunda', usernf: '400 user_id tidak ada', attrres: '400 atribut dicadangkan', attrbad: '400 atribut tidak sah', grpscope: '403 scope_mismatch (grup aktif)', s403: '403 seragam', s404: '404 halaman', s500: '500 galat umum', s503: '503 Retry-After', retry: 'Mencoba lagi', offlinebar: 'Offline (banner)', offline: 'Offline (halaman)', boundary: 'Error boundary', sent: 'Terkirim (jawaban generik)' };
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
