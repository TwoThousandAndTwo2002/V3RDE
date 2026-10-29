// X35e: web voice capture with the Web Audio API (no React, no Expo imports,
// so it is tested alone in Chromium). expo-audio 58.0.10's useAudioStream is
// a stub on the web (build/AudioStream.web.js returns stream: null).
//   getUserMedia (https only, MDN) -> MediaStreamSource -> AudioWorkletNode
//   "voice-capture" (public/voice-capture-worklet.js: channel 0 of each
//   128-frame block, posted to this thread) -> chunks at the device rate ->
//   src/wav.ts: 16 kHz mono 16-bit WAV.
import { voiceWav } from "./wav";

export const WORKLET_URL = "/voice-capture-worklet.js";

export class VoiceError extends Error {
  code: "denied" | "unsupported" | "failed";
  constructor(code: VoiceError["code"], message: string) { super(message); this.code = code; }
}

export type WebCapture = {
  stop: () => { wav: Uint8Array<ArrayBuffer>; ms: number; deviceRate: number };
  cancel: () => void;
};

export async function startWebCapture(workletUrl = WORKLET_URL): Promise<WebCapture> {
  const md = typeof navigator !== "undefined" ? navigator.mediaDevices : undefined;
  if (!md?.getUserMedia || typeof AudioWorkletNode === "undefined") {
    throw new VoiceError("unsupported", "This browser cannot record here (https is needed)");
  }
  let media: MediaStream;
  try {
    media = await md.getUserMedia({ audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true, autoGainControl: true } });
  } catch (e: any) {
    const denied = e?.name === "NotAllowedError";
    throw new VoiceError(denied ? "denied" : "failed", denied ? "Allow the microphone to talk to the app" : "The microphone did not start");
  }
  const ctx = new AudioContext();
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
    if (ctx.state === "suspended") await ctx.resume();
  } catch (e: any) {
    media.getTracks().forEach((t) => t.stop());
    ctx.close().catch(() => undefined);
    throw new VoiceError("failed", e?.message || "The microphone did not start");
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
