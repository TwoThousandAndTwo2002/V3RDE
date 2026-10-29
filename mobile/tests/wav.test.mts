// X35e: the edge WAV (src/wav.ts). The server's strict reader (post/wav.go)
// must take this file byte for byte: 16 kHz, mono, 16-bit, exact sizes.
import { test } from "node:test";
import { strict as assert } from "node:assert";
import { encodeWav, resample, toInt16, joinFloat, voiceWav, wavInfo, VOICE_RATE, VOICE_MAX_MS } from "../src/wav.ts";

const sine = (rate: number, hz: number, ms: number) => {
  const n = Math.round(rate * ms / 1000);
  const a = new Float32Array(n);
  for (let i = 0; i < n; i++) a[i] = 0.5 * Math.sin(2 * Math.PI * hz * i / rate);
  return a;
};
const crossings = (a: ArrayLike<number>) => { let c = 0; for (let i = 1; i < a.length; i++) if ((a[i - 1] < 0) !== (a[i] < 0)) c++; return c; };

test("encodeWav writes the canonical 44-byte header with exact sizes", () => {
  const w = encodeWav(new Int16Array([0, 1, -1, 32767, -32768]), 16000);
  assert.equal(w.length, 44 + 10);
  assert.deepEqual(wavInfo(w), { format: 1, channels: 1, rate: 16000, bits: 16, dataBytes: 10, exact: true });
  const v = new DataView(w.buffer);
  assert.equal(v.getUint32(28, true), 32000); // bytes per second
  assert.equal(v.getUint16(32, true), 2);     // block align
  assert.equal(v.getInt16(44 + 6, true), 32767);
  assert.equal(v.getInt16(44 + 8, true), -32768);
});

test("resample keeps the length ratio and the pitch; toInt16 clamps", () => {
  const s48 = sine(48000, 440, 1000);
  const s16 = resample(s48, 48000, 16000);
  assert.equal(s16.length, 16000);
  // 440 Hz for 1 s = 880 sign changes, before and after
  assert.ok(Math.abs(crossings(s16) - crossings(s48)) <= 2, `${crossings(s16)} vs ${crossings(s48)}`);
  assert.equal(resample(s48, 48000, 48000), s48);
  assert.equal(resample(sine(44100, 300, 500), 44100, 16000).length, 8000);
  assert.equal(resample(sine(8000, 300, 500), 8000, 16000).length, 8000);
  assert.deepEqual(Array.from(toInt16(new Float32Array([2, -2, 0.5, 0]))), [32767, -32767, 16384, 0]);
});

test("voiceWav: chunks at the device rate -> 16 kHz WAV, cut at the limit", () => {
  const blocks: Float32Array[] = [];
  const s = sine(48000, 440, 2000);
  for (let i = 0; i < s.length; i += 128) blocks.push(s.subarray(i, i + 128)); // AudioWorklet render quanta
  assert.equal(joinFloat(blocks).length, s.length);
  const { wav, ms } = voiceWav(blocks, 48000);
  assert.equal(ms, 2000);
  const info = wavInfo(wav)!;
  assert.equal(info.rate, VOICE_RATE);
  assert.equal(info.exact, true);
  assert.equal(info.dataBytes, 16000 * 2 * 2);
  const long = voiceWav([new Float32Array(16000 * 61)], 16000);
  assert.equal(long.ms, VOICE_MAX_MS);
  assert.equal(wavInfo(long.wav)!.dataBytes, 16000 * 60 * 2);
  assert.equal(wavInfo(new Uint8Array(10)), null);
});
