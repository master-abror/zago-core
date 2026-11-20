// Perilaku demo halaman Login per state (?state=). Di SPA, Svelte menggantikan seluruhnya; lihat DESIGN-HANDOFF.md (F6).
(() => {
  const $ = id => document.getElementById(id), st = document.body.dataset.state;
  const form = $('login-form'), fs = $('lg-fs'), box = $('login-alert'), btn = $('login-submit'), email = $('lg-email'), pw = $('lg-password');
  const TONE = { validation: ['danger', 'validation'], invalid: ['danger', 'invalid'], suspended: ['danger', 'suspended'], locked: ['danger', 'locked'],
    rate: ['warning', 'rate'], expired: ['info', 'expired'], error: ['danger', 'error'], success: ['success', 'ok'] };
  function show(tone, key) {
    box.className = `alert alert-${tone}`;
    box.setAttribute('role', tone === 'danger' || tone === 'warning' ? 'alert' : 'status');
    box.hidden = false;
    $('la-t').dataset.i18n = `al.${key}.t`; $('la-m').dataset.i18n = `al.${key}.m`;
    ZApp.apply();
  }
  function invalid(el, errId) {
    el.classList.add('is-invalid'); el.setAttribute('aria-invalid', 'true'); el.setAttribute('aria-describedby', errId);
  }
  function busy(on, labelKey) {
    fs.disabled = on; form.setAttribute('aria-busy', String(on)); $('lg-spin').hidden = !on;
    $('lg-label').dataset.i18n = labelKey; ZApp.apply();
  }
  document.addEventListener('click', e => {
    if (!e.target.closest('[data-action="toggle-pw"]')) return;
    const hide = pw.type === 'password';
    pw.type = hide ? 'text' : 'password';
    $('lg-toggle').dataset.i18n = hide ? 'lg.hide' : 'lg.show'; $('lg-toggle').setAttribute('aria-pressed', String(hide)); ZApp.apply();
  });
  form.addEventListener('submit', e => e.preventDefault()); // demo: tidak ada request
  if (TONE[st]) show(...TONE[st]);
  if (st === 'validation') { invalid(email, 'lg-email-e'); invalid(pw, 'lg-password-e'); email.focus(); }
  if (st === 'invalid' || st === 'suspended' || st === 'locked') pw.focus();
  if (st === 'expired' || st === 'error') email.focus();
  if (st === 'loading') busy(true, 'lg.submitting');
  if (st === 'success') busy(true, 'lg.submitting');
  if (st === 'rate') {
    let n = Math.max(1, Number(new URLSearchParams(location.search).get('retry')) || 30); // dari header Retry-After (detik)
    let h; const c = $('la-c'), tick = () => {
      if (n <= 0) { clearInterval(h); btn.disabled = false; c.hidden = true; show('info', 'rate.done'); return; }
      c.hidden = false; c.textContent = ZApp.t('al.rate.c', { s: n }); n -= 1;
    };
    btn.disabled = true; email.focus(); tick(); if (n > 0) h = setInterval(tick, 1000);
  }
})();
