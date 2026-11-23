// Perilaku demo Assignments per state (?state=): daftar (filter, cabut) dan form beri role. Di SPA Svelte menggantikan seluruhnya; lihat DESIGN-HANDOFF.md (F6).
(() => {
  const $ = id => document.getElementById(id), st = document.body.dataset.state, T = (k, p) => ZApp.t(k, p);
  const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
  const U1 = '0b7e1c52-4a3d-4f6e-9a10-2c5d8e7f3a41', G1 = '3c5e8a12-f7d4-4b90-a6c1-82e0d4b9175f';
  document.addEventListener('click', e => { if (e.target.closest('[data-action="reload"]')) location.reload(); });
  const markErr = (id, key, focus) => {
    const el = $(id), msg = $(`${id}-e`), h = $(`${id}-h`);
    msg.dataset.i18n = key; el.classList.add('is-invalid'); el.setAttribute('aria-invalid', 'true');
    el.setAttribute('aria-describedby', (h ? `${id}-h ` : '') + `${id}-e`);
    if (focus) el.focus();
  };
  const clearErr = id => { const el = $(id); el.classList.remove('is-invalid'); el.removeAttribute('aria-invalid'); el.setAttribute('aria-describedby', $(`${id}-h`) ? `${id}-h` : ''); if (!$(`${id}-h`)) el.removeAttribute('aria-describedby'); };

  // ---------------------------------------------------------------- Daftar assignment
  const table = $('as-table');
  if (table) {
    // Filter (demo): status -> state; role/subjek terisi -> state "nofilter". Di SPA: query ?status=&role_id=&subject_id= dan cursor dimulai ulang.
    const S2ST = { active: 'normal', revoked: 'revoked', expired: 'lapsed', all: 'all' }, ST2S = { revoked: 'revoked', lapsed: 'expired', all: 'all' };
    const status = $('as-status'), subj = $('as-subject');
    if (status) status.value = ST2S[st] || 'active';
    if (st === 'badfilter') { subj.value = 'bukan-uuid'; markErr('as-subject', 'as.f.subject.bad'); }
    if (st === 'nofilter') $('as-role').value = 'r-k';
    $('as-filter').addEventListener('submit', e => {
      e.preventDefault();
      const v = subj.value.trim();
      if (v && !UUID.test(v)) { markErr('as-subject', 'as.f.subject.bad', true); ZApp.apply(); return; }
      clearErr('as-subject');
      location.search = '?state=' + (($('as-role').value || v) ? 'nofilter' : S2ST[status.value]);
    });
    // Aksi ubah disembunyikan bila tanpa izin (pola F1d: tidak dirender; tidak ada alasan sebab).
    if (st === 'viewer') document.querySelectorAll('[data-manage]').forEach(el => { el.hidden = true; });
    const count = () => { const c = $('as-count'); if (c) c.textContent = T('as.count', { n: table.querySelectorAll('tbody tr:not([hidden])').length }); };
    count();

    // Cabut: dialog biasa. Hasil konfirmasi ditentukan state (demo); galat tampil DI DALAM dialog agar konteks tidak hilang.
    const dlg = $('dlg-revoke'), OUT = {
      revlast: ['as.rv.last.t', 'as.rv.last.m'], revgone: ['as.rv.gone.t', 'as.rv.gone.m', true], revgrant: ['al.grant.t', 'as.rv.grant.m'],
      revcsrf: ['al.csrf.t', 'al.csrf.m', true], reverr: ['al.error.t', 'al.error.m'] }[st];
    let row = null, who = null;
    const reset = () => { $('rv-alert').hidden = true; $('rv-reload').hidden = true; $('rv-spin').hidden = true; $('rv-ok').disabled = false; $('rv-ok-l').dataset.i18n = 'dlg.confirm'; };
    document.addEventListener('click', e => {
      const b = e.target.closest('[data-action="as-revoke"]');
      if (b) {
        row = b.closest('tr'); who = { role: b.dataset.role, subject: b.dataset.subject };
        reset(); $('rv-d').textContent = T('as.rv.d', who); $('rv-plat').hidden = !b.dataset.platform; ZApp.apply(); dlg.showModal(); return;
      }
      if (!e.target.closest('[data-action="rv-confirm"]')) return;
      $('rv-ok').disabled = true; $('rv-spin').hidden = false; $('rv-ok-l').dataset.i18n = 'btn.revoking'; ZApp.apply();
      setTimeout(() => {
        if (!OUT) { dlg.close('confirm'); return; }
        $('rv-spin').hidden = true; $('rv-ok').disabled = false; $('rv-ok-l').dataset.i18n = 'dlg.confirm';
        $('rv-at').dataset.i18n = OUT[0]; $('rv-am').dataset.i18n = OUT[1]; $('rv-reload').hidden = !OUT[2]; $('rv-alert').hidden = false; ZApp.apply();
        $('rv-alert').tabIndex = -1; $('rv-alert').focus();
      }, 700);
    });
    dlg.addEventListener('close', () => {
      const ok = dlg.returnValue === 'confirm'; dlg.returnValue = ''; reset();
      if (!ok) return;
      row.hidden = true; count(); ZUI.toast('success', 'as.revoked.ok', who);
      $('as-h1').focus(); // a baris hilang: fokus ke judul halaman (di SPA: judul atau baris berikutnya)
    });
  }

  // ---------------------------------------------------------------- Form beri role
  const form = $('an-form');
  if (form) {
    const plat = st === 'platform', box = $('an-alert');
    $('an-role').querySelectorAll(plat ? 'option[data-org]' : 'option[data-plat]').forEach(o => o.remove());
    ($(plat ? 'an-sc-plat' : 'an-sc-org')).checked = true;
    const showGroup = () => { $('an-group-wrap').hidden = !$('an-sc-grp').checked; };
    const showExp = () => { const n = $('an-noexp'); if (n) $('an-exp-wrap').hidden = n.checked; };
    form.addEventListener('change', e => { if (e.target.name === 'an-scope') showGroup(); if (e.target.id === 'an-noexp') showExp(); });
    const TONE = {
      validation: ['danger', 'al.validation.t', 'al.validation.m'], csrf: ['danger', 'al.csrf.t', 'al.csrf.m', 'r'], error: ['danger', 'al.error.t', 'al.error.m'],
      grant: ['danger', 'al.grant.t', 'an.grant.m'], exists: ['warning', 'an.exists.t', 'an.exists.m', 'l'], rolearch: ['danger', 'an.rolearch.t', 'an.rolearch.m'],
      rolenf: ['danger', 'an.rolenf.t', 'an.rolenf.m'], idem: ['warning', 'an.idem.t', 'an.idem.m', 'l'], idemconf: ['danger', 'an.idemconf.t', 'an.idemconf.m', 'r'],
      success: ['success', 'an.ok.t', 'an.ok.m', 'la'] };
    const alertShow = (tone, tk, mk, fl = '') => {
      box.className = `alert alert-${tone}`; box.setAttribute('role', tone === 'success' ? 'status' : 'alert'); box.hidden = false;
      $('an-t').dataset.i18n = tk; $('an-m').dataset.i18n = mk;
      $('an-reload').hidden = !fl.includes('r'); $('an-link').hidden = !fl.includes('l'); $('an-again').hidden = !fl.includes('a');
    };
    // Prefill untuk state hasil-server: isian yang valid tetap ada, galat menempel pada field yang disebut kontrak.
    if (!['normal', 'platform', 'validation', 'loading'].includes(st)) {
      $('an-role').value = plat ? 'r-sa' : 'r-k'; $('an-subject').value = U1;
      if (st === 'grpnf' || st === 'invscope') { $('an-sc-grp').checked = true; $('an-group').value = G1; showGroup(); }
      if (st === 'expinv') { $('an-noexp').checked = false; $('an-exp').value = '2025-01-01T09:00'; showExp(); }
    }
    const ERR = { validation: [['an-role', 'an.role.req'], ['an-subject', 'an.subject.req']], subjnf: [['an-subject', 'an.subject.nf']], grpnf: [['an-group', 'an.group.nf']],
      expinv: [['an-exp', 'an.exp.bad']], invscope: [['an-group', 'an.group.bad']], rolearch: [['an-role', 'an.role.arch']], rolenf: [['an-role', 'an.role.nf']] };
    (ERR[st] || []).forEach(([id, key], i) => markErr(id, key, i === 0));
    if (TONE[st]) alertShow(...TONE[st]);
    if (st === 'success') { box.tabIndex = -1; box.focus(); }
    if (st === 'loading') { $('an-fs').disabled = true; form.setAttribute('aria-busy', 'true'); $('an-spin').hidden = false; $('an-label').dataset.i18n = 'btn.loading'; }
    showGroup(); showExp();

    // Kirim (demo): validasi sisi klien, lalu spinner dan hasil sukses. Di SPA: POST /role-assignments dengan Idempotency-Key per percobaan (kunci sama saat coba ulang isian yang sama).
    form.addEventListener('submit', e => {
      e.preventDefault();
      ['an-role', 'an-subject', 'an-group', 'an-exp'].forEach(id => { if ($(id)) clearErr(id); });
      const bad = [];
      if (!$('an-role').value) bad.push(['an-role', 'an.role.req']);
      if (!UUID.test($('an-subject').value.trim())) bad.push(['an-subject', 'an.subject.req']);
      if ($('an-sc-grp').checked && !UUID.test($('an-group').value.trim())) bad.push(['an-group', 'an.group.req']);
      if ($('an-noexp') && !$('an-noexp').checked && !$('an-exp').value) bad.push(['an-exp', 'an.exp.req']);
      if (bad.length) { alertShow('danger', 'al.validation.t', 'al.validation.m'); bad.forEach(([id, k], i) => markErr(id, k, i === 0)); ZApp.apply(); return; }
      box.hidden = true; $('an-fs').disabled = true; $('an-spin').hidden = false; $('an-label').dataset.i18n = 'btn.loading'; ZApp.apply();
      setTimeout(() => { location.search = '?state=success'; }, 700);
    });
  }
  ZApp.apply();
})();
