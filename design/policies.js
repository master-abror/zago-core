// Perilaku demo Policy per state (?state=): daftar (filter, aktifkan/nonaktifkan) dan editor buat/ubah. Di SPA Svelte menggantikan seluruhnya; lihat DESIGN-HANDOFF.md (F6).
(() => {
  const $ = id => document.getElementById(id), st = document.body.dataset.state, T = (k, p) => ZApp.t(k, p);
  const dyn = []; // teks bentukan skrip yang harus digambar ulang saat bahasa diganti (apply() hanya menyentuh [data-i18n])
  document.addEventListener('click', e => { if (e.target.closest('[data-action="reload"]')) location.reload(); });
  ['change', 'click'].forEach(ev => document.addEventListener(ev, e => {
    if (e.target.closest('[data-pref="lang"], [data-set^="lang:"]')) dyn.forEach(f => f());
  }));

  // ---------------------------------------------------------------- Daftar policy
  const table = $('po-table');
  if (table) {
    const status = $('po-status');
    // Filter (demo): status -> state. Di SPA: GET /policies?status=all|active|disabled dengan offset dimulai ulang dari awal.
    const S2ST = { all: 'normal', active: 'active', disabled: 'disabled' };
    status.value = { active: 'active', disabled: 'disabled', tglimit: 'disabled' }[st] || 'all';
    $('po-filter').addEventListener('submit', e => {
      e.preventDefault();
      location.search = '?state=' + (status.value === 'all' && ['platform', 'pviewer'].includes(st) ? st : S2ST[status.value]);
    });
    if (st === 'pviewer') {
      // Pola izin F1d: aksi ubah tidak dirender tanpa policy.manage; baris tetap bisa dibuka untuk dilihat.
      document.querySelectorAll('[data-manage]').forEach(el => { el.hidden = true; });
      table.querySelectorAll('a[href^="policy-edit.html"]').forEach(a => { a.href = 'policy-edit.html?state=pviewer'; });
    }
    const visible = () => table.querySelectorAll('tbody tr:not([hidden])').length;
    const renderRange = () => {
      const n = visible(), off = st === 'paged' ? 20 : 0;
      $('po-range').textContent = T('po.range', { a: n ? off + 1 : 0, b: off + n, n: off + n });
      table.querySelectorAll('[data-i18n-more]').forEach(el => { el.textContent = T('po.more', { n: el.dataset.n }); });
    };
    dyn.push(renderRange); renderRange();

    // Aktifkan/nonaktifkan: dialog biasa; hasil ditentukan state (demo). Galat tampil DI DALAM dialog agar konteks tidak hilang.
    // Di SPA: PATCH /policies/{id} dengan {status}. Menonaktifkan selalu diizinkan; mengaktifkan divalidasi penuh dan tunduk pada batas 50.
    const dlg = $('dlg-toggle'), OUT = {
      tglimit: ['po.limit.t', 'po.limit.m'], tggone: ['po.gone.t', 'po.gone.m', true], tgcsrf: ['al.csrf.t', 'al.csrf.m', true],
      tgerr: ['al.error.t', 'al.error.m'], tgscope: ['po.scope.t', 'po.scope.m'] }[st];
    let cur = null;
    const reset = () => { $('tg-alert').hidden = true; $('tg-reload').hidden = true; $('tg-spin').hidden = true; $('tg-ok').disabled = false; };
    const okLabel = () => { $('tg-ok-l').textContent = T(cur.to === 'disable' ? 'po.dis.ok' : 'po.en.ok'); };
    document.addEventListener('click', e => {
      const b = e.target.closest('[data-action="po-toggle"]');
      if (b) {
        cur = { row: b.closest('tr'), btn: b, to: b.dataset.to, name: b.dataset.name };
        const dis = cur.to === 'disable';
        reset(); $('tg-t').textContent = T(dis ? 'po.dis.t' : 'po.en.t'); $('tg-d').textContent = T(dis ? 'po.dis.d' : 'po.en.d', { name: cur.name });
        $('tg-ok').className = `btn d-inline-flex align-items-center gap-2 ${dis ? 'btn-danger' : 'btn-primary'}`; okLabel(); dlg.showModal(); return;
      }
      if (!e.target.closest('[data-action="tg-confirm"]')) return;
      $('tg-ok').disabled = true; $('tg-spin').hidden = false; $('tg-ok-l').textContent = T(cur.to === 'disable' ? 'po.dis.busy' : 'po.en.busy');
      setTimeout(() => {
        // Demo: tglimit hanya berlaku saat mengaktifkan; state galat lain berlaku untuk kedua arah.
        if (OUT && (st !== 'tglimit' || cur.to === 'enable')) {
          $('tg-spin').hidden = true; $('tg-ok').disabled = false; okLabel();
          $('tg-at').textContent = T(OUT[0]); $('tg-am').textContent = T(OUT[1]); $('tg-reload').hidden = !OUT[2]; $('tg-alert').hidden = false;
          $('tg-alert').tabIndex = -1; $('tg-alert').focus(); return;
        }
        dlg.close('confirm');
      }, 700);
    });
    dlg.addEventListener('close', () => {
      const ok = dlg.returnValue === 'confirm'; dlg.returnValue = ''; reset();
      if (!ok || !cur) return;
      const { row, btn, to, name } = cur, dis = to === 'disable';
      // Filter aktif/nonaktif: baris yang tidak lagi cocok dengan filter hilang dari daftar.
      const gone = (st === 'active' && dis) || ((st === 'disabled' || st === 'tglimit') && !dis);
      row.children[3].innerHTML = dis ? '<span class="badge bg-body-secondary text-body border" data-i18n="po.st.disabled"></span>'
        : '<span class="badge bg-success-subtle text-success-emphasis border border-success-subtle" data-i18n="po.st.active"></span>';
      btn.dataset.to = dis ? 'enable' : 'disable'; btn.className = `btn btn-sm ${dis ? 'btn-outline-secondary' : 'btn-outline-danger'}`;
      btn.querySelector('[data-i18n]').dataset.i18n = dis ? 'po.enable' : 'po.disable';
      row.hidden = gone; renderRange(); ZApp.apply(); renderRange();
      ZUI.toast('success', dis ? 'po.tg.dis.ok' : 'po.tg.en.ok', { name });
      (gone ? $('po-h1') : btn).focus(); // baris hilang: fokus ke judul halaman; jika tidak, tombol yang sama
    });
  }

  // ---------------------------------------------------------------- Editor policy
  const form = $('pf-form');
  if (form) {
    const POLS = {
      org: { code: 'user.protect_locked', name: 'Lindungi pengguna terkunci', desc: 'Mencegah perubahan atau penghapusan pengguna yang sedang terkunci.', effect: 'deny', actions: ['user.update', 'user.delete'], expr: 'resource.status == "locked"', active: true },
      glob: { code: 'user.delete.admin_guard', name: 'Larang hapus akun admin', desc: 'Akun admin tidak boleh dihapus lewat jalur biasa.', effect: 'deny', actions: ['user.delete'], expr: 'resource.is_admin == true', active: true },
      fresh: { code: 'user.update.limit_admin', name: 'Batasi ubah pengguna admin', desc: '', effect: 'deny', actions: ['user.update'], expr: 'resource.is_admin == true', active: true } };
    const MODE = { edit: 'edit', platedit: 'edit', emptypatch: 'edit', saved: 'edit', globalro: 'ro', pviewer: 'ro' }, mode = MODE[st] || 'new';
    const EMPTY = ['normal', 'platform', 'validation', 'loading'];
    const src = mode === 'new' ? (EMPTY.includes(st) ? null : POLS.fresh) : { platedit: POLS.glob, globalro: POLS.glob }[st] || POLS.org;
    const boxes = [...form.querySelectorAll('[data-perm]')], BYTES = 2000, MAXACT = 50;
    const bytes = v => new TextEncoder().encode(v).length, runes = v => [...v].length;

    // Judul dan label tombol mengikuti mode.
    const titleKey = 'pf.title.' + { new: 'new', edit: 'edit', ro: 'view' }[mode];
    $('pf-h1').dataset.i18n = titleKey; document.querySelector('title').dataset.i18n = titleKey;
    $('pf-sub').dataset.i18n = mode === 'ro' ? 'pf.sub.view' : 'pf.sub';
    $('pf-label').dataset.i18n = mode === 'edit' ? 'pf.save' : 'pf.create';

    // Prefill.
    if (src) {
      $('pf-code').value = src.code; $('pf-name').value = src.name; $('pf-desc').value = src.desc; $('pf-expr').value = src.expr; $('pf-active').checked = src.active;
      $('pf-eff-' + src.effect).checked = true; boxes.forEach(b => { b.checked = src.actions.includes(b.value); });
    }
    // Modul katalog tertutup kecuali berisi aksi terpilih: field Ekspresi tidak terdorong jauh ke bawah; pencarian membuka modul yang cocok.
    form.querySelectorAll('[data-perm-mod]').forEach(m => { m.open = !!m.querySelector('[data-perm]:checked'); });
    if (st === 'exprsyntax') $('pf-expr').value = 'resource.owner_id == ';
    if (st === 'exprcomplex') $('pf-expr').value = Array(130).fill('resource.a==1').join('||'); // 1948 byte (di bawah 2000) tetapi minimal 520 token (batas 400)
    if (mode !== 'new') $('pf-code').readOnly = true;

    // Penghitung byte ekspresi. Pembaca layar hanya diberi tahu saat melewati ambang, bukan tiap ketikan.
    let lvl = 0;
    const renderBytes = () => {
      const n = bytes($('pf-expr').value); $('pf-expr-n').textContent = T('pf.expr.bytes', { n });
      $('pf-expr-n').classList.toggle('text-danger-emphasis', n > BYTES);
      const l = n > BYTES ? 2 : n >= BYTES * 0.9 ? 1 : 0;
      if (l !== lvl) { lvl = l; $('pf-expr-sr').textContent = l === 2 ? T('pf.expr.over') : l === 1 ? T('pf.expr.near') : ''; }
    };
    $('pf-expr').addEventListener('input', renderBytes);

    // Pemilih aksi: pilih semua per modul (tri-state), filter teks, penghitung. Pola sama dengan editor permission peran (roles.js); di SPA satu komponen bersama.
    const ed = form.querySelector('[data-perm-editor]');
    const sync = () => {
      ed.querySelectorAll('[data-perm-mod]').forEach(m => {
        const b = [...m.querySelectorAll('[data-perm]')], on = b.filter(x => x.checked).length, all = m.querySelector('[data-perm-all-mod]');
        all.checked = on === b.length; all.indeterminate = on > 0 && on < b.length; m.querySelector('[data-perm-modcount]').textContent = `${on}/${b.length}`;
      });
      ed.querySelector('[data-perm-count]').textContent = T('pf.count', { n: boxes.filter(x => x.checked).length });
    };
    ed.addEventListener('change', e => {
      if (e.target.matches('[data-perm-all-mod]')) {
        e.target.closest('[data-perm-mod]').querySelectorAll('[data-perm-row]:not([hidden]) [data-perm]:not(:disabled)').forEach(x => { x.checked = e.target.checked; });
      }
      sync();
    });
    const filter = v => {
      v = v.trim().toLowerCase();
      ed.querySelectorAll('[data-perm-row]').forEach(r => { r.hidden = v !== '' && !r.textContent.toLowerCase().includes(v); });
      ed.querySelectorAll('[data-perm-res]').forEach(f => { f.hidden = !f.querySelector('[data-perm-row]:not([hidden])'); });
      ed.querySelectorAll('[data-perm-mod]').forEach(m => { m.hidden = !m.querySelector('[data-perm-row]:not([hidden])'); if (v && !m.hidden) m.open = true; });
      ed.querySelector('[data-perm-none]').hidden = !!ed.querySelector('[data-perm-row]:not([hidden])');
    };
    ed.addEventListener('input', e => { if (e.target.matches('[data-perm-q]')) filter(e.target.value); });

    // Baca-saja: yang tampil hanya aksi terpilih (daftar katalog penuh tidak berguna untuk dibaca).
    if (mode === 'ro') {
      $('pf-fs').disabled = true; $('pf-btns').hidden = true; $('pf-back').hidden = false;
      ed.querySelector('[data-perm-tools]').hidden = true;
      ed.querySelectorAll('[data-perm-all-mod]').forEach(x => { x.closest('.form-check').hidden = true; });
      ed.querySelectorAll('[data-perm-row]').forEach(r => { r.hidden = !r.querySelector('[data-perm]').checked; });
      ed.querySelectorAll('[data-perm-res]').forEach(f => { f.hidden = !f.querySelector('[data-perm-row]:not([hidden])'); });
      ed.querySelectorAll('[data-perm-mod]').forEach(m => { m.hidden = !m.querySelector('[data-perm-row]:not([hidden])'); m.open = true; });
      $('pf-actions-h').hidden = true;
    }
    sync(); renderBytes(); dyn.push(sync, renderBytes);

    // Galat per field: teks lewat kunci i18n; aria-describedby dipulihkan ke dasar saat galat dibersihkan.
    const BASE = {}; ['pf-code', 'pf-name', 'pf-desc', 'pf-expr', 'pf-actions'].forEach(id => { BASE[id] = $(id).getAttribute('aria-describedby') || ''; });
    const setDesc = (el, v) => { v.trim() ? el.setAttribute('aria-describedby', v.trim()) : el.removeAttribute('aria-describedby'); };
    const markErr = (id, key, focus) => {
      const el = $(id), msg = $(`${id}-e`);
      msg.dataset.i18n = key; msg.hidden = false; setDesc(el, `${BASE[id]} ${id}-e`);
      if (id === 'pf-actions') { if (focus) { el.tabIndex = -1; el.focus(); } return; }
      el.classList.add('is-invalid'); el.setAttribute('aria-invalid', 'true'); if (focus) el.focus();
    };
    const clearErr = id => {
      const el = $(id), msg = $(`${id}-e`); msg.hidden = id === 'pf-actions'; delete msg.dataset.i18n; msg.textContent = ''; setDesc(el, BASE[id]);
      el.classList.remove('is-invalid'); el.removeAttribute('aria-invalid');
    };
    const FIELDS = ['pf-code', 'pf-name', 'pf-desc', 'pf-expr', 'pf-actions'];

    const box = $('pf-alert');
    const alertShow = (tone, tk, mk, fl = '') => {
      box.className = `alert alert-${tone}`; box.setAttribute('role', tone === 'success' || tone === 'info' ? 'status' : 'alert'); box.hidden = false;
      $('pf-at').dataset.i18n = tk; $('pf-am').dataset.i18n = mk; $('pf-reload').hidden = !fl.includes('r'); $('pf-link').hidden = !fl.includes('l');
    };
    const TONE = {
      validation: ['danger', 'al.validation.t', 'al.validation.m'], exprsyntax: ['danger', 'al.validation.t', 'al.validation.m'], exprcomplex: ['danger', 'al.validation.t', 'al.validation.m'],
      actprot: ['danger', 'al.validation.t', 'al.validation.m'], actunk: ['danger', 'al.validation.t', 'al.validation.m', 'r'],
      codetaken: ['warning', 'pf.taken.t', 'pf.taken.m'], limit: ['danger', 'po.limit.t', 'po.limit.m'], emptypatch: ['info', 'pf.nochange.t', 'pf.nochange.m'],
      scope: ['danger', 'po.scope.t', 'po.scope.m'], csrf: ['danger', 'al.csrf.t', 'al.csrf.m', 'r'], error: ['danger', 'al.error.t', 'al.error.m'],
      success: ['success', 'pf.ok.t', 'pf.ok.m', 'l'], saved: ['success', 'pf.saved.t', 'pf.saved.m'] };
    const ERR = { validation: [['pf-code', 'pf.code.req'], ['pf-name', 'pf.name.req'], ['pf-expr', 'pf.expr.req'], ['pf-actions', 'pf.actions.req']],
      exprsyntax: [['pf-expr', 'pf.expr.syntax']], exprcomplex: [['pf-expr', 'pf.expr.complex']], actprot: [['pf-actions', 'pf.actions.prot']],
      actunk: [['pf-actions', 'pf.actions.unk']], codetaken: [['pf-code', 'pf.code.taken']] };
    (ERR[st] || []).forEach(([id, key], i) => markErr(id, key, i === 0));
    if (TONE[st]) alertShow(...TONE[st]);
    if (['success', 'saved', 'emptypatch'].includes(st)) { box.tabIndex = -1; box.focus(); }
    if (st === 'success') { $('pf-fs').disabled = true; $('pf-btns').hidden = true; }
    if (st === 'loading') { $('pf-fs').disabled = true; form.setAttribute('aria-busy', 'true'); $('pf-spin').hidden = false; $('pf-label').dataset.i18n = 'btn.loading'; $('pf-save').disabled = true; }

    // Isian dalam bentuk yang dikirim: nama dipangkas (server menolak spasi di tepi, tidak memangkasnya).
    const read = () => ({ code: $('pf-code').value.trim(), name: $('pf-name').value.trim(), desc: $('pf-desc').value, expr: $('pf-expr').value,
      effect: form.querySelector('input[name="pf-eff"]:checked').value, actions: boxes.filter(b => b.checked).map(b => b.value).sort(), active: $('pf-active').checked });
    const initial = JSON.stringify(read());

    // Kirim (demo): validasi sisi klien, spinner, lalu hasil. Di SPA: POST /policies (buat) atau PATCH /policies/{id} hanya dengan field yang berubah.
    // Catatan kontrak: tidak ada GET /policies/{id}; halaman ubah mengambil record dari daftar (state router atau cari di GET /policies).
    form.addEventListener('submit', e => {
      e.preventDefault();
      FIELDS.forEach(clearErr); box.hidden = true;
      const v = read(), bad = [];
      if (mode === 'new') {
        if (!v.code) bad.push(['pf-code', 'pf.code.req']);
        else if (v.code.length > 150 || !/^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$/.test(v.code)) bad.push(['pf-code', 'pf.code.bad']);
      }
      if (!v.name) bad.push(['pf-name', 'pf.name.req']); else if (runes(v.name) > 150) bad.push(['pf-name', 'pf.name.bad']);
      if (runes(v.desc) > 1000) bad.push(['pf-desc', 'pf.desc.bad']);
      if (!v.expr.trim()) bad.push(['pf-expr', 'pf.expr.req']); else if (bytes(v.expr) > BYTES) bad.push(['pf-expr', 'pf.expr.over']);
      if (!v.actions.length) bad.push(['pf-actions', 'pf.actions.req']); else if (v.actions.length > MAXACT) bad.push(['pf-actions', 'pf.actions.max']);
      if (bad.length) { alertShow('danger', 'al.validation.t', 'al.validation.m'); bad.forEach(([id, k], i) => markErr(id, k, i === 0)); ZApp.apply(); return; }
      if (mode === 'edit' && JSON.stringify(v) === initial) { alertShow(...TONE.emptypatch); box.tabIndex = -1; box.focus(); ZApp.apply(); return; }
      $('pf-fs').disabled = true; $('pf-save').disabled = true; $('pf-spin').hidden = false; $('pf-label').dataset.i18n = 'btn.loading'; ZApp.apply();
      setTimeout(() => { location.search = '?state=' + (mode === 'edit' ? 'saved' : 'success'); }, 700);
    });
  }
  ZApp.apply(); dyn.forEach(f => f());
})();
