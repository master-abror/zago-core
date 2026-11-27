// Halaman sistem (F5a): 403 seragam, 404, 5xx (+ kode rujukan), 503 + Retry-After, mencoba lagi, offline (banner dan halaman penuh), error boundary.
// State dari ?state= (states.js). Demo saja: di SPA ini satu komponen halaman galat dengan varian; 401 TIDAK punya halaman (alur ke login.html?state=expired).
(() => {
  const $ = id => document.getElementById(id), st = document.body.dataset.state, q = new URLSearchParams(location.search);
  const T = (k, p) => window.ZApp.t(k, p);

  document.addEventListener('click', async e => {
    if (e.target.closest('[data-action="reload"]')) { location.reload(); return; }
    if (e.target.closest('[data-action="copy-ref"]')) {
      try { await navigator.clipboard.writeText($('sy-ref').textContent.trim()); window.ZUI.toast('success', 'sy.copied'); }
      catch (_) { window.ZUI.toast('danger', 'sy.copyfail'); }
    }
  });

  // Mencoba lagi: tombol sibuk (nonaktif + spinner), wilayah diumumkan sebagai status, bukan alert.
  if (st === 'retry') {
    const z = $('sy-500'); z.setAttribute('role', 'status'); z.setAttribute('aria-busy', 'true');
    $('sy-retry').disabled = true; $('sy-spin').hidden = false; $('sy-retry-l').dataset.i18n = 'sy.retrying';
  }

  // 503: hitung mundur dari Retry-After (?retry=detik, bawaan 30, minimum 1). Hitungan visual disembunyikan dari pembaca layar; akhirnya diumumkan SEKALI lewat #sy-live.
  if (st === 's503') {
    const v = parseInt(q.get('retry'), 10); let n = Number.isFinite(v) ? Math.max(1, v) : 30, h;
    const c = $('sy-count'), b = $('sy-retry-503');
    const tick = () => {
      if (n <= 0) {
        clearInterval(h); b.disabled = false; c.removeAttribute('aria-hidden'); c.dataset.i18n = 'sy.ready';
        $('sy-live').textContent = T('sy.ready'); window.ZApp.apply(); return;
      }
      c.textContent = T('al.rate.c', { s: n }); n -= 1;
    };
    tick(); h = setInterval(tick, 1000);
  }

  // Judul dokumen mengikuti h1 state aktif; fokus ke h1 (di SPA: saat rute galat dirender).
  const h1 = [...document.querySelectorAll('h1')].find(h => !h.closest('[hidden]'));
  if (h1) { document.querySelector('title').dataset.i18n = h1.dataset.i18n; h1.tabIndex = -1; h1.focus(); }
  window.ZApp.apply();
})();
