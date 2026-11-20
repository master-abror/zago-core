// JS vanilla prototipe: tema, bahasa, i18n. Tanpa inline handler; semua lewat atribut data-*.
// Catatan SPA: di Svelte, preferensi ini ditulis ulang sebagai store; localStorage hanya untuk preferensi tampilan.
(() => {
  const d = document.documentElement, KEY = { mode: 'z-mode', palette: 'z-palette', lang: 'z-lang' };
  const get = (k, f) => { try { return localStorage.getItem(KEY[k]) || f; } catch { return f; } };
  const set = (k, v) => { try { localStorage.setItem(KEY[k], v); } catch { /* penyimpanan dinonaktifkan */ } };
  const dark = matchMedia('(prefers-color-scheme: dark)');
  const t = (key, p = {}) => {
    let s = ((window.ZI18N || {})[d.lang] || {})[key] ?? key;
    for (const [a, b] of Object.entries(p)) s = s.replaceAll(`{${a}}`, b);
    return s;
  };
  function apply() {
    const m = get('mode', 'auto');
    d.setAttribute('data-bs-theme', m === 'auto' ? (dark.matches ? 'dark' : 'light') : m);
    d.setAttribute('data-palette', get('palette', 'aurora'));
    d.lang = get('lang', 'id-ID');
    document.querySelectorAll('[data-i18n]').forEach(e => { e.textContent = t(e.dataset.i18n); });
    document.querySelectorAll('[data-i18n-attr]').forEach(e => e.dataset.i18nAttr.split(';').forEach(x => {
      const [a, k] = x.split(':'); e.setAttribute(a, t(k));
    }));
    document.querySelectorAll('[data-pref]').forEach(e => { e.value = get(e.dataset.pref, e.dataset.default); });
  }
  document.addEventListener('change', e => {
    const p = e.target.closest('[data-pref]');
    if (p) { set(p.dataset.pref, p.value); apply(); }
  });
  document.addEventListener('click', e => {
    if (e.target.closest('[data-action="toggle-mode"]')) { set('mode', d.getAttribute('data-bs-theme') === 'dark' ? 'light' : 'dark'); apply(); }
  });
  dark.addEventListener('change', apply);
  window.ZApp = { t, apply };
  apply();
})();
