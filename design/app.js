// JS vanilla prototipe: tema, bahasa, i18n, mode menu. Tanpa inline handler; semua lewat atribut data-*.
// Catatan SPA: di Svelte, preferensi ini menjadi store; localStorage hanya untuk preferensi tampilan.
(() => {
  const d = document.documentElement, KEY = { mode: 'z-mode', palette: 'z-palette', lang: 'z-lang', nav: 'z-nav' };
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
    d.setAttribute('data-nav', get('nav', 'side'));
    d.lang = get('lang', 'id-ID');
    document.querySelectorAll('[data-i18n]').forEach(e => { e.textContent = t(e.dataset.i18n); });
    document.querySelectorAll('[data-i18n-attr]').forEach(e => e.dataset.i18nAttr.split(';').forEach(x => {
      const [a, k] = x.split(':'); e.setAttribute(a, t(k));
    }));
    document.querySelectorAll('[data-pref]').forEach(e => { e.value = get(e.dataset.pref, e.dataset.default); });
    document.querySelectorAll('[data-set]').forEach(e => {
      const [k, v] = e.dataset.set.split(':'), on = get(k, k === 'palette' ? 'aurora' : 'id-ID') === v;
      e.classList.toggle('active', on); if (on) e.setAttribute('aria-current', 'true'); else e.removeAttribute('aria-current');
    });
  }
  function drawer(open) {
    const nav = document.getElementById('z-nav'), scrim = document.querySelector('.z-scrim'), btn = document.querySelector('.z-drawer-btn');
    if (!nav) return;
    const on = open ?? !nav.classList.contains('is-open');
    nav.classList.toggle('is-open', on); scrim?.classList.toggle('is-open', on); btn?.setAttribute('aria-expanded', String(on));
  }
  document.addEventListener('change', e => {
    const p = e.target.closest('[data-pref]');
    if (p) { set(p.dataset.pref, p.value); apply(); }
  });
  document.addEventListener('click', e => {
    const s = e.target.closest('[data-set]');
    if (s) { const [k, v] = s.dataset.set.split(':'); set(k, v); apply(); return; }
    const a = e.target.closest('[data-action]')?.dataset.action;
    if (a === 'toggle-mode') { set('mode', d.getAttribute('data-bs-theme') === 'dark' ? 'light' : 'dark'); apply(); }
    if (a === 'toggle-nav') { set('nav', get('nav', 'side') === 'side' ? 'min' : 'side'); apply(); }
    if (a === 'toggle-drawer') drawer();
  });
  document.addEventListener('keydown', e => { if (e.key === 'Escape') drawer(false); });
  dark.addEventListener('change', apply);
  window.ZApp = { t, apply };
  apply();
})();
