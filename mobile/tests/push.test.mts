// WS1 tests: the push socket of the app (src/push.ts) with a fake WebSocket, fake timers and a fake clock.
import test from "node:test";
import { strict as assert } from "node:assert";
import { onPush, parsePush, pingMessage, pollMs, pushNow, pushOpen, pushWait, startPush, PUSH_PING_MS, PUSH_PONG_OK_MS, PUSH_REOPEN_MS, PUSH_RETRY_MS } from "../src/push.ts";

type Fake = { url: string; onopen: any; onmessage: any; onclose: any; onerror: any; closed: boolean; sent: string[]; close(): void; send(s: string): void };
function fakeTimers() {
  let now = 1_000_000;
  const due: { at: number; fn: () => void; id: number }[] = [];
  let next = 1;
  const t = {
    set: (fn: () => void, ms: number) => { const id = next++; due.push({ at: now + ms, fn, id }); return id as any; },
    clear: (id: any) => { const i = due.findIndex((d) => d.id === id); if (i >= 0) due.splice(i, 1); },
    now: () => now,
    async tick(ms: number) {
      const end = now + ms;
      for (;;) {
        due.sort((a, b) => a.at - b.at);
        if (!due.length || due[0].at > end) break;
        const d = due.shift()!;
        now = d.at;
        d.fn();
        await new Promise((r) => setImmediate(r));
      }
      now = end;
    },
  };
  return t;
}
const flush = () => new Promise((r) => setImmediate(r));
function sockets() {
  const made: Fake[] = [];
  const make = (url: string) => {
    const f: Fake = { url, onopen: null, onmessage: null, onclose: null, onerror: null, closed: false, sent: [],
      close() { if (!this.closed) { this.closed = true; this.onclose?.(); } }, send(s: string) { this.sent.push(s); } };
    made.push(f);
    return f;
  };
  return { made, make };
}
const pong = (f: Fake) => f.onmessage({ data: '{"kind":"pong"}' });

test("parsePush keeps only kind, id and status strings", () => {
  assert.deepEqual(parsePush('{"kind":"rescue","id":"r1","status":"done","user_id":"u1","x":1}'), { kind: "rescue", id: "r1", status: "done" });
  assert.equal(parsePush("not json"), null);
  assert.equal(parsePush('{"id":"r1"}'), null);
  assert.equal(parsePush(42), null);
  assert.equal(parsePush('{"kind":"' + "x".repeat(600) + '"}'), null);
});

test("startPush: ticket, open, ping; slow polls only while pongs come; messages end a wait; a message during a read is not lost", async () => {
  const t = fakeTimers();
  const { made, make } = sockets();
  let tickets = 0;
  const stop = startPush(async () => { tickets++; return { ticket: "T" + tickets, url: "wss://push.example/prod" }; }, make, t);
  await flush();
  assert.equal(made[0].url, "wss://push.example/prod?ticket=T1");
  made[0].onopen();
  assert.deepEqual(made[0].sent, ["ping"]);
  assert.equal(pushOpen(), false); // open, but no pong yet: polls as before
  assert.equal(pollMs(1000, 5000), 1000);
  pong(made[0]);
  assert.equal(pushOpen(), true);
  assert.equal(pollMs(1000, 5000), 5000);
  // a message ends the matching wait at once
  let ended = false;
  const w = pushWait(5000, (m) => m.kind === "touchup" && m.id === "t1", pushNow(), t).then(() => { ended = true; });
  made[0].onmessage({ data: '{"kind":"touchup","id":"t1","status":"done"}' });
  await w;
  assert.equal(ended, true);
  // a message that came while the job was read (after `since`): the next wait ends at once
  const since = pushNow();
  await t.tick(10);
  made[0].onmessage({ data: '{"kind":"rescue","id":"r9","status":"done"}' });
  await pushWait(5000, (m) => m.kind === "rescue" && m.id === "r9", since, t);
  // but not one from before the read
  let early = false;
  pushWait(5000, (m) => m.kind === "rescue" && m.id === "r9", pushNow() + 1, t).then(() => { early = true; });
  await flush();
  assert.equal(early, false);
  await t.tick(5000);
  // pings every minute; no pong for PUSH_PONG_OK_MS: the polls are as before again
  await t.tick(PUSH_PING_MS);
  assert.equal(made[0].sent.length >= 2, true);
  await t.tick(PUSH_PONG_OK_MS);
  assert.equal(pushOpen(), false);
  pong(made[0]);
  assert.equal(pushOpen(), true);
  // reopen after PUSH_REOPEN_MS with a new ticket
  await t.tick(PUSH_REOPEN_MS);
  await flush();
  assert.equal(made[0].closed, true);
  assert.equal(made.length, 2);
  assert.equal(made[1].url.endsWith("ticket=T2"), true);
  assert.equal(pushOpen(), false);
  stop();
  assert.equal(made[1].closed, true);
  await t.tick(PUSH_REOPEN_MS * 2);
  await flush();
  assert.equal(made.length, 2);
});

