// The service worker of aminte-web (README.md, "Offline"). The journal takes an entry while the server does not answer:
// the worker keeps a capture that got no answer in an outbox (IndexedDB, in this browser only) and sends it again when
// the server answers. The same import_key goes with an entry on every try, so the server writes it one time (D28).
// The worker keeps one page, /offline, which holds nothing of the life log, and it keeps no answer of a session. It
// answers a capture, a navigation and the Remove button of /offline. Every other request goes to the network as before.
const WAIT = 8000; // the milliseconds that a capture, and a navigation to a start address, wait for the server
const PAGE = '/offline', MARK = '<div id="outbox"></div>', KEPT = 'aminte-offline', FORM = 'application/x-www-form-urlencoded';
const CAPTURE = /^\/days\/([^/]+)\/capture$/;
// STARTS are the addresses that the installed app opens (the start_url and the shortcuts of the manifest). Only they
// have a limit of time: any other page, such as the integrity check, can take long.
const STARTS = ['/', '/days/today'];
const plain = (words, status) => new Response(words, { status, headers: { 'Content-Type': 'text/plain; charset=utf-8' } });

// outbox runs one request on the store of the entries: {key, path, body, queued, refused}. key is the import_key, body
// is the fields of the form, queued is the time it was kept, and refused is the status of a refusal that will not pass.
// The version of the database never changes, so a new worker reads the entries that an old one kept.
const outbox = async (mode, run) => {
  const db = await new Promise((ok, no) => {
    const open = indexedDB.open('aminte-outbox', 1);
    open.onupgradeneeded = () => open.result.createObjectStore('entries', { keyPath: 'key' });
    open.onsuccess = () => ok(open.result);
    open.onerror = () => no(open.error);
  });
  return new Promise((ok, no) => {
    const tx = db.transaction('entries', mode), request = run(tx.objectStore('entries'));
    tx.oncomplete = () => { db.close(); ok(request.result); };
    tx.onabort = () => { db.close(); no(tx.error); };
  });
};
const entries = async () => (await outbox('readonly', (s) => s.getAll())).sort((a, b) => a.queued - b.queued);

// post sends the fields of one capture, and answers null when no answer came in time. The server answers a capture that
// it wrote, or wrote before (Already added), with a 303, and it sends no other redirect to a POST of this route. The
// worker does not follow it, so the answer has the type opaqueredirect. from is the request of the page, for its
// Accept and its Referer: the page of a refusal goes back to the page of the form.
const post = (path, body, from) => fetch(path, {
  method: 'POST', body, redirect: 'manual', signal: AbortSignal.timeout(WAIT), referrer: from ? from.referrer : '',
  headers: { 'Content-Type': FORM, Accept: (from && from.headers.get('Accept')) || '*/*' },
}).catch(() => null);
const written = (res) => res.type === 'opaqueredirect' || res.ok;

// keep puts a capture in the outbox, on the day and with the time of this device. A mood alone has no time.
const keep = async (path, fields) => {
  const d = new Date(), two = (n) => String(n).padStart(2, '0');
  const today = `${d.getFullYear()}-${two(d.getMonth() + 1)}-${two(d.getDate())}`;
  if ((fields.get('text') || '').trim() && !fields.get('time')) fields.set('time', `${two(d.getHours())}:${two(d.getMinutes())}`);
  // A form that the browser restored from its history sends its old key with new words: they are a new entry.
  const waits = (await entries()).find((e) => e.key === fields.get('import_key'));
  if (waits && new URLSearchParams(waits.body).get('text') !== fields.get('text')) fields.set('import_key', crypto.randomUUID());
  const day = (all, name) => `/days/${name === 'today' ? today : name}/capture`;
  await outbox('readwrite', (s) => s.put({ key: fields.get('import_key'), path: path.replace(CAPTURE, day), body: fields.toString(), queued: Date.now(), refused: 0 }));
};

// capture answers the POST of a capture form. The worker sends the request itself, with a key, so the first try and
// each later try have the same key. An answer of the server goes to the page: the 303 of a write, or the page of a
// refusal. With no answer, no session, a locked session (401) or no server behind the proxy (502 to 504), the entry
// goes to the outbox and the page goes to /offline.
const capture = async (req) => {
  const path = new URL(req.url).pathname, fields = new URLSearchParams(await req.text());
  if (!fields.get('import_key')) fields.set('import_key', crypto.randomUUID()); // /offline and the share target send none
  const res = await post(path, fields.toString(), req);
  if (res && res.status !== 401 && !(res.status >= 502 && res.status <= 504)) return res;
  try {
    await keep(path, fields);
  } catch (e) { // refused, not lost: the owner can copy the words
    return plain(`This browser cannot keep the entry, and the server did not take it. Copy it:\n\n${fields.get('text') || ''}`, 503);
  }
  return Response.redirect(PAGE, 303);
};

