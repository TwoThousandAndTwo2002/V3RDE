// X36a: the automatic stop keeps the clip (review finding 7 of X35e).
import { test } from "node:test";
import { strict as assert } from "node:assert";
import { ClipBusy, ClipCancelled, ClipSession } from "../src/clipsession.ts";

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

function parts(log: string[], clip = "clip-1") {
  return {
    begin: async () => { log.push("begin"); },
    finish: async () => { log.push("finish"); return clip; },
    abort: () => { log.push("abort"); },
  };
}

test("the automatic stop gives the clip to onAutoStop and to the Stop tap after it", async () => {
  const log: string[] = [];
  const states: boolean[] = [];
  let auto: string | null = null;
  const s = new ClipSession<string>(20, (r) => states.push(r), (c) => { auto = c; });
  await s.start(parts(log));
  assert.equal(s.recording, true);
  await sleep(60);
  assert.equal(auto, "clip-1");
  assert.equal(s.recording, false);
  assert.equal(await s.stop(), "clip-1"); // the late Stop tap: the same clip, no error
  assert.deepEqual(log, ["begin", "finish"]); // finished once
  assert.deepEqual(states, [true, false]);
  await assert.rejects(s.stop(), /Not recording/);
});

test("a manual stop before the limit returns the clip and clears the timer", async () => {
  const log: string[] = [];
  let auto = 0;
  const s = new ClipSession<string>(30, undefined, () => { auto++; });
  await s.start(parts(log));
  assert.equal(await s.stop(), "clip-1");
  await sleep(60);
  assert.equal(auto, 0);
  assert.deepEqual(log, ["begin", "finish"]);
});

test("a second start while recording throws and keeps the first microphone", async () => {
  const log: string[] = [];
  const s = new ClipSession<string>(1000);
  await s.start(parts(log));
  await assert.rejects(s.start(parts(log, "clip-2")), ClipBusy);
  assert.equal(await s.stop(), "clip-1");
  assert.deepEqual(log, ["begin", "finish"]); // no second begin, no abort of the first
  await s.start(parts(log, "clip-3")); // after the stop a new clip can start
  assert.equal(await s.stop(), "clip-3");
});

test("cancel drops the clip and stops the timer (also on unmount)", async () => {
  const log: string[] = [];
  let auto = 0;
  const s = new ClipSession<string>(20, undefined, () => { auto++; });
  await s.start(parts(log));
  s.cancel();
  await sleep(50);
  assert.equal(auto, 0);
  assert.deepEqual(log, ["begin", "abort"]);
  await assert.rejects(s.stop(), /Not recording/);
});

test("a failed begin leaves the session free", async () => {
  const s = new ClipSession<string>(1000);
  await assert.rejects(s.start({ begin: async () => { throw new Error("denied"); }, finish: async () => "x", abort: () => undefined }), /denied/);
  assert.equal(s.recording, false);
  const log: string[] = [];
  await s.start(parts(log));
  assert.equal(await s.stop(), "clip-1");
});

// The third review (findings 3, 4, 7): a cancel always wins.
function slowParts(log: string[], beginMs: number, finishMs: number, clip = "clip-s") {
  return {
    begin: async () => { await sleep(beginMs); log.push("begin"); },
    finish: async () => { await sleep(finishMs); log.push("finish"); return clip; },
    abort: () => { log.push("abort"); },
  };
}

test("cancel during begin closes the microphone when it opens, and no timer runs", async () => {
  const log: string[] = [];
  let auto = 0;
  const s = new ClipSession<string>(20, undefined, () => { auto++; });
  const p = s.start(slowParts(log, 30, 0));
  assert.equal(s.recording, true);
  s.cancel();
  assert.equal(s.recording, false);
  await assert.rejects(p, ClipCancelled);
  await sleep(60);
  assert.equal(auto, 0);
  assert.equal(s.recording, false);
  assert.deepEqual(log, ["begin", "abort"]);
});

test("cancel while the automatic stop finishes: no onAutoStop", async () => {
  const log: string[] = [];
  let auto = 0;
  const s = new ClipSession<string>(10, undefined, () => { auto++; });
  await s.start(slowParts(log, 0, 40));
  await sleep(25); // the timer fired, finish runs
  s.cancel();
  await sleep(50);
  assert.equal(auto, 0);
  await assert.rejects(s.stop(), /Not recording/);
});

test("start while a clip finishes is busy; stop during begin waits and returns the clip", async () => {
  const log: string[] = [];
  const s = new ClipSession<string>(1000);
  await s.start(slowParts(log, 0, 30, "clip-a"));
  const d = s.stop();
  await assert.rejects(s.start(slowParts(log, 0, 0, "clip-b")), ClipBusy);
  assert.equal(await d, "clip-a");
  const p = s.start(slowParts(log, 20, 0, "clip-c"));
  const st = s.stop(); // the Stop tap while the microphone opens
  await p;
  assert.equal(await st, "clip-c");
  assert.equal(s.recording, false);
});
