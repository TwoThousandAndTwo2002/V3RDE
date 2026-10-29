// Rules of the new-listing flow (camera, scan, review, post), written new for V3RDE (NEW_WORK_UI.md, UI 14 and 15).
// Pure functions (no React, no network), so the tests run them directly.

/** The JPEG quality of a phone camera photo (kept quick; the upload is made again at UPLOAD.quality). */
export const SHOT_QUALITY = 0.7;

/** The app waits this long for the analyze route (api.analyze); the progress bar fills in this time. */
export const ANALYZE_WAIT_MS = 75_000;

/** The share of the progress bar after `ms` of analysis (0..1). */
export function analyzeShare(ms: number): number {
  return Math.min(1, Math.max(0, ms / ANALYZE_WAIT_MS));
}

/** Tenths of a second, counting up: "2.4 s". */
export function elapsedText(ms: number): string {
  return (Math.floor(Math.max(0, ms) / 100) / 10).toFixed(1) + " s";
}

/**
 * A barcode the camera read, as the catalog takes it: the digits of an EAN-8, UPC-A, EAN-13 or GTIN-14 code;
 * "" for anything else (it is not sent).
 */
export function barcodeOf(data: unknown): string {
  const digits = String(data ?? "").replace(/[^0-9]/g, "");
  return digits.length === 8 || digits.length === 12 || digits.length === 13 || digits.length === 14 ? digits : "";
}

/** The limits of a listing (the listings service checks the same: listings.Input.Validate on branch services). */
export const LIMITS = { title: 80, servingsMax: 99, pickup: 200 } as const;

/** The words of the form checks. */
export const FORM_WORDS = {
  title: "Give a title of 1 to 80 characters",
  servings: "Give servings from 1 to 99",
  pickup: "Give pickup details of 1 to 200 characters",
} as const;

/** The first problem of a listing before it goes to the services, or "". */
export function listingProblem(title: string, servings: string | number, pickup: string): string {
  const t = title.trim(), p = pickup.trim(), n = Number(servings);
  if (t.length === 0 || t.length > LIMITS.title) return FORM_WORDS.title;
  if (!Number.isInteger(n) || n < 1 || n > LIMITS.servingsMax) return FORM_WORDS.servings;
  if (p.length === 0 || p.length > LIMITS.pickup) return FORM_WORDS.pickup;
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
  if (barcode) return "From the barcode " + barcode;
  if (description) return "The scanner says: " + description;
  return "";
}
