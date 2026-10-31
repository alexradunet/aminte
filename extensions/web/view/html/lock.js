// The script of the lock (docs/plans/160-the-lock.md). The layout adds it only when the lock is on; data-minutes is the
// setting lock_after. The server alone cannot clear the screen, so this script does it. It keeps the time of the last
// input of the owner in localStorage, one number shared by every tab. It tells the server about input (POST /awake, at
// most once in 30 seconds), and when the idle time is over it locks the session (POST /lock) and replaces the page with
// the lock page. Every tab does the same, because every tab reads the shared time. A load of the page counts as input:
// a navigation is activity on the server too. A prerendered page waits until it is shown. A text that is not saved is
// lost when the page is replaced: the script keeps no copy of it.
(() => {
  const limit = Number(document.currentScript.dataset.minutes) * 60000, KEY = 'lifelog-last-input';
  if (!(limit > 0)) return;
  let mem = 0, sent = 0, gone = false; // mem is the time of the last input of this tab: the fallback when there is no storage

  const last = () => { try { return Number(localStorage.getItem(KEY)) || mem; } catch (e) { return mem; } };
  const touch = () => { mem = Date.now(); try { localStorage.setItem(KEY, String(mem)); } catch (e) { /* no storage */ } };
  const show = () => {
    if (gone) return;
    gone = true;
    location.replace('/locked?next=' + encodeURIComponent(location.pathname));
  };

  // check locks when the idle time is over. The server must lock first: the lock page sends an open session back.
  const check = async () => {
    if (gone || Date.now() - last() < limit) return;
    try { await fetch('/lock', { method: 'POST', credentials: 'same-origin', signal: AbortSignal.timeout(5000) }); } catch (e) { /* show anyway */ }
    show();
  };

  const input = async () => {
    const now = Date.now();
    if (now - mem < 1000) return; // a scroll fires often
    touch();
    if (now - sent < 30000) return;
    sent = now;
    try {
      const r = await fetch('/awake', { method: 'POST', credentials: 'same-origin' });
      if (r.status === 401) show();
    } catch (e) { /* the next input tries again */ }
  };

  const start = () => {
    touch();
    for (const type of ['keydown', 'pointerdown', 'input', 'wheel', 'scroll', 'touchstart']) {
      addEventListener(type, input, { passive: true, capture: true }); // scroll does not bubble
    }
    // The Lock button ends the shared time at once, so that every other tab locks at its next check too.
    document.addEventListener('submit', (e) => {
      if (e.target.matches('form[action="/lock"]')) try { localStorage.setItem(KEY, '1'); } catch (err) { /* no storage */ }
    });
    // A page that comes back (a tab, a sleep, the back-forward cache) only checks the time: it is no input.
    document.addEventListener('visibilitychange', () => { if (!document.hidden) check(); });
    addEventListener('focus', check);
    addEventListener('pageshow', check);
    setInterval(check, 15000);
  };

  if (document.prerendering) document.addEventListener('prerenderingchange', start, { once: true });
  else start();
})();
