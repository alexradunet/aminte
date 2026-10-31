// The one script of the app (README, "No script"). Every form works without it. On every page it gives the focus back
// when a menu dialog closes: its opening button is in a hidden popover, so the focus goes to the ⋯ button, or, for a
// form that the page's link asked for, to its head button (close does not bubble: one listener, capture phase). On the
// chat column (#chat) and the chat screen (#conversation) it polls while the agent has not answered, posts the
// composer in place, and lets Enter send (Shift+Enter breaks a line). The thread is a reversed column, so after a swap
// it is still at its newest turn. A 401 locked answer of the poll (docs/plans/160-the-lock.md) shows the lock page. The column remembers whether its popover is open. Any error leaves the page as it is.
// A prerendered page runs it too; it guards showPopover.
(() => {
  document.addEventListener('close', (e) => {
    const d = e.target, at = document.activeElement;
    if (!d.matches || !d.matches('dialog.menu-dialog') || (at !== document.body && !d.contains(at))) return;
    (d.closest('.menu')?.querySelector('.menu-button') || document.querySelector(`[commandfor="${CSS.escape(d.id)}"][command="close"]`))?.focus();
  }, true);
  const chat = document.getElementById('chat') || document.getElementById('conversation');
  if (!chat) return;
  const same = { credentials: 'same-origin' }, KEY = 'lifelog.chat.open'; let timer;

  // swap replaces the column's content with the one in a page's HTML, keeps a draft that is being typed, and gives the
  // new box the focus that the old one had.
  const swap = (html) => {
    const next = new DOMParser().parseFromString(html, 'text/html').getElementById(chat.id);
    if (!next) return;
    const draft = chat.querySelector('.chat-form textarea');
    const typed = draft && !draft.form.dataset.sent ? draft.value : '';
    const focused = draft && document.activeElement === draft;
    chat.dataset.version = next.dataset.version;
    chat.innerHTML = next.innerHTML;
    const box = chat.querySelector('.chat-form textarea');
    if (box) { box.value = typed; if (focused) box.focus(); }
    poll();
  };

  // poll asks for the page's version every 3 seconds while the last turn is the owner's.
  const poll = () => {
    clearTimeout(timer);
    if (!chat.querySelector('.thinking')) return;
    timer = setTimeout(async () => {
      try {
        const r = await fetch(`/days/${chat.dataset.day}/conversation`, { ...same, headers: { Accept: 'application/vnd.siren+json' } });
        const props = (await r.json()).properties || {}, version = props.version || '';
        if (r.status === 401) { if (props.code === 'locked') location.replace('/locked?next=' + encodeURIComponent(location.pathname)); return; } // the lock page, and no more polls
        if (version !== chat.dataset.version) {
          const page = await fetch(location.href, { ...same, headers: { Accept: 'text/html' } });
          if (page.ok) { swap(await page.text()); return; }
        }
      } catch (e) { /* the column stays as it is */ }
      poll();
    }, 3000);
  };

  // The composer posts with fetch; the answer is the page it returns to, and the column is swapped from it.
  chat.addEventListener('submit', async (e) => {
    const form = e.target;
    if (!form.matches('form.chat-form')) return;
    e.preventDefault();
    try {
      const r = await fetch(form.action, { ...same, method: form.method, body: new URLSearchParams(new FormData(form)), headers: { Accept: 'text/html' } });
      if (!r.ok) { if (r.status < 500) form.submit(); return; } // a refusal: its page says why. A fault: the draft stays
      form.dataset.sent = '1';
      swap(await r.text());
    } catch (err) { /* the draft stays in the box */ }
  });

  // Enter in the composer sends it through the submit path above; the browser's own check of the form comes first.
  chat.addEventListener('keydown', (e) => {
    const box = e.target;
    if (e.key !== 'Enter' || e.shiftKey || e.isComposing || !box.matches('.chat-form textarea')) return;
    e.preventDefault();
    if (box.form.reportValidity()) box.form.requestSubmit();
  });

  if (chat.id === 'chat') { // the screen is no popover
    chat.addEventListener('toggle', (e) => {
      try { localStorage.setItem(KEY, e.newState === 'open' ? '1' : '0'); } catch (err) { /* no storage */ }
    });
    try { if (localStorage.getItem(KEY) === '1') chat.showPopover(); } catch (err) { /* no storage */ }
  }
  poll();
})();
