// Perilaku demo F1d: dialog, toast, tooltip, popup minibar. Di SPA Svelte menulis ulang semuanya (lihat DESIGN-HANDOFF.md, F6).
(() => {
  const tr = (k, p) => ZApp.t(k, p);
  function toast(tone, msgKey, p) {
    const box = document.getElementById('z-toasts');
    if (!box) return;
    const urgent = tone === 'danger' || tone === 'warning', el = document.createElement('div');
    el.className = `toast show z-toast z-toast--${tone}`;
    el.setAttribute('role', urgent ? 'alert' : 'status');
    el.innerHTML = '<div class="d-flex"><div class="toast-body"><strong class="me-1"></strong><span></span></div><button type="button" class="btn-close me-2 m-auto" data-toast-close></button></div>';
    el.querySelector('strong').textContent = tr(`ts.l.${tone}`);
    el.querySelector('span').textContent = tr(msgKey, p);
    el.querySelector('.btn-close').setAttribute('aria-label', tr('ts.close'));
    box.append(el);
    if (urgent) return; // galat/peringatan menetap sampai ditutup (WCAG 2.2.1)
    let timer;
    const arm = () => { clearTimeout(timer); timer = setTimeout(() => el.remove(), 8000); };
    ['mouseleave', 'focusout'].forEach(ev => el.addEventListener(ev, arm));
    ['mouseenter', 'focusin'].forEach(ev => el.addEventListener(ev, () => clearTimeout(timer)));
    arm();
  }
  window.ZUI = { toast };
  document.addEventListener('click', e => {
    const o = e.target.closest('[data-dialog-open]');
    if (o) { document.getElementById(o.dataset.dialogOpen)?.showModal(); return; }
    if (e.target.matches('dialog.z-dlg')) { e.target.close('dismiss'); return; } // klik latar
    const k = e.target.closest('[data-toast]');
    if (k) { const [tone, key] = k.dataset.toast.split(':'); toast(tone, key); return; }
    e.target.closest('[data-toast-close]')?.closest('.toast').remove();
    if (e.target.closest('[aria-disabled="true"]')) e.preventDefault();
  });
  // WCAG 1.4.13: Esc menutup tooltip dan popup minibar tanpa memindahkan kursor atau fokus.
  document.addEventListener('keydown', e => {
    if (e.key !== 'Escape') return;
    document.querySelectorAll('.z-tip:hover, .z-tip:focus-within, .z-grp:hover, .z-grp:focus-within').forEach(el => el.classList.add('is-dismissed'));
  });
  ['mouseout', 'focusout'].forEach(ev => document.addEventListener(ev, e => {
    const g = e.target.closest?.('.z-tip, .z-grp');
    if (g && !g.contains(e.relatedTarget)) g.classList.remove('is-dismissed');
  }));
})();
