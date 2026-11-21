// Perilaku demo Roles per state (?state=) dan editor permission. Di SPA Svelte menggantikan seluruhnya; lihat DESIGN-HANDOFF.md (F6).
(() => {
  const $ = id => document.getElementById(id), st = document.body.dataset.state, T = (k, p) => ZApp.t(k, p);
  document.addEventListener('click', e => {
    if (e.target.closest('[data-action="reload"]')) location.reload();
    if (e.target.closest('[data-action="apply-filter"]')) location.search = '?state=' + ($('rl-status').value === 'archived' ? 'archived' : 'normal');
  });
  const sel = $('rl-status'); if (sel && st === 'archived') sel.value = 'archived';
  const rg = $('rl-range');
  if (rg) { const r = { paged: [26, 32, 32], archived: [1, 1, 1], platform: [1, 4, 4] }[st] || [1, 6, 6]; rg.textContent = T('rl.range', { a: r[0], b: r[1], n: r[2] }); }

  // Editor permission: pilih semua per modul (tri-state), filter teks, penghitung.
  const ed = document.querySelector('[data-perm-editor]');
  const sync = () => {
    if (!ed) return;
    ed.querySelectorAll('[data-perm-mod]').forEach(m => {
      const b = [...m.querySelectorAll('[data-perm]')], on = b.filter(x => x.checked).length, all = m.querySelector('[data-perm-all-mod]');
      all.checked = on === b.length; all.indeterminate = on > 0 && on < b.length; m.querySelector('[data-perm-modcount]').textContent = `${on}/${b.length}`;
    });
    const b = [...ed.querySelectorAll('[data-perm]')];
    ed.querySelector('[data-perm-count]').textContent = T('pe.count', { n: b.filter(x => x.checked).length, m: b.length });
  };
  if (ed) {
    ed.addEventListener('change', e => {
      if (e.target.matches('[data-perm-all-mod]')) {
        e.target.closest('[data-perm-mod]').querySelectorAll('[data-perm-row]:not([hidden]) [data-perm]:not(:disabled)').forEach(x => { x.checked = e.target.checked; });
      }
      sync();
    });
    ed.addEventListener('input', e => {
      if (!e.target.matches('[data-perm-q]')) return;
      const v = e.target.value.trim().toLowerCase();
      ed.querySelectorAll('[data-perm-row]').forEach(r => { r.hidden = v !== '' && !r.textContent.toLowerCase().includes(v); });
      ed.querySelectorAll('[data-perm-res]').forEach(f => { f.hidden = !f.querySelector('[data-perm-row]:not([hidden])'); });
      ed.querySelectorAll('[data-perm-mod]').forEach(m => { m.hidden = !m.querySelector('[data-perm-row]:not([hidden])'); if (v && !m.hidden) m.open = true; });
      ed.querySelector('[data-perm-none]').hidden = !!ed.querySelector('[data-perm-row]:not([hidden])');
    });
    sync();
  }

  // Detail peran (F3b). Di SPA: PATCH detail dan PUT permission adalah dua permintaan terpisah; bilah simpan hanya untuk permission.
  const rdf = $('rd-form');
  if (rdf) {
    const R = { system: ['Organization Admin', 'organization-admin', 'system', 'active', 'Mengelola satu organisasi.'], archived: ['Kasir', 'kasir', 'org', 'archived', 'Peran lama, sudah diarsipkan.'] }[st]
      || ['Keuangan', 'keuangan', 'org', 'active', 'Mengelola data keuangan.'];
    const RO = ['system', 'archived', 'viewer'].includes(st);
    const BADGE = { system: 'bg-info-subtle text-info-emphasis border border-info-subtle', org: 'bg-primary-subtle text-primary-emphasis border border-primary-subtle',
      active: 'bg-success-subtle text-success-emphasis border border-success-subtle', archived: 'bg-body-secondary text-body border' };
    $('rd-title').textContent = R[0]; $('rd-slug').textContent = R[1]; $('rd-name').value = R[0]; $('rd-desc').value = R[4];
    const bc = $('rd-bc'); bc.textContent = R[0]; bc.removeAttribute('data-i18n');
    [['rd-b-type', R[2], 'rl.type.'], ['rd-b-st', R[3], 'rl.st.']].forEach(([id, v, p]) => { const el = $(id); el.className = `badge ${BADGE[v]}`; el.dataset.i18n = p + v; });
    const boxes = [...ed.querySelectorAll('[data-perm]')];
    const keep = { system: c => !c.startsWith('announcements.'), archived: c => ['organization.read', 'user.read'].includes(c) }[st] || (c => ['organization.read', 'user.read', 'role.read'].includes(c));
    boxes.forEach(x => { x.checked = keep(x.value); }); sync();
    const base = () => boxes.filter(x => x.checked).map(x => x.value).sort().join();
    let b0 = base();
    if (RO) { boxes.forEach(x => { x.disabled = true; }); ed.querySelectorAll('[data-perm-all-mod]').forEach(x => { x.disabled = true; }); $('rd-fs').disabled = true; }
    const bar = $('rd-bar'), live = $('rd-live'); let was = false;
    const upd = () => {
      const on = !RO && base() !== b0; bar.hidden = !on;
      if (on !== was) { live.textContent = on ? T('rd.dirty') : ''; was = on; }
    };
    ed.addEventListener('change', upd);
    const save = () => {
      $('rd-save').disabled = true; $('rd-spin').hidden = false; $('rd-save-l').dataset.i18n = 'btn.loading'; ZApp.apply();
      setTimeout(() => {
        b0 = base(); $('rd-spin').hidden = true; $('rd-save').disabled = false; $('rd-save-l').dataset.i18n = 'rd.save';
        upd(); ZUI.toast('success', 'rd.saved'); ZApp.apply(); $('rd-h-perms').focus(); // bilah hilang: fokus ke judul bagian
      }, 700);
    };
    document.addEventListener('click', e => {
      const a = e.target.closest('[data-action]')?.dataset.action;
      if (a === 'rd-cancel') { const was0 = b0.split(','); boxes.forEach(x => { x.checked = was0.includes(x.value); }); sync(); upd(); $('rd-h-perms').focus(); }
      if (a === 'rd-save') { if (boxes.some(x => x.checked)) save(); else $('dlg-empty').showModal(); }
    });
    $('dlg-empty').addEventListener('close', e => { if (e.target.returnValue === 'confirm') save(); e.target.returnValue = ''; });
    $('dlg-archive').addEventListener('close', e => { const ok = e.target.returnValue === 'confirm'; e.target.returnValue = ''; if (ok) location.search = '?state=archived'; });
    const nameErr = on => {
      const el = $('rd-name'); el.classList.toggle('is-invalid', on); if (on) el.setAttribute('aria-invalid', 'true'); else el.removeAttribute('aria-invalid');
      el.setAttribute('aria-describedby', on ? 'rd-name-h rd-name-e' : 'rd-name-h'); if (on) { $('rd-name-e').dataset.i18n = 'rn.name.req'; ZApp.apply(); el.focus(); }
    };
    rdf.addEventListener('submit', e => { e.preventDefault(); if (!$('rd-name').value.trim()) nameErr(true); else { nameErr(false); ZUI.toast('success', 'rd.saved.d'); } });
    const box = $('rd-alert');
    const TONE = { validation: ['danger', 'al.validation.t', 'al.validation.m'], grant: ['danger', 'al.grant.t', 'rd.grant.m'], permnf: ['danger', 'al.permnf.t', 'al.permnf.m'],
      csrf: ['danger', 'al.csrf.t', 'al.csrf.m'], saveerr: ['danger', 'al.error.t', 'al.error.m'], success: ['success', 'al.rdok.t', 'al.rdok.m'] };
    if (['grant', 'permnf', 'csrf', 'saveerr'].includes(st)) { $('p-role.manage').checked = true; sync(); upd(); } // percobaan simpan yang ditolak: perubahan tetap ada
    if (st === 'validation') { $('rd-name').value = ''; }
    if (TONE[st]) {
      const [tone, tk, mk] = TONE[st];
      box.className = `alert alert-${tone}`; box.setAttribute('role', tone === 'success' ? 'status' : 'alert'); box.hidden = false;
      $('ra-t').dataset.i18n = tk; $('ra-m').dataset.i18n = mk; box.tabIndex = -1;
    }
    if (st === 'csrf') $('rd-reload').hidden = false;
    ZApp.apply();
    if (st === 'validation') nameErr(true); else if (TONE[st]) box.focus();
  }

  // Form buat peran.
  const form = $('rn-form');
  if (!form) return;
  const box = $('rn-alert'), grp = $('rn-group-wrap');
  const TONE = { validation: ['danger', 'validation'], csrf: ['danger', 'csrf'], error: ['danger', 'error'], grant: ['danger', 'grant'], permnf: ['danger', 'permnf'], success: ['success', 'rnok'] };
  const ERR = { validation: [['name', 'rn.name.req'], ['slug', 'rn.slug.req']], slugtaken: [['slug', 'form.slug.err']], invscope: [['group', 'rn.group.bad']] };
  const showGroup = () => { grp.hidden = !$('rn-type-grp').checked; };
  form.addEventListener('change', e => { if (e.target.name === 'rn-type') showGroup(); });
  form.addEventListener('submit', e => e.preventDefault());
  if (st !== 'normal') ['role.read', 'role.manage'].forEach(c => { $(`p-${c}`).checked = true; });
  if (st === 'invscope') { $('rn-type-grp').checked = true; }
  showGroup();
  (ERR[st] || []).forEach(([f, key], i) => {
    const el = $(`rn-${f}`), msg = $(`rn-${f}-e`);
    msg.dataset.i18n = key; el.classList.add('is-invalid'); el.setAttribute('aria-invalid', 'true');
    el.setAttribute('aria-describedby', ($(`rn-${f}-h`) ? `rn-${f}-h ` : '') + `rn-${f}-e`);
    if (i === 0) el.focus();
  });
  if (TONE[st]) {
    const [tone, key] = TONE[st];
    box.className = `alert alert-${tone}`; box.setAttribute('role', tone === 'success' ? 'status' : 'alert'); box.hidden = false;
    $('ra-t').dataset.i18n = `al.${key}.t`; $('ra-m').dataset.i18n = `al.${key}.m`;
  }
  if (st === 'csrf' || st === 'permnf') $('rn-reload').hidden = false;
  if (st === 'success') { $('rn-link').hidden = false; box.tabIndex = -1; box.focus(); }
  if (st === 'loading') { $('rn-fs').disabled = true; form.setAttribute('aria-busy', 'true'); $('rn-spin').hidden = false; $('rn-label').dataset.i18n = 'btn.loading'; }
  sync(); ZApp.apply();
})();
