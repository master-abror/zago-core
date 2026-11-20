// Perilaku demo halaman Security per state (?state=). Di SPA Svelte menggantikan seluruhnya; lihat DESIGN-HANDOFF.md (F6).
(() => {
  const $ = id => document.getElementById(id), st = document.body.dataset.state, T = (k, p) => ZApp.t(k, p);
  document.addEventListener('click', e => { if (e.target.closest('[data-action="reload"]')) location.reload(); });
  document.querySelectorAll('dialog.z-dlg[data-done]').forEach(d => d.addEventListener('close', () => {
    if (d.returnValue === 'confirm') ZUI.toast('success', d.dataset.done, { n: d.dataset.n });
    d.returnValue = ''; // fokus kembali ke pemicu secara native; di SPA pindahkan fokus ke judul bila baris hilang
  }));
  const form = $('pw-form');
  if (!form) return;
  const alertBox = $('pw-alert'), submit = $('pw-submit');
  const TONE = { validation: ['danger', 'validation'], csrf: ['danger', 'csrf'], error: ['danger', 'error'], rate: ['warning', 'rate'], success: ['success', 'pwok'] };
  const ERR = { validation: [['cur', 'pw.cur.req'], ['new', 'pw.new.req']], short: [['new', 'pw.e.short']], long: [['new', 'pw.e.long']],
    same: [['new', 'pw.e.same']], wrongpw: [['cur', 'pw.e.wrong']] };
  $('pw-show').addEventListener('change', e => { ['cur', 'new'].forEach(f => { $(`pw-${f}`).type = e.target.checked ? 'text' : 'password'; }); });
  form.addEventListener('submit', e => e.preventDefault());
  function show(tone, key) {
    alertBox.className = `alert alert-${tone}`; alertBox.setAttribute('role', tone === 'success' || tone === 'info' ? 'status' : 'alert'); alertBox.hidden = false;
    $('pa-t').dataset.i18n = `al.${key}.t`; $('pa-m').dataset.i18n = `al.${key}.m`;
  }
  (ERR[st] || []).forEach(([f, key], i) => {
    const el = $(`pw-${f}`), msg = $(`pw-${f}-e`);
    msg.dataset.i18n = key; el.classList.add('is-invalid'); el.setAttribute('aria-invalid', 'true');
    el.setAttribute('aria-describedby', (f === 'new' ? 'pw-new-h ' : '') + `pw-${f}-e`);
    if (i === 0) el.focus();
  });
  if (TONE[st]) show(...TONE[st]);
  if (st === 'validation') { /* ringkasan di atas + pesan per field; fokus ke field pertama yang galat */ }
  if (st === 'csrf') $('pw-reload').hidden = false;
  if (st === 'success') { alertBox.tabIndex = -1; alertBox.focus(); }
  if (st === 'loading') { $('pw-fs').disabled = true; form.setAttribute('aria-busy', 'true'); $('pw-spin').hidden = false; $('pw-label').dataset.i18n = 'btn.loading'; }
  if (st === 'rate') {
    let n = Math.max(1, Number(new URLSearchParams(location.search).get('retry')) || 30), h; // dari header Retry-After (detik); cadangan bila header tidak ada
    const c = $('pa-c'), tick = () => {
      if (n <= 0) { clearInterval(h); submit.disabled = false; c.hidden = true; show('info', 'rate.done'); ZApp.apply(); return; }
      c.hidden = false; c.textContent = T('al.rate.c', { s: n }); n -= 1;
    };
    submit.disabled = true; tick(); if (n > 0) h = setInterval(tick, 1000);
  }
  ZApp.apply();
})();
