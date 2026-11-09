// The script of every page of the shell (README.md, "The scripts"). Every form works without it. It gives the focus back
// when a menu dialog closes: its opening button is in a hidden popover, so the focus goes to the ⋯ button, or, for a
// form that the page's link asked for, to its head button (close does not bubble: one listener, capture phase).
(() => {
  document.addEventListener('close', (e) => {
    const d = e.target, at = document.activeElement;
    if (!d.matches || !d.matches('dialog.menu-dialog') || (at !== document.body && !d.contains(at))) return;
    (d.closest('.menu')?.querySelector('.menu-button') || document.querySelector(`[commandfor="${CSS.escape(d.id)}"][command="close"]`))?.focus();
  }, true);
})();
