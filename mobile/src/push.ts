// WS1 (T9 2026-11-01: "lets move forward with aws socket"): the app's side of the push channel (the web app only:
// a phone's WebSocket sends another Origin, which the box refuses). When the box has push on, a signed-in app gets a
// one-time ticket (POST /api/push/ticket) and opens the API Gateway WebSocket with it. A message has only a kind, an
// id and a status ({"kind":"rescue","id":"r_..","status":"done"}); it never has data: it only makes the matching wait
// end at once, and the app then reads the thing through its usual route. The app sends "ping" every minute; the box
// answers {"kind":"pong"} for a connection it knows. The socket counts as working only while pongs come, so a broken
// push (a box that lost its list, no credentials) never makes the polls slow. The polling stays: with a working socket
// the polls are slower (they are the fallback); without one they are as before. Pure logic with an injected WebSocket,
// timers and clock, so the tests run it directly.

export type PushMsg = { kind: string; id?: string; status?: string };
export type Ticket = { ticket: string; url: string; key?: string };

/** X57: the ping message. The SME's $default route maps no connection id, so the ping carries the ticket's key, by
 *  which the box finds the socket ({"action":"ping","key":".."}); with no key it is the plain "ping" as before. */
export function pingMessage(key?: string): string {
  return key ? JSON.stringify({ action: "ping", key }) : "ping";
}

/** The longest a socket stays open before the app opens a new one (API Gateway: 10 min idle, 2 h at most). */
export const PUSH_REOPEN_MS = 9 * 60_000;
/** The app sends "ping" this often (and once when the socket opens). */
export const PUSH_PING_MS = 60_000;
/** The socket counts as working this long after a pong. */
export const PUSH_PONG_OK_MS = 90_000;
/** The waits before a new try after a failed ticket or a socket that closed before it worked (fixed steps). */
export const PUSH_RETRY_MS = [2_000, 5_000, 15_000, 60_000];

type Listener = (m: PushMsg) => void;
type WSLike = { onopen: any; onmessage: any; onclose: any; onerror: any; close(): void; send(s: string): void };
type Timers = { set: (fn: () => void, ms: number) => any; clear: (id: any) => void; now: () => number };
const realTimers: Timers = { set: (fn, ms) => setTimeout(fn, ms), clear: (id) => clearTimeout(id), now: () => Date.now() };

const listeners = new Set<Listener>();
let lastPong = -Infinity;
let clock: () => number = () => Date.now();
/** The last messages (kind, id) and when they came: a wait that starts after a message for it does not miss it. */
const recent: { m: PushMsg; at: number }[] = [];

export function onPush(l: Listener): () => void {
  listeners.add(l);
  return () => listeners.delete(l);
}

/** True while the socket works (a pong in the last PUSH_PONG_OK_MS): the polls may be slower. */
export function pushOpen(): boolean {
  return clock() - lastPong < PUSH_PONG_OK_MS;
}

/** A message from the socket: only {kind, id, status} strings go on; anything else is dropped. */
export function parsePush(data: unknown): PushMsg | null {
  if (typeof data !== "string" || data.length > 512) return null;
  try {
    const m = JSON.parse(data);
    if (!m || typeof m !== "object" || typeof m.kind !== "string") return null;
    const out: PushMsg = { kind: m.kind };
    if (typeof m.id === "string") out.id = m.id;
    if (typeof m.status === "string") out.status = m.status;
    return out;
  } catch {
    return null;
  }
}

function deliver(m: PushMsg, at: number) {
  if (m.kind === "pong") {
    lastPong = at;
    return;
  }
  recent.push({ m, at });
  while (recent.length > 20) recent.shift();
  listeners.forEach((l) => l(m));
}

/** Wait ms, or less: until a message for which match is true. A message that came at or after `since` (the time the
 *  last read of the job started) ends the wait at once, so a message during that read is not lost. */
export function pushWait(ms: number, match: (m: PushMsg) => boolean, since = Infinity, timers: Timers = realTimers): Promise<void> {
  if (recent.some((r) => r.at >= since && match(r.m))) return Promise.resolve();
  return new Promise((done) => {
    let off = () => {};
    const t = timers.set(() => { off(); done(); }, ms);
    off = onPush((m) => {
      if (!match(m)) return;
      timers.clear(t);
      off();
      done();
    });
  });
}

/** The poll wait of a job: slow with a working socket, as before without one. */
export function pollMs(normal: number, withPush: number): number {
  return pushOpen() ? withPush : normal;
}

/** The current time of the push clock (the time to give pushWait as `since`). */
export function pushNow(): number {
  return clock();
}

/** Runs the socket until the returned stop() is called: ticket, open, ping, reopen every PUSH_REOPEN_MS, try again
 *  after a failure. getTicket gives null when push is off (404): then it stops (no polling change). The retry steps
 *  start again only after a pong (a socket that closes before it works, for example one the box closed because the
 *  user has too many, waits longer each time). */
export function startPush(getTicket: () => Promise<Ticket | null>, makeWS: (url: string) => WSLike,
                          timers: Timers = realTimers): () => void {
  clock = timers.now;
  let stopped = false;
  let ws: WSLike | null = null;
  let timer: any = null;
  let tries = 0;
  const later = (ms: number) => { if (!stopped) timer = timers.set(run, ms); };
  const retry = () => later(PUSH_RETRY_MS[Math.min(tries++, PUSH_RETRY_MS.length - 1)]);
  async function run() {
    timer = null;
    let t: Ticket | null = null;
    try {
      t = await getTicket();
    } catch {
      return retry();
    }
    if (stopped || !t) return;
    const url = t.url + (t.url.includes("?") ? "&" : "?") + "ticket=" + encodeURIComponent(t.ticket);
    let s: WSLike;
    try {
      s = makeWS(url);
    } catch {
      return retry();
    }
    ws = s;
    let reopen: any = null;
    let pinger: any = null;
    const ping = () => {
      if (ws !== s) return;
      try { s.send(pingMessage(t?.key)); } catch { /* the close handler tries again */ }
      pinger = timers.set(ping, PUSH_PING_MS);
    };
    const end = () => {
      if (reopen) timers.clear(reopen);
      if (pinger) timers.clear(pinger);
    };
    s.onopen = () => {
      if (ws !== s) return;
      ping();
      reopen = timers.set(() => { if (ws === s) { end(); ws = null; lastPong = -Infinity; s.close(); later(0); } }, PUSH_REOPEN_MS);
    };
    s.onmessage = (e: { data: unknown }) => {
      const m = parsePush(e?.data);
      if (!m || ws !== s) return;
      if (m.kind === "pong") tries = 0; // the socket works: the retry steps start again
      deliver(m, timers.now());
    };
    s.onclose = s.onerror = () => {
      end();
      if (ws !== s) return;
      ws = null;
      lastPong = -Infinity;
      retry();
    };
  }
  run();
  return () => {
    stopped = true;
    if (timer) timers.clear(timer);
    const s = ws;
    ws = null;
    lastPong = -Infinity;
    if (s) s.close();
  };
}