// flush sends the entries that wait, the oldest first, one flush at a time for every tab. It answers 'silent' when the
// server gave no answer, 'refused' when the server refused an entry for good (a 4xx that is not 401: the owner must
// see it), and 'sent' otherwise. On a 401 or a fault of the server the entries wait, and the page of the server says why.
let flushing = null;
const flush = () => flushing || (flushing = (async () => {
  let state = 'sent';
  for (const e of await entries()) {
    if (e.refused) continue;
    const res = await post(e.path, e.body);
    if (!res) return 'silent';
    if (written(res)) await outbox('readwrite', (s) => s.delete(e.key));
    else if (res.status === 401 || res.status >= 500) return state;
    else { state = 'refused'; await outbox('readwrite', (s) => s.put({ ...e, refused: res.status })); }
  }
  return state;
})().finally(() => { flushing = null; }));

// offline is the kept page, with the list of the entries that wait in the place of its mark.
const esc = (s) => String(s).replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`);
const row = (e) => {
  const f = new URLSearchParams(e.body), mood = f.get('mood') ? ` · mood ${esc(f.get('mood'))}` : '';
  const refused = !e.refused ? '' : `<div class="alert danger" role="alert"><p><strong>Refused.</strong> The server answered ${esc(e.refused)} and will not take this entry. Copy the words if you want them, then remove the entry.</p></div>
<form method="POST" action="${PAGE}" class="inline"><input type="hidden" name="drop" value="${esc(e.key)}"><button class="quiet">Remove</button></form>`;
  return `<li><span class="meta">${esc(e.path.match(CAPTURE)[1])} ${esc(f.get('time') || '')}${mood}</span>
<textarea readonly rows="3" aria-label="Entry">${esc(f.get('text') || '')}</textarea>${refused}</li>`;
};
const offline = async () => {
  const kept = await (await caches.open(KEPT)).match(PAGE) || await fetch(PAGE).catch(() => null); // the browser cleared the kept page
  if (!kept || !kept.ok) return plain('The server does not answer.', 503);
  const all = await entries().catch(() => []);
  const list = !all.length ? '' : `<section aria-label="Entries that wait"><h2>${all.length === 1 ? '1 entry waits' : `${all.length} entries wait`}</h2>
<ul class="plain">${all.map(row).join('')}</ul></section>`;
  return new Response((await kept.text()).replace(MARK, () => list), { headers: { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' } });
};

// renew gets /offline again, so the kept page is the page of the server that runs now.
const renew = async () => {
  const res = await fetch(PAGE, { cache: 'no-cache' });
  if (!res.ok) throw new Error(`GET ${PAGE}: ${res.status}`);
  await (await caches.open(KEPT)).put(PAGE, res);
};

// navigate answers a navigation. Entries that wait go first, so the page shows them. The request of the page goes to
// the network unchanged. /offline shows when the network gives an error, or, for a start address, no answer in time.
const navigate = async (event) => {
  const path = new URL(event.request.url).pathname;
  if (path === PAGE) return offline();
  if (await entries().then((all) => all.some((e) => !e.refused), () => false)) {
    const state = await flush().catch(() => 'sent');
    if (state === 'silent') return offline();
    if (state === 'refused') return Response.redirect(PAGE, 303);
  }
  try {
    const answer = fetch(event.request);
    const res = await (STARTS.includes(path) ? Promise.race([answer, new Promise((ok, no) => setTimeout(no, WAIT))]) : answer);
    if (path === '/') event.waitUntil(renew().catch(() => {}));
    return res;
  } catch (e) {
    return offline();
  }
};

// remove answers the Remove button of a refused entry: POST /offline with drop, the key of the entry.
const remove = async (req) => {
  const key = new URLSearchParams(await req.text()).get('drop');
  if (key) await outbox('readwrite', (s) => s.delete(key));
  return Response.redirect(PAGE, 303);
};

addEventListener('install', (event) => event.waitUntil(renew().then(() => skipWaiting())));
addEventListener('activate', (event) => event.waitUntil(clients.claim()));
addEventListener('fetch', (event) => {
  const req = event.request, url = new URL(req.url);
  if (url.origin !== location.origin || req.headers.has('Sec-Purpose')) return; // a prerender is not a request of the owner
  const form = req.method === 'POST' && (req.headers.get('Content-Type') || '').startsWith(FORM);
  if (form && url.pathname === PAGE) event.respondWith(remove(req));
  else if (form && CAPTURE.test(url.pathname)) event.respondWith(capture(req));
  else if (req.method === 'GET' && req.mode === 'navigate') event.respondWith(navigate(event));
});
