// X35e: the voice recording is made into WAV on the device (T9 2026-10-28:
// "this should be done at edge"). The speech endpoint takes only a
// RIFF/WAVE file, mono, 16-bit integer PCM, at 16 kHz or 24 kHz
// (dev.meta.ai/docs/speech-to-text), so every platform sends exactly that:
// 16 kHz, mono, 16-bit, with exact header sizes. The server then sends the
// bytes on unchanged (post/wav.go: the pass-through case).
//
// Pure functions only (tested in tests/wav.test.mts). The resampler is the
// same rule as the server's fallback (post/wav.go resample): a moving
// average over the ratio when the rate goes down, then linear interpolation.

export const VOICE_RATE = 16000;
export const VOICE_MAX_MS = 60_000; // the server's -max-audio

// resample changes the rate of mono float samples in [-1, 1].
export function resample(input: Float32Array, from: number, to: number): Float32Array {
  if (from === to || input.length === 0) return input;
  const ratio = from / to;
  let src = input;
  if (ratio > 1) {
    const w = Math.ceil(ratio);
    src = new Float32Array(input.length);
    let sum = 0;
    for (let i = 0; i < input.length; i++) {
      sum += input[i];
      if (i >= w) sum -= input[i - w];
      src[i] = sum / Math.min(w, i + 1);
    }
  }
  const n = Math.floor(input.length / ratio);
  const out = new Float32Array(n);
  for (let i = 0; i < n; i++) {
    const x = i * ratio;
    const j = Math.floor(x);
    const f = x - j;
    const a = src[j];
    const b = j + 1 < src.length ? src[j + 1] : a;
    out[i] = a + (b - a) * f;
  }
  return out;
}

// toInt16 clamps floats to [-1, 1] and scales them to signed 16-bit.
export function toInt16(input: Float32Array): Int16Array {
  const out = new Int16Array(input.length);
  for (let i = 0; i < input.length; i++) {
    const v = Math.max(-1, Math.min(1, input[i]));
    out[i] = Math.round(v * 32767);
  }
  return out;
}

// joinFloat joins the captured chunks into one buffer.
export function joinFloat(chunks: Float32Array[]): Float32Array {
  let n = 0;
  for (const c of chunks) n += c.length;
  const out = new Float32Array(n);
  let o = 0;
  for (const c of chunks) { out.set(c, o); o += c.length; }
  return out;
}

// encodeWav writes the canonical 44-byte header (RIFF, "fmt " of 16 bytes,
// PCM format 1, then "data") and the samples, little-endian. The sizes are
// exact, so the server's strict reader takes the file as it is.
export function encodeWav(samples: Int16Array, rate: number): Uint8Array<ArrayBuffer> {
  const data = samples.length * 2;
  const buf = new ArrayBuffer(44 + data);
  const v = new DataView(buf);
  const ascii = (o: number, s: string) => { for (let i = 0; i < s.length; i++) v.setUint8(o + i, s.charCodeAt(i)); };
  ascii(0, "RIFF");
  v.setUint32(4, 36 + data, true);
  ascii(8, "WAVE");
  ascii(12, "fmt ");
  v.setUint32(16, 16, true);       // fmt chunk size
  v.setUint16(20, 1, true);        // PCM
  v.setUint16(22, 1, true);        // mono
  v.setUint32(24, rate, true);
  v.setUint32(28, rate * 2, true); // bytes per second
  v.setUint16(32, 2, true);        // block align
  v.setUint16(34, 16, true);       // bits per sample
  ascii(36, "data");
  v.setUint32(40, data, true);
  for (let i = 0; i < samples.length; i++) v.setInt16(44 + i * 2, samples[i], true);
  return new Uint8Array(buf);
}

// voiceWav is the whole edge step: captured float chunks at `rate` -> a
// 16 kHz mono 16-bit WAV, cut at VOICE_MAX_MS. It also gives the length.
export function voiceWav(chunks: Float32Array[], rate: number): { wav: Uint8Array<ArrayBuffer>; ms: number } {
  let mono = resample(joinFloat(chunks), rate, VOICE_RATE);
  const max = Math.floor(VOICE_RATE * VOICE_MAX_MS / 1000);
  if (mono.length > max) mono = mono.subarray(0, max);
  return { wav: encodeWav(toInt16(mono), VOICE_RATE), ms: Math.round(mono.length * 1000 / VOICE_RATE) };
}

// wavInfo reads a canonical header (tests, and a check before upload).
export function wavInfo(b: Uint8Array): { rate: number; channels: number; bits: number; format: number; dataBytes: number; exact: boolean } | null {
  if (b.length < 44) return null;
  const v = new DataView(b.buffer, b.byteOffset, b.byteLength);
  const s = (o: number) => String.fromCharCode(b[o], b[o + 1], b[o + 2], b[o + 3]);
  if (s(0) !== "RIFF" || s(8) !== "WAVE" || s(12) !== "fmt " || s(36) !== "data") return null;
  const dataBytes = v.getUint32(40, true);
  return { format: v.getUint16(20, true), channels: v.getUint16(22, true), rate: v.getUint32(24, true),
    bits: v.getUint16(34, true), dataBytes, exact: v.getUint32(4, true) === b.length - 8 && dataBytes === b.length - 44 };
}
