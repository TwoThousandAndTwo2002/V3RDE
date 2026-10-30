// X48n: the web voice capture starts its audio in the tap (iOS Safari) and never hangs (src/webvoice.ts).
import test from "node:test";
import { strict as assert } from "node:assert";
import { register } from "node:module";

type Log = string[];
function fakes(log: Log, opts: { resumes: boolean; mic?: "ok" | "denied" }) {
  class FakeCtx {
    state: "suspended" | "running" | "closed" = "suspended";
    sampleRate = 48000;
    audioWorklet = { addModule: async () => { log.push("addModule"); } };
    destination = {};
    constructor() { log.push("ctx"); }
    resume() {
      log.push("resume");
      // iOS: only a resume that was asked for inside the tap runs; a later one never ends
      if (opts.resumes) { this.state = "running"; return Promise.resolve(); }
      return new Promise<void>(() => undefined);
    }
    close() { this.state = "closed"; log.push("close"); return Promise.resolve(); }
    createMediaStreamSource() { return { connect: () => undefined }; }
    createGain() { return { gain: { value: 1 }, connect: () => undefined }; }
  }
  class FakeNode { port = { onmessage: null as unknown }; connect() { /* graph */ } disconnect() { /* graph */ } }
  const track = { stop: () => log.push("track.stop") };
  Object.assign(globalThis, { AudioContext: FakeCtx, AudioWorkletNode: FakeNode });
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: { mediaDevices: { getUserMedia: async () => {
    log.push("getUserMedia");
    if (opts.mic === "denied") throw Object.assign(new Error("no"), { name: "NotAllowedError" });
    return { getTracks: () => [track] };
  } } } });
}

// src/webvoice.ts imports "./wav" with no extension (the app's bundler adds it); here a resolve hook tries ".ts"
register("data:text/javascript," + encodeURIComponent(
  "export async function resolve(s, c, next) { if (/^\\.\\.?\\//.test(s) && !/\\.[a-z]+$/.test(s)) { try { return await next(s + '.ts', c); } catch {} } return next(s, c); }"));
const { startWebCapture } = await import("../src/webvoice.ts");

test("the audio starts in the tap: the context is made and resumed before the microphone prompt", async () => {
  const log: Log = [];
  fakes(log, { resumes: true });
  const cap = await startWebCapture("/w.js", 50);
  assert.deepEqual(log.slice(0, 3), ["ctx", "resume", "getUserMedia"]);
  const clip = cap.stop();
  assert.equal(clip.deviceRate, 48000);
  assert.ok(log.includes("track.stop") && log.includes("close"));
});

test("a start whose audio cannot run fails with a message, not a hang (then Stop is not stuck)", async () => {
  const log: Log = [];
  fakes(log, { resumes: false });
  const t0 = Date.now();
  await assert.rejects(startWebCapture("/w.js", 50), /Tap the microphone again/);
  assert.ok(Date.now() - t0 < 1000, "it waited too long");
  assert.ok(log.includes("track.stop") && log.includes("close"), "the microphone and the audio are closed");
});

test("a denied microphone closes the audio context too", async () => {
  const log: Log = [];
  fakes(log, { resumes: true, mic: "denied" });
  await assert.rejects(startWebCapture("/w.js", 50), (e: any) => e.code === "denied");
  assert.ok(log.includes("close"));
});
