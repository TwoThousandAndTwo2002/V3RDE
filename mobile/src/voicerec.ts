// X35e: record one voice clip on the device, as the WAV the voice service
// takes (16 kHz, mono, 16-bit; src/wav.ts). Sources read 2026-10-28 in the
// installed code, not only the docs:
//
// - Android / iOS: expo-audio 58.0.10 useAudioStream (the version
//   expo/bundledNativeModules.json pins for SDK 58). It is asked for
//   16 kHz mono int16; startFileRecordingAsync({format: "wav"}) writes a
//   WAV with a live header (AudioStreamFileWriter). iOS converts to the asked
//   rate (AVAudioConverter in ios/AudioStream.swift); Android's AudioRecord
//   falls back to a supported rate if the device refuses 16 kHz
//   (android/.../AudioStream.kt), and the clip reports the rate it got. The
//   server's fallback converts such a clip (post/wav.go).
// - Web: useAudioStream is a stub there (build/AudioStream.web.js returns
//   stream: null). The web uses src/webvoice.ts: getUserMedia (https only)
//   + an AudioWorklet (public/voice-capture-worklet.js) at the device rate;
//   src/wav.ts resamples to 16 kHz and writes the WAV.
//
// No UI here: the mic button comes with its own mockup (style guide rule).
//
// X36a: both platforms use src/clipsession.ts. The automatic stop before the
// server's limit keeps the clip: the Stop tap after it gets the same clip,
// and opts.onAutoStop gets it at once. A second start while one runs throws
// (no second microphone), and unmount cancels the clip.
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Platform } from "react-native";
import { requestRecordingPermissionsAsync, useAudioStream } from "expo-audio";
import type { Upload } from "./api";
import { ClipBusy, ClipCancelled, ClipSession } from "./clipsession";
import { VOICE_MAX_MS, VOICE_RATE } from "./wav";
import { startWebCapture, VoiceError, type WebCapture } from "./webvoice";
import { MIC } from "./words";

export { VoiceError };
// start() / stop() reject with ClipCancelled when cancel() (or unmount) came first.
export { ClipCancelled };

export type VoiceClip = Upload & { ms: number; rate: number; source: "web" | "native" };

export type VoiceRecorder = {
  recording: boolean;
  start: () => Promise<void>;
  stop: () => Promise<VoiceClip>;
  cancel: () => void;
};

export type VoiceOptions = {
  // the clip of the automatic stop (VOICE_MAX_MS - 500 ms); stop() after it
  // returns the same clip.
  onAutoStop?: (clip: VoiceClip) => void;
};

// useSession is one ClipSession for the life of the component.
function useSession(opts?: VoiceOptions) {
  const [recording, setRecording] = useState(false);
  const auto = useRef(opts?.onAutoStop);
  auto.current = opts?.onAutoStop;
  const session = useMemo(
    () => new ClipSession<VoiceClip>(VOICE_MAX_MS - 500, setRecording, (c) => auto.current?.(c)),
    [],
  );
  useEffect(() => () => session.cancel(), [session]);
  return { session, recording };
}

const busy = (e: unknown) => {
  if (e instanceof ClipBusy) throw new VoiceError("failed", e.message);
  throw e;
};

function useNativeVoice(opts?: VoiceOptions): VoiceRecorder {
  const { stream } = useAudioStream({ sampleRate: VOICE_RATE, channels: 1, encoding: "int16" });
  const { session, recording } = useSession(opts);

  const start = useCallback(async () => {
    await session.start({
      begin: async () => {
        const perm = await requestRecordingPermissionsAsync();
        if (!perm.granted) throw new VoiceError("denied", MIC.allow);
        try {
          await stream.start();
          await stream.startFileRecordingAsync({ format: "wav" });
        } catch (e: any) {
          try { stream.stop(); } catch { /* already stopped */ }
          throw new VoiceError("failed", e?.message || MIC.failed);
        }
      },
      finish: async () => {
        try {
          const r = await stream.stopFileRecordingAsync();
          return { uri: r.uri, name: "voice.wav", type: "audio/wav", ms: Math.round(r.duration * 1000), rate: r.sampleRate, source: "native" };
        } finally {
          try { stream.stop(); } catch { /* already stopped */ }
        }
      },
      abort: () => { try { stream.stop(); } catch { /* already stopped */ } },
    }).catch(busy);
  }, [stream, session]);

  const stop = useCallback(() => session.stop(), [session]);
  const cancel = useCallback(() => session.cancel(), [session]);
  return { recording, start, stop, cancel };
}

function useWebVoice(opts?: VoiceOptions): VoiceRecorder {
  const { session, recording } = useSession(opts);

  const start = useCallback(async () => {
    let cap: WebCapture | null = null;
    await session.start({
      begin: async () => { cap = await startWebCapture(); },
      finish: async () => {
        const { wav, ms } = cap!.stop();
        const blob = new Blob([wav], { type: "audio/wav" });
        return { blob, uri: URL.createObjectURL(blob), name: "voice.wav", type: "audio/wav", ms, rate: VOICE_RATE, source: "web" };
      },
      abort: () => cap?.cancel(),
    }).catch(busy);
  }, [session]);

  const stop = useCallback(() => session.stop(), [session]);
  const cancel = useCallback(() => session.cancel(), [session]);
  return { recording, start, stop, cancel };
}

// The platform is fixed for the life of the app, so the hook order is stable.
export const useVoiceRecorder: (opts?: VoiceOptions) => VoiceRecorder = Platform.OS === "web" ? useWebVoice : useNativeVoice;
