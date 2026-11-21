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
