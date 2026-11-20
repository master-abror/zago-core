// Dimuat sinkron di <head> agar tema terpasang sebelum render (tanpa kedip). Logika penuh ada di app.js.
(function () {
  var d = document.documentElement;
  function g(k, f) { try { return localStorage.getItem(k) || f; } catch (e) { return f; } }
  var m = g('z-mode', 'auto');
  if (m === 'auto') m = matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  d.setAttribute('data-bs-theme', m);
  d.setAttribute('data-palette', g('z-palette', 'aurora'));
  d.setAttribute('data-nav', g('z-nav', 'side'));
  d.lang = g('z-lang', 'id-ID');
})();