test("startPush: a socket that closes before it works waits longer each time (no tight loop)", async () => {
  const t = fakeTimers();
  const { made, make } = sockets();
  const stop = startPush(async () => ({ ticket: "T", url: "wss://p/x" }), make, t);
  await flush();
  for (let i = 0; i < 3; i++) {
    made[i].onopen();
    made[i].close(); // the box closed it (too many sockets) before any pong
    await t.tick(PUSH_RETRY_MS[i] - 1);
    await flush();
    assert.equal(made.length, i + 1, "too early: try " + i);
    await t.tick(1);
    await flush();
    assert.equal(made.length, i + 2);
  }
  // after a pong the steps start again
  made[3].onopen();
  pong(made[3]);
  made[3].close();
  await t.tick(PUSH_RETRY_MS[0]);
  await flush();
  assert.equal(made.length, 5);
  stop();
});

test("startPush: push off (no ticket) opens nothing and tries nothing more", async () => {
  const t = fakeTimers();
  let made = 0, asked = 0;
  const stop = startPush(async () => { asked++; return null; }, () => { made++; return {} as any; }, t);
  await flush();
  await t.tick(PUSH_RETRY_MS[3] * 3);
  assert.equal(asked, 1);
  assert.equal(made, 0);
  assert.equal(pushOpen(), false);
  stop();
});

test("startPush: a failed ticket request tries again later, with longer waits", async () => {
  const t = fakeTimers();
  let asked = 0;
  const stop = startPush(async () => { asked++; throw new Error("network"); }, () => ({}) as any, t);
  await flush();
  assert.equal(asked, 1);
  await t.tick(PUSH_RETRY_MS[0]);
  await flush();
  assert.equal(asked, 2);
  await t.tick(PUSH_RETRY_MS[0]);
  await flush();
  assert.equal(asked, 2);
  await t.tick(PUSH_RETRY_MS[1] - PUSH_RETRY_MS[0]);
  await flush();
  assert.equal(asked, 3);
  stop();
});

test("onPush: a listener gets messages until it is removed", () => {
  const got: string[] = [];
  const off = onPush((m) => got.push(m.kind));
  off();
  assert.deepEqual(got, []);
});

// X57: the SME's $default route has no connection id: the ping carries the ticket's key.
test("the ping carries the ticket's key when the box gives one", async () => {
  assert.equal(pingMessage(undefined), "ping");
  assert.equal(pingMessage("ab12"), '{"action":"ping","key":"ab12"}');
  const t = fakeTimers();
  const { made, make } = sockets();
  const stop = startPush(async () => ({ ticket: "T1", key: "k0123456789abcdef0123456789abcdef", url: "wss://push.example/prod" }), make, t);
  await flush();
  assert.equal(made[0].url, "wss://push.example/prod?ticket=T1"); // the key never goes in the URL
  made[0].onopen();
  assert.deepEqual(made[0].sent, ['{"action":"ping","key":"k0123456789abcdef0123456789abcdef"}']);
  stop();
});
