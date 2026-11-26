// Perilaku demo Simulator per state (?state=). Di SPA Svelte menggantikan seluruhnya; lihat DESIGN-HANDOFF.md (F6).
// Di SPA: POST /permissions/simulate dengan X-CSRF-Token, tanpa Idempotency-Key. Body: {user_id, action, active_group_id?, resource?{type,id,owner_id?,group_id?,attrs?}};
// field kosong DIHILANGKAN (bukan string kosong); attrs: str -> string, num -> angka, yes/no -> bool, list -> array skalar. Respons {result, reason, level, policy_code, deferred_policies}.
(() => {
  const $ = id => document.getElementById(id), st = document.body.dataset.state, form = $('sm-form');
  if (!form) return;
  const U = '3f2b8c1e-5a47-4d0e-9b1a-7c6d2e8f4a10', R = '9d0c4e7a-1b2f-4c3d-8e5a-6f7a8b9c0d1e', UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
  const RES = { allow: ['ALLOW', 'granted', 'organization'], noperm: ['DENY', 'no_permission'], scopemis: ['DENY', 'scope_mismatch'], policy: ['DENY', 'policy_denied', null, 'user.protect_locked'],
    polfail: ['DENY', 'policy_evaluation_failed', null, 'user.update.limit_admin'], nopolread: ['DENY', 'policy_denied'], deferred: ['ALLOW', 'granted', 'group', null, ['user.protect_locked', 'invoice.amount_limit']] };
  const ERR = { platform: ['warning', 'sm.plat.t', 'sm.plat.m', 'c'], grpscope: ['danger', 'sm.grp.t', 'sm.grp.m'], csrf: ['danger', 'al.csrf.t', 'al.csrf.m', 'r'], error: ['danger', 'al.error.t', 'al.error.m'],
    usernf: ['danger', 'al.validation.t', 'al.validation.m'], attrres: ['danger', 'al.validation.t', 'al.validation.m'], attrbad: ['danger', 'al.validation.t', 'al.validation.m'], validation: ['danger', 'al.validation.t', 'al.validation.m'] };
  const FE = { usernf: [['sm-user', 'sm.user.nf']], attrres: [['sm-attrs', 'sm.attr.reserved']], attrbad: [['sm-attrs', 'sm.attr.badval']] };
  const PRE = { allow: ['role.read'], noperm: ['role.manage'], scopemis: ['user.update'], policy: ['user.update', 1], polfail: ['user.update', 1], nopolread: ['user.update', 1], deferred: ['user.update'], usernf: ['role.read'], attrres: ['user.update', 2], attrbad: ['user.update', 3], grpscope: ['user.read'], csrf: ['role.read'], error: ['role.read'], platform: ['role.read'] };
  const FIELDS = ['sm-user', 'sm-action', 'sm-group', 'sm-rtype', 'sm-rid', 'sm-rowner', 'sm-rgroup', 'sm-attrs'], BASE = {};
  FIELDS.forEach(id => { BASE[id] = $(id).getAttribute('aria-describedby') || ''; });
  const rows = $('sm-rows'), tpl = $('sm-row-t'), box = $('sm-alert'), out = $('sm-out');
  const setDesc = (el, v) => { v.trim() ? el.setAttribute('aria-describedby', v.trim()) : el.removeAttribute('aria-describedby'); };
  const mark = (id, key, focus) => {
    const el = $(id), msg = $(`${id}-e`); msg.dataset.i18n = key; msg.hidden = false; setDesc(el, `${BASE[id]} ${id}-e`);
    if (id === 'sm-attrs') { if (focus) (rows.querySelector('[data-attr-name]') || $('sm-attr-add')).focus(); return; }
    el.classList.add('is-invalid'); el.setAttribute('aria-invalid', 'true'); if (focus) el.focus();
  };
  const clear = id => { const el = $(id), msg = $(`${id}-e`); msg.hidden = true; delete msg.dataset.i18n; msg.textContent = ''; setDesc(el, BASE[id]); el.classList.remove('is-invalid'); el.removeAttribute('aria-invalid'); };
  const alertShow = (tone, tk, mk, fl = '') => {
    box.className = `alert alert-${tone}`; box.setAttribute('role', 'alert'); box.hidden = false; $('sm-at').dataset.i18n = tk; $('sm-am').dataset.i18n = mk;
    $('sm-reload').hidden = !fl.includes('r'); $('sm-cta').hidden = !fl.includes('c');
  };
  const addRow = (name = '', type = 'str', val = '') => {
    const r = tpl.content.firstElementChild.cloneNode(true);
    r.querySelector('[data-attr-name]').value = name; r.querySelector('[data-attr-type]').value = type; r.querySelector('[data-attr-val]').value = val;
    r.querySelector('[data-attr-val]').disabled = type === 'yes' || type === 'no'; rows.append(r); ZApp.apply(); $('sm-attr-add').disabled = rows.children.length >= 32;
    return r;
  };
  $('sm-attr-add').addEventListener('click', () => addRow().querySelector('[data-attr-name]').focus());
  rows.addEventListener('click', e => { if (!e.target.closest('[data-attr-rm]')) return; e.target.closest('[data-attr-row]').remove(); $('sm-attr-add').disabled = false; $('sm-attr-add').focus(); });
  rows.addEventListener('change', e => { if (e.target.matches('[data-attr-type]')) e.target.closest('[data-attr-row]').querySelector('[data-attr-val]').disabled = ['yes', 'no'].includes(e.target.value); });
  document.addEventListener('click', e => {
    if (e.target.closest('[data-action="reload"]')) location.reload();
    if (e.target.closest('[data-action="open-org"]')) document.querySelector('.z-top .dropdown-toggle')?.click();
  });

  // Isian contoh per state (demo).
  const p = PRE[st];
  if (p) {
    $('sm-user').value = st === 'usernf' ? R : U; $('sm-action').value = p[0];
    if (st === 'grpscope') $('sm-group').value = R;
    if (p[1]) { $('sm-rtype').value = 'user'; $('sm-rid').value = R; $('sm-rowner').value = U; $('sm-res').open = true; }
    if (p[1] === 1) addRow('status', 'str', 'locked');
    if (p[1] === 2) addRow('owner_id', 'str', 'abc');
    if (p[1] === 3) addRow('level', 'num', '12x');
  }

  // Validasi sisi klien; urutan galat = urutan visual. Server melaporkan SEMUA field salah sekaligus.
  const validate = () => {
    const bad = [], v = id => $(id).value.trim(), uuid = (id, opt) => { if (v(id) ? !UUID.test(v(id)) : !opt) bad.push([id, v(id) ? 'sm.uuid.bad' : 'sm.user.req']); };
    uuid('sm-user'); if (!v('sm-action')) bad.push(['sm-action', 'sm.action.req']); uuid('sm-group', 1);
    const rs = rows.querySelectorAll('[data-attr-row]'), any = ['sm-rtype', 'sm-rid', 'sm-rowner', 'sm-rgroup'].some(v) || rs.length;
    if (any) {
      if (!v('sm-rtype')) bad.push(['sm-rtype', 'sm.rtype.req']); else if (!/^[a-z][a-z0-9_]{0,99}$/.test(v('sm-rtype'))) bad.push(['sm-rtype', 'sm.rtype.bad']);
      if (!v('sm-rid')) bad.push(['sm-rid', 'sm.rid.req']); else if (!UUID.test(v('sm-rid'))) bad.push(['sm-rid', 'sm.uuid.bad']);
      uuid('sm-rowner', 1); uuid('sm-rgroup', 1);
      const names = [...rs].map(r => r.querySelector('[data-attr-name]').value.trim()), RSV = ['type', 'id', 'organization_id', 'owner_id', 'group_id'];
      let ak = '';
      if (rs.length > 32) ak = 'sm.attr.max';
      else if (names.some(n => !/^[A-Za-z_][A-Za-z0-9_]{0,63}$/.test(n))) ak = 'sm.attr.badname';
      else if (names.some(n => RSV.includes(n))) ak = 'sm.attr.reserved';
      else if (new Set(names).size < names.length) ak = 'sm.attr.dup';
      else if ([...rs].some(r => r.querySelector('[data-attr-type]').value === 'num' && !Number.isFinite(Number(r.querySelector('[data-attr-val]').value)) || r.querySelector('[data-attr-type]').value === 'num' && !r.querySelector('[data-attr-val]').value.trim())) ak = 'sm.attr.badval';
      if (ak) bad.push(['sm-attrs', ak]);
    }
    return bad;
  };
  const showErrors = list => { list.sort((a, b) => FIELDS.indexOf(a[0]) - FIELDS.indexOf(b[0])); alertShow(...ERR.validation); list.forEach(([id, k], i) => mark(id, k, i === 0)); if (list.some(x => x[0] === 'sm-rtype' || x[0] === 'sm-rid' || x[0] === 'sm-attrs')) $('sm-res').open = true; ZApp.apply(); };

  // Hasil: dibangun dari kunci i18n (tanpa data pengguna); kode policy diisi lewat textContent.
  const render = ([result, reason, level, policy, deferred]) => {
    const ok = result === 'ALLOW', nopol = st === 'nopolread';
    out.innerHTML = `<div class="card ${ok ? 'border-success' : 'border-danger'}"><div class="card-body"><div class="d-flex align-items-center gap-2 mb-3"><span class="badge fs-6 ${ok ? 'bg-success-subtle text-success-emphasis border border-success-subtle' : 'bg-danger-subtle text-danger-emphasis border border-danger-subtle'}" data-i18n="${ok ? 'sm.allow' : 'sm.deny'}"></span><code>${result}</code></div>`
      + `<dl class="row mb-0"><dt class="col-sm-4" data-i18n="sm.reason"></dt><dd class="col-sm-8"><span data-i18n="sm.r.${reason}"></span> <code class="text-break">${reason}</code></dd>`
      + (level ? `<dt class="col-sm-4" data-i18n="sm.level"></dt><dd class="col-sm-8"><span data-i18n="sm.lv.${level}"></span> <code>${level}</code></dd>` : '')
      + (policy ? '<dt class="col-sm-4" data-i18n="sm.policy"></dt><dd class="col-sm-8"><code class="text-break" data-pol></code></dd>' : '')
      + (deferred ? '<dt class="col-sm-4" data-i18n="sm.deferred"></dt><dd class="col-sm-8"><ul class="mb-1 ps-3" data-def></ul><p class="small text-body-secondary mb-0" data-i18n="sm.deferred.m"></p></dd>' : '')
      + `</dl>${nopol ? '<p class="small text-body-secondary mt-3 mb-0" data-i18n="sm.nopolread"></p>' : ''}</div></div>`;
    if (policy) out.querySelector('[data-pol]').textContent = policy;
    (deferred || []).forEach(c => { const li = document.createElement('li'), k = document.createElement('code'); k.textContent = c; li.append(k); out.querySelector('[data-def]').append(li); });
    ZApp.apply();
  };
  const emptyOut = () => { out.innerHTML = '<p class="text-body-secondary mb-0" data-i18n="sm.out.empty"></p>'; ZApp.apply(); };
  const busy = on => { $('sm-fs').disabled = on; $('sm-run').disabled = on; $('sm-spin').hidden = !on; $('sm-label').dataset.i18n = on ? 'sm.busy' : 'sm.run'; out.setAttribute('aria-busy', String(on)); form.toggleAttribute('aria-busy', on); ZApp.apply(); };
  const done = state => {
    busy(false); box.hidden = true; FIELDS.forEach(clear); emptyOut();
    if (RES[state]) { render(RES[state]); out.tabIndex = -1; out.focus(); return; }
    if (ERR[state]) { alertShow(...ERR[state]); (FE[state] || []).forEach(([id, k], i) => mark(id, k, i === 0)); if (state === 'attrres' || state === 'attrbad') $('sm-res').open = true; ZApp.apply(); if (!FE[state]) { box.tabIndex = -1; box.focus(); } }
  };
  form.addEventListener('submit', e => {
    e.preventDefault(); FIELDS.forEach(clear); box.hidden = true;
    const bad = validate(); if (bad.length) { showErrors(bad); return; }
    busy(true); out.innerHTML = '<div class="placeholder-glow" aria-hidden="true"><span class="placeholder col-4 mb-3 d-block"></span><span class="placeholder col-10 d-block mb-2"></span><span class="placeholder col-7 d-block"></span></div>';
    setTimeout(() => done(RES[st] || ERR[st] && st !== 'validation' ? st : 'allow'), 700);
  });

  // Keadaan awal menurut state: hasil/galat tampil seolah kiriman sudah dijalankan.
  emptyOut();
  if (st === 'loading') { $('sm-user').value = U; $('sm-action').value = 'role.read'; busy(true); out.innerHTML = '<div class="placeholder-glow" aria-hidden="true"><span class="placeholder col-4 mb-3 d-block"></span><span class="placeholder col-10 d-block mb-2"></span><span class="placeholder col-7 d-block"></span></div>'; }
  else if (st === 'validation') showErrors(validate());
  else if (RES[st] || ERR[st]) { done(st); if (st === 'platform') { $('sm-fs').disabled = true; $('sm-run').disabled = true; } }
  ZApp.apply();
})();
