// Rules of the new-listing flow (camera, scan, review, post), written new for V3RDE (NEW_WORK_UI.md, UI 14 and 15).
// Pure functions (no React, no network), so the tests run them directly.

/** The JPEG quality of a phone camera photo (kept quick; the upload is made again at UPLOAD.quality). */
import { SOURCE_NOTE, UNITS } from "./words";
export const SHOT_QUALITY = 0.7;

/** The app waits this long for the analyze route (api.analyze); the progress bar fills in this time. */
export const ANALYZE_WAIT_MS = 75_000;

/** The share of the progress bar after `ms` of analysis (0..1). */
export function analyzeShare(ms: number): number {
  return Math.min(1, Math.max(0, ms / ANALYZE_WAIT_MS));
}

/** Tenths of a second, counting up: "2.4 s". */
export function elapsedText(ms: number): string {
  return UNITS.seconds((Math.floor(Math.max(0, ms) / 100) / 10).toFixed(1));
}

/**
 * A barcode the camera read, as the catalog takes it: the digits of an EAN-8, UPC-A, EAN-13 or GTIN-14 code;
 * "" for anything else (it is not sent).
 */
export function barcodeOf(data: unknown): string {
  const digits = String(data ?? "").replace(/[^0-9]/g, "");
  return digits.length === 8 || digits.length === 12 || digits.length === 13 || digits.length === 14 ? digits : "";
}

/** I1: one code the app read in the still photo and its box, in the pixels of the still (fw x fh); the
 *  "barcodes" field of /api/analyze (inference/barcodes.go CodeBox). */
export type FoundCode = { code: string; x: number; y: number; w: number; h: number; fw: number; fh: number };
/** I1: the most codes one scan sends (inference MaxBarcodes). */
export const MAX_CODES = 24;
/** A code the reader found (expo-camera BarcodeScanningResult: data, bounds). */
type ReadCode = { data?: unknown; bounds?: { origin?: { x?: number; y?: number }; size?: { width?: number; height?: number } } };

/** I1: the retail codes of a still photo, once each, at most MAX_CODES; a box that is not inside the
 *  still (or a still with no size) is sent with no box, so the service does not match it to an item. */
export function codesFound(found: readonly ReadCode[], fw: number, fh: number): FoundCode[] {
  const out: FoundCode[] = [];
  for (const f of found) {
    const code = barcodeOf(f?.data);
    if (!code || out.some((o) => o.code === code)) continue;
    const x = Math.round(f.bounds?.origin?.x ?? -1), y = Math.round(f.bounds?.origin?.y ?? -1);
    const w = Math.round(f.bounds?.size?.width ?? 0), h = Math.round(f.bounds?.size?.height ?? 0);
    const inside = fw > 0 && fh > 0 && x >= 0 && y >= 0 && w > 0 && h > 0 && x + w <= fw && y + h <= fh;
    out.push(inside ? { code, x, y, w, h, fw: Math.round(fw), fh: Math.round(fh) } : { code, x: 0, y: 0, w: 0, h: 0, fw: 0, fh: 0 });
    if (out.length === MAX_CODES) break;
  }
  return out;
}

/** I1: what the scan sends: the live code as today, and the codes of the still when there are two or more
 *  different codes in all, or no live code and a code of the still with its box (a plate with one packet:
 *  the scanner runs too). One live code alone is today's fast path. `shown` is the number of different codes. */
export function codesToSend(live: string, still: readonly FoundCode[]): { barcode: string; barcodes: FoundCode[]; shown: number } {
  const all = new Set([...(live ? [live] : []), ...still.map((c) => c.code)]);
  const send = all.size >= 2 || (!live && still.some((c) => c.fw > 0));
  return { barcode: live, barcodes: send ? [...still] : [], shown: all.size };
}

/** I1 review: the read of a still waits at most this long (a slow reader must not hold "Use photo"). */
export const READ_WAIT_MS = 2000;

/** The limits of a listing (the listings service checks the same: listings.Input.Validate on branch services). */
export const LIMITS = { title: 80, servingsMax: 99, pickup: 200, weightMax: 1000 } as const; // X49: weight in kg (listings/details.go)

/** The words of the form checks. */
export const FORM_WORDS = {
  title: "Give a title of 1 to 80 characters",
  servings: "Give servings from 1 to 99",
  pickup: "Give pickup details of 1 to 200 characters",
  weight: "Give a weight from 0 to 1000 kg",
} as const;

/** The first problem of a listing before it goes to the services, or "". */
export function listingProblem(title: string, servings: string | number, pickup: string, pickupOptional = false): string {
  const t = title.trim(), p = pickup.trim(), n = Number(servings);
  if (t.length === 0 || t.length > LIMITS.title) return FORM_WORDS.title;
  if (!Number.isInteger(n) || n < 1 || n > LIMITS.servingsMax) return FORM_WORDS.servings;
  // X49j: with a chosen address the pickup text is only the instructions, and they may be empty
  if ((p.length === 0 && !pickupOptional) || p.length > LIMITS.pickup) return FORM_WORDS.pickup;
  return "";
}

/** Servings after − or + (whole numbers, 1 to 99; a field that is not a number counts as 1). */
export function stepServings(current: string, by: number): string {
  const n = Math.floor(Number(current)) || 1;
  return String(Math.min(LIMITS.servingsMax, Math.max(1, n + by)));
}

/** The servings field keeps only digits (2 at most). */
export function servingsInput(text: string): string {
  return text.replace(/[^0-9]/g, "").slice(0, 2);
}

/** The line under the title of the one-item form: where the title came from. */
export function sourceNote(barcode: string, description: string): string {
  if (barcode) return SOURCE_NOTE.barcode(barcode);
  if (description) return SOURCE_NOTE.scanner(description);
  return "";
}

/** X49: the weight field (kg) keeps digits and one decimal point (7 characters at most). */
export function weightInput(text: string): string {
  const t = text.replace(",", ".").replace(/[^0-9.]/g, "");
  const dot = t.indexOf(".");
  return (dot < 0 ? t : t.slice(0, dot + 1) + t.slice(dot + 1).replace(/\./g, "")).slice(0, 7);
}

/** X49: the problem of a weight (kg) the user typed, or "". An empty field is no weight (no problem). */
export function weightProblem(text: string): string {
  const t = text.trim();
  if (t === "") return "";
  const n = Number(t);
  return /^\d*\.?\d*$/.test(t) && t !== "." && Number.isFinite(n) && n >= 0 && n <= LIMITS.weightMax ? "" : FORM_WORDS.weight;
}

/** X49x: the word tables of this file (src/lang.ts). */
export const CAPTURE_TABLES = { FORM_WORDS } as unknown as Record<string, Record<string, unknown>>;
