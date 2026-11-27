// Perilaku demo halaman pemulihan akun per state (?state=): lupa password (F5b-1), atur ulang password (F5b-2), terima undangan (F5b-3).
// Contract-first: tidak ada request. Di SPA Svelte menggantikan seluruhnya; lihat DESIGN-HANDOFF.md (F6).
(() => {
  const $ = id => document.getElementById(id), st = document.body.dataset.state, T = (k, p) => ZApp.t(k, p);
  const P = { forgot: 'fp', reset: 'rp', accept: 'ai' }[document.body.dataset.rec];
  if (!P) return;
  const form = $(`${P}-form`), fs = $(`${P}-fs`), box = $(`${P}-alert`), btn = $(`${P}-submit`);
  const TONE = { validation: ['danger', 'validation'], error: ['danger', 'error'], rate: ['warning', 'rate'] };
  function show(tone, key) {
    box.className = `alert alert-${tone}`;
    box.setAttribute('role', tone === 'danger' || tone === 'warning' ? 'alert' : 'status');
    box.hidden = false;
    $(`${P}-at`).dataset.i18n = `al.${key}.t`; $(`${P}-am`).dataset.i18n = `al.${key}.m`;
    ZApp.apply();
  }
  function invalid(el, errId) {
    el.classList.add('is-invalid'); el.setAttribute('aria-invalid', 'true'); el.setAttribute('aria-describedby', errId);
  }
  function busy(on, labelKey) {
    fs.disabled = on; form.setAttribute('aria-busy', String(on)); $(`${P}-spin`).hidden = !on;
    $(`${P}-label`).dataset.i18n = labelKey; ZApp.apply();
  }
  // Hitung mundur dari Retry-After (detik); cadangan 30 detik bila header tidak ada. Angka tidak dibacakan tiap detik (aria-hidden); selesai = pesan status.
  function countdown(doneKey, focusEl) {
    let n = Math.max(1, Number(new URLSearchParams(location.search).get('retry')) || 30), h;
    const c = $(`${P}-ac`), tick = () => {
      if (n <= 0) { clearInterval(h); btn.disabled = false; c.hidden = true; show('info', doneKey); return; }
      c.hidden = false; c.textContent = T('al.rate.c', { s: n }); n -= 1;
    };
    btn.disabled = true; if (focusEl) focusEl.focus(); tick(); if (n > 0) h = setInterval(tick, 1000);
  }
  // Tampilkan password: kotak centang dengan label tetap (bukan tombol yang labelnya berganti); di SPA ubah type input, jangan simpan nilainya.
  const sh = $(`${P}-show`), pwEl = $(`${P}-pw`);
  if (sh && pwEl) sh.addEventListener('change', () => { pwEl.type = sh.checked ? 'text' : 'password'; });
  form.addEventListener('submit', e => e.preventDefault()); // demo: tidak ada request
  if (TONE[st]) show(...TONE[st]);

  if (P === 'fp') {
    const email = $('fp-email');
    if (st === 'validation') { email.value = 'nama@contoh'; invalid(email, 'fp-email-e'); email.focus(); }
    if (st === 'loading' || st === 'rate' || st === 'error') email.value = 'nama@contoh.id'; // isian dipertahankan saat galat
    if (st === 'loading') busy(true, 'lg.submitting');
    if (st === 'error') email.focus();
    if (st === 'rate') countdown('fpdone', email);
    if (st === 'sent') $('fp-sent-h').focus(); // form diganti panel jawaban generik; fokus ke judul baru
  }

  if (P === 'rp') {
    const pw = $('rp-pw');
    const bad = (key, val) => { // galat per field dari server (validation_failed) atau klien: pesan di bawah field, fokus ke field
      $('rp-pw-e').dataset.i18n = key; pw.value = val; pw.classList.add('is-invalid'); pw.setAttribute('aria-invalid', 'true');
      pw.setAttribute('aria-describedby', 'rp-pw-h rp-pw-e'); pw.focus();
    };
    if (st === 'validation') bad('pw.new.req', '');
    if (st === 'short') bad('pw.e.short', 'pendek123');
    if (st === 'long') bad('pw.e.long', 'x'.repeat(300));
    if (st === 'loading' || st === 'rate' || st === 'error') pw.value = 'frasa-sandi-yang-panjang'; // isian dipertahankan saat galat
    if (st === 'loading') busy(true, 'btn.loading');
    if (st === 'error') pw.focus();
    if (st === 'rate') countdown('rpdone', pw);
    if (st === 'token') $('rp-token-h').focus(); // invalid_reset_token: satu pesan untuk tak sah/terpakai/kedaluwarsa; form diganti panel
    if (st === 'success') $('rp-ok-h').focus();  // semua sesi dicabut; tidak ada masuk otomatis
  }

  if (P === 'ai') {
    const nm = $('ai-name'), pw = $('ai-pw'), NAME = 'Sari Wulandari'; // nama contoh fiktif
    const bad = (el, errId, key, val, desc) => { // galat per field (validation_failed): pesan di bawah field; fokus ke field pertama yang galat
      $(errId).dataset.i18n = key; el.value = val; el.classList.add('is-invalid'); el.setAttribute('aria-invalid', 'true'); el.setAttribute('aria-describedby', desc);
    };
    if (st === 'validation') { bad(nm, 'ai-name-e', 'ai.name.req', '', 'ai-name-e'); bad(pw, 'ai-pw-e', 'pw.new.req', '', 'ai-pw-h ai-pw-e'); nm.focus(); }
    if (st === 'short') { nm.value = NAME; bad(pw, 'ai-pw-e', 'pw.e.short', 'pendek123', 'ai-pw-h ai-pw-e'); pw.focus(); }
    if (st === 'long') { nm.value = NAME; bad(pw, 'ai-pw-e', 'pw.e.long', 'x'.repeat(300), 'ai-pw-h ai-pw-e'); pw.focus(); }
    if (st === 'loading' || st === 'rate' || st === 'error') { nm.value = NAME; pw.value = 'frasa-sandi-yang-panjang'; } // isian dipertahankan saat galat
    if (st === 'loading') busy(true, 'btn.loading');
    if (st === 'error') pw.focus();
    if (st === 'rate') countdown('aidone', pw);
    if (st === 'token') $('ai-token-h').focus(); // invalid_invitation: satu pesan; tanpa CTA minta tautan (undangan diterbitkan administrator)
    if (st === 'success') $('ai-ok-h').focus();  // tidak ada masuk otomatis; respons sukses belum terdokumentasi
  }
  ZApp.apply();
})();
