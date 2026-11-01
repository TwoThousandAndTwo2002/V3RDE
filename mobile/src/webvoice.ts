// X35e: web voice capture with the Web Audio API (no React, no Expo imports,
// so it is tested alone in Chromium). expo-audio 58.0.10's useAudioStream is
// a stub on the web (build/AudioStream.web.js returns stream: null).
//   getUserMedia (https only, MDN) -> MediaStreamSource -> AudioWorkletNode
//   "voice-capture" (public/voice-capture-worklet.js: channel 0 of each
//   128-frame block, posted to this thread) -> chunks at the device rate ->
//   src/wav.ts: 16 kHz mono 16-bit WAV.
import { voiceWav } from "./wav";
import { MIC } from "./words";

export const WORKLET_URL = "/voice-capture-worklet.js";

export class VoiceError extends Error {
  code: "denied" | "unsupported" | "failed";
  constructor(code: VoiceError["code"], message: string) { super(message); this.code = code; }
}

export type WebCapture = {
  stop: () => { wav: Uint8Array<ArrayBuffer>; ms: number; deviceRate: number };
  cancel: () => void;
};

// X48n (T9 2026-10-30: on iOS "user is able to press the button to start voice
// recording but not stop it"; the desktop browsers and Android work): iOS
// Safari starts an AudioContext only from a user gesture. The context was
// made after `await getUserMedia`, outside the tap, so it stayed "suspended"
// and `await ctx.resume()` never ended: the start never ended, and Stop waits
// for the start. Now the context is made and resumed at once, in the tap,
// before the microphone prompt; and the wait for "running" has a time limit,
// so a start that cannot run fails with a message instead of hanging.
export const RESUME_WAIT_MS = 2000;

type AudioContextCtor = new () => AudioContext;

export async function startWebCapture(workletUrl = WORKLET_URL, resumeWaitMs = RESUME_WAIT_MS): Promise<WebCapture> {
  const md = typeof navigator !== "undefined" ? navigator.mediaDevices : undefined;
  const Ctx: AudioContextCtor | undefined = typeof AudioContext !== "undefined" ? AudioContext
    : (globalThis as { webkitAudioContext?: AudioContextCtor }).webkitAudioContext;
  if (!md?.getUserMedia || typeof AudioWorkletNode === "undefined" || !Ctx) {
    throw new VoiceError("unsupported", MIC.https);
  }
  // in the tap (no await before this line): iOS lets the audio start only here
  const ctx = new Ctx();
  const resumed = ctx.state === "suspended" ? ctx.resume().catch(() => undefined) : Promise.resolve();
  let media: MediaStream;
  try {
    media = await md.getUserMedia({ audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true, autoGainControl: true } });
  } catch (e: any) {
    ctx.close().catch(() => undefined);
    const denied = e?.name === "NotAllowedError";
    throw new VoiceError(denied ? "denied" : "failed", denied ? MIC.allow : MIC.failed);
  }
  const chunks: Float32Array[] = [];
  let node: AudioWorkletNode;
  try {
    await ctx.audioWorklet.addModule(workletUrl);
    const src = ctx.createMediaStreamSource(media);
    node = new AudioWorkletNode(ctx, "voice-capture");
    node.port.onmessage = (e: MessageEvent) => { if (e.data instanceof Float32Array) chunks.push(e.data); };
    const mute = ctx.createGain();
    mute.gain.value = 0; // keeps the node pulled by the graph, with no echo
    src.connect(node);
    node.connect(mute);
    mute.connect(ctx.destination);
    // the resume asked for in the tap, or one more now; never wait without a limit
    if (ctx.state !== "running") {
      await Promise.race([resumed.then(() => (ctx.state === "suspended" ? ctx.resume() : undefined)),
        new Promise((go) => setTimeout(go, resumeWaitMs))]);
    }
    if (ctx.state !== "running") throw new Error(MIC.tapAgain);
  } catch (e: any) {
    media.getTracks().forEach((t) => t.stop());
    ctx.close().catch(() => undefined);
    throw new VoiceError("failed", e?.message || MIC.failed);
  }
  let done = false;
  const release = () => {
    if (done) return;
    done = true;
    media.getTracks().forEach((t) => t.stop());
    try { node.port.onmessage = null; node.disconnect(); } catch { /* gone */ }
    ctx.close().catch(() => undefined);
  };
  return {
    stop: () => {
      const deviceRate = ctx.sampleRate;
      release();
      return { ...voiceWav(chunks, deviceRate), deviceRate };
    },
    cancel: release,
  };
}
