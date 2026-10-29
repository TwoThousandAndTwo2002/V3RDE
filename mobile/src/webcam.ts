// Web camera at full size (X32d, T9 2026-10-27). expo-camera's web camera
// opens the stream with no size, so the browser's default is used (18 of 18
// app uploads were 480x640), and takePictureAsync draws THAT video frame.
// Here, after the camera opens, the track is asked for the largest size the
// camera reports (MediaStreamTrack.applyConstraints); at the shutter a still
// photo is taken with ImageCapture.takePhoto() where the browser has it
// (Chrome for Android; Safari on iOS 18.4+), else the (larger) video frame is
// drawn. The still is used only when it is turned like the preview.
// Web only; native keeps takePictureAsync.
import { sameTurn, streamTarget } from "./logic";

export type Still = { uri: string; blob: Blob; width: number; height: number; source: "photo" | "frame" };

// X32e (T9 2026-10-27: a delay at the shutter, and once an older photo): the
// preview's state at the press, sent with the photo and logged by the server.
// A video whose time does not move between presses, or a muted/ended track,
// means the preview was frozen.
export function videoState(v: HTMLVideoElement) {
  const t = track(v);
  return { video_t: Math.round(v.currentTime * 1000) / 1000, paused: v.paused, ready: v.readyState,
    vw: v.videoWidth, vh: v.videoHeight, track: t?.readyState ?? "none", muted: t?.muted ?? null };
}

// The <video> that expo-camera renders inside the camera box (react-native-web
// refs are DOM nodes).
export function videoIn(box: unknown): HTMLVideoElement | null {
  const el = box as { querySelector?: (s: string) => Element | null } | null;
  const v = el?.querySelector?.("video");
  return v instanceof HTMLVideoElement ? v : null;
}

function track(v: HTMLVideoElement): MediaStreamTrack | null {
  const s = v.srcObject as MediaStream | null;
  return s?.getVideoTracks?.()[0] ?? null;
}

// Ask for the camera's largest size; returns the size the track now has.
export async function raise(v: HTMLVideoElement): Promise<{ width: number; height: number } | null> {
  const t = track(v);
  if (!t) return null;
  try {
    const caps = typeof t.getCapabilities === "function" ? (t.getCapabilities() as any) : null;
    await t.applyConstraints(streamTarget(caps) as MediaTrackConstraints);
  } catch {
    // keep the stream as it is
  }
  const s = t.getSettings();
  return { width: s.width ?? v.videoWidth, height: s.height ?? v.videoHeight };
}

function frame(v: HTMLVideoElement, quality: number): Promise<Still> {
  const c = document.createElement("canvas");
  c.width = v.videoWidth; c.height = v.videoHeight;
  const ctx = c.getContext("2d", { alpha: false });
  if (!ctx) return Promise.reject(new Error("no canvas"));
  ctx.drawImage(v, 0, 0, c.width, c.height);
  return new Promise((ok, bad) => c.toBlob((b) => b ? ok({ uri: URL.createObjectURL(b), blob: b, width: c.width, height: c.height, source: "frame" })
    : bad(new Error("no blob")), "image/jpeg", quality));
}

async function size(blob: Blob): Promise<{ width: number; height: number }> {
  const b = await createImageBitmap(blob); // decodes with the file's orientation
  const out = { width: b.width, height: b.height };
  b.close();
  return out;
}

// X32f (T9 2026-10-27: "user should not be waiting for confirmation"): the
// preview stops at the press, before anything else runs.
export function freeze(v: HTMLVideoElement) { try { v.pause(); } catch { /* keep going */ } }
export function unfreeze(v: HTMLVideoElement | null) { try { v?.play(); } catch { /* keep going */ } }

// X32f: one decode and one encode to the upload size (X32e measured 10.4 s
// for prepare() on a 3000x4000 photo on T9's phone: the image library decodes
// and encodes twice). createImageBitmap applies the file's orientation.
export async function shrink(blob: Blob, maxEdge: number, quality: number): Promise<{ blob: Blob; width: number; height: number }> {
  const b = await createImageBitmap(blob);
  const scale = Math.min(1, maxEdge / Math.max(b.width, b.height));
  const c = document.createElement("canvas");
  c.width = Math.round(b.width * scale); c.height = Math.round(b.height * scale);
  const ctx = c.getContext("2d", { alpha: false });
  if (!ctx) { b.close(); throw new Error("no canvas"); }
  ctx.imageSmoothingEnabled = true; ctx.imageSmoothingQuality = "high";
  ctx.drawImage(b, 0, 0, c.width, c.height);
  b.close();
  const out: Blob = await new Promise((ok, bad) => c.toBlob((x) => (x ? ok(x) : bad(new Error("no blob"))), "image/jpeg", quality));
  return { blob: out, width: c.width, height: c.height };
}

// The photo for the shutter (and for a barcode read).
export async function still(v: HTMLVideoElement, quality = 0.92): Promise<Still> {
  const t = track(v);
  const IC = (globalThis as any).ImageCapture;
  if (t && typeof IC === "function") {
    try {
      const blob: Blob = await new IC(t).takePhoto();
      const s = await size(blob);
      if (sameTurn(s.width, s.height, v.videoWidth, v.videoHeight)) {
        return { uri: URL.createObjectURL(blob), blob, width: s.width, height: s.height, source: "photo" };
      }
    } catch {
      // fall back to the video frame
    }
  }
  return frame(v, quality);
}
