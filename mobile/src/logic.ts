// V3RDE app logic: pure functions with no React and no network, so the tests
// run them directly with `node --test` (Node type stripping).
//
// Two kinds of code (NEW_WORK_UI.md, UI 4; docs/sources/U4.md):
//   CARRIED  blocks first written during the Hackathon (entries 10, 12, 15), checked
//            line by line against the audit (git blame -M -C -C -C, no old line).
//   NEW      functions written for V3RDE in place of the earlier app's helpers.

// ---- CARRIED (entries 10 and 15): the analyze answer, the label report, the words of the review list
// X32 (T9, 2026-10-27): one route, /api/analyze. A 200 is {request_id,
// source, model, latency_ms, count, items: [{item, fields, photo_id}]}; the
// same shape for one item and for many. Each item becomes one card in the
// review list (or the one-item form); servings = the count of identical
// items, clamped to the form's 1-99.
export type ItemForm = {
  item: number; title: string; servings: string; photo_id: string; description: string; err: string;
  details?: ItemDetails; // X48d: the scanner's other fields, kept with the listing
};
/** X48d: the scanner's fields of an item besides the title and the servings (listings/details.go). */
export type ItemDetails = {
  description?: string; is_non_veg?: 0 | 1; serving_type?: "individual" | "bulk";
  meal_condition?: "ready_to_eat" | "cooking_required" | "sealed"; weight?: number; meal_cost?: number;
};
/** The details of one answer item: only the values the listings service takes (a wrong one is left out, so a post
 *  never fails on them); undefined when there are none. */
export function itemDetails(f: any): ItemDetails | undefined {
  if (!f || typeof f !== "object") return undefined;
  const d: ItemDetails = {};
  // at most 200 characters (code points, as the service counts them), no control characters
  const desc = Array.from(typeof f.description === "string" ? f.description.replace(/[\u0000-\u001f\u007f]/g, " ").trim() : "");
  if (desc.length > 0) d.description = desc.slice(0, 200).join("").trim();
  if (f.is_non_veg === 0 || f.is_non_veg === 1) d.is_non_veg = f.is_non_veg;
  if (f.serving_type === "individual" || f.serving_type === "bulk") d.serving_type = f.serving_type;
  if (["ready_to_eat", "cooking_required", "sealed"].includes(f.meal_condition)) d.meal_condition = f.meal_condition;
  const num = (v: unknown, max: number) => typeof v === "number" && Number.isFinite(v) && v >= 0 && v <= max;
  if (num(f.weight, 1000)) d.weight = f.weight;
  if (num(f.meal_cost, 100_000)) d.meal_cost = f.meal_cost;
  return Object.keys(d).length > 0 ? d : undefined;
}
export function answerToItems(resp: any): ItemForm[] {
  const items = Array.isArray(resp?.items) ? resp.items : [];
  return items.map((it: any, i: number) => {
    const f = it?.fields ?? {};
    const n = Number(f.number_of_serving);
    return {
      item: Number.isFinite(it?.item) ? it.item : i + 1,
      title: String(f.name ?? "").slice(0, 80),
      servings: String(Number.isFinite(n) && n >= 1 ? Math.min(99, Math.floor(n)) : 1),
      photo_id: typeof it?.photo_id === "string" ? it.photo_id : "",
      description: String(f.description ?? ""),
      err: "",
      details: itemDetails(f),
    };
  });
}

// X32g (T9 2026-10-27: "logging the corrected labels"): what the user did
// with an answer -- for every item of the answer, the model's title and
// servings, the user's final ones, and whether it was kept -- sent to
// /api/analyze/labels once per answer and logged next to it. `final` holds
// the items as the user left them (removed ones are missing).
export type LabelOutcome = "posted" | "retake" | "closed";
export function labelReport(requestId: string, answer: ItemForm[], final: ItemForm[], outcome: LabelOutcome, locale = "") {
  const byItem = new Map(final.map((f) => [f.item, f]));
  const num = (s: string) => { const n = Math.floor(Number(s)); return Number.isFinite(n) ? Math.min(99, Math.max(0, n)) : 0; };
  return {
    request_id: requestId, outcome, locale,
    items: answer.map((a) => {
      const f = outcome === "posted" ? byItem.get(a.item) : undefined;
      return { item: a.item, photo_id: a.photo_id, model_title: a.title, final_title: f ? f.title.trim() : a.title,
        model_servings: num(a.servings), final_servings: f ? num(f.servings) : num(a.servings), kept: !!f };
    }),
  };
}

// The device's language tag (en-US, es-MX…), "" when unknown (X32g).
export function deviceLocale(get: () => string = () => Intl.DateTimeFormat().resolvedOptions().locale): string {
  try { const l = get(); return /^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8}){0,3}$/.test(l) ? l : ""; } catch { return ""; }
}

// X32: every error answer has a fixed code; the code picks the sheet
// (approved mockup, docs/icarus_x32_mockup_2026-10-27.md).
export type AnalyzeKind = "not_food" | "nofood_single" | "nofood_multi" | "already_listed" | "daily_cap" | "unavailable" | "retry";
export function analyzeKind(body: any): AnalyzeKind {
  switch (body?.code) {
    case "not_food": return "not_food";
    case "model_no_food_single": return "nofood_single";
    case "model_no_food_multi": return "nofood_multi";
    case "already_listed": return "already_listed";
    case "daily_cap_reached": return "daily_cap";
    case "caller_daily_cap_reached": return "daily_cap"; // this user's share of the day (X44a): the same sheet
    case "gate_unavailable": return "unavailable";
  }
  return "retry";
}

// U18: the scan job (branch services, inference/scanjob.go). GET /api/analyze/scan/{id} answers
// {status: "running" | "done" | "failed", http_status?, answer?}; the answer of a finished job is the body the
// one-request route gives (200, or an error body with its code).
export const SCAN_POLL_MS = 1000; // the service asks for about one poll a second
export const SCAN_WAIT_MS = 75_000;
export const SCAN_HOLD_MS = 8000; // X48c: the service holds one poll until the job changes (at most 10 s)

/** The next poll of a scan job (X48c). The first poll: a plain GET at once (the 202 has no version). A job with a
 *  version: a held poll at once (since = that version), so each change comes back as it happens. A service with no
 *  version (before X48c), or a lost answer: wait SCAN_POLL_MS, then a plain GET. */
export function scanPoll(path: string, job: any, first = false): { path: string; sleepMs: number; timeoutMs: number } {
  if (first) return { path, sleepMs: 0, timeoutMs: 10_000 };
  const v = job && typeof job === "object" ? job.version : undefined;
  if (typeof v === "number" && Number.isInteger(v) && v >= 0) {
    return { path: `${path}?since=${v}&wait=${SCAN_HOLD_MS}`, sleepMs: 0, timeoutMs: SCAN_HOLD_MS + 10_000 };
  }
  return { path, sleepMs: SCAN_POLL_MS, timeoutMs: 10_000 };
}
export type ScanStep = { kind: "wait" } | { kind: "answer"; body: any } | { kind: "error"; status: number; body: any };
export function scanStep(job: any): ScanStep {
  if (!job || typeof job !== "object" || job.status === "running" || job.http_status === undefined) return { kind: "wait" };
  const status = Number(job.http_status);
  if (status === 200 && job.status === "done") return { kind: "answer", body: job.answer };
  return { kind: "error", status: status >= 400 ? status : 502, body: job.answer ?? null };
}

// The ErrorText under "Try again?" for the retry answers; the photo is kept.
export function retryText(status: number, body: any): string {
  if (body?.code === "busy") return "The scanner is busy. Wait a few seconds — your photo is kept.";
  if (body?.code === "rate_limited" || status === 429) return "Too many photos this minute. Wait a minute — your photo is kept.";
  if (body?.code === "gate_unavailable") return "The food check is not available right now — your photo is kept.";
  if (body?.code === "timeout") return "This photo took too long. Try a closer photo of fewer items — your photo is kept.";
  if (status === 0 || status >= 500) return "The food scanner is busy, try again — your photo is kept.";
  return (typeof body?.error === "string" && body.error ? body.error : `Request failed (${status})`) + " — your photo is kept.";
}

// The words of the review list (approved mockup).
export function plural(n: number, one: string, many: string): string {
  return n === 1 ? `1 ${one}` : `${n} ${many}`;
}
export function foundTitle(n: number): string { return plural(n, "item found", "items found"); }
export function postTitle(n: number): string { return "Post " + plural(n, "listing", "listings"); }
export function postingText(i: number, n: number): string { return `Posting ${i} of ${n}…`; }
export function notPostedTitle(left: number): string { return plural(left, "item not posted", "items not posted"); }
export function postedSummary(posted: number, left: number): string {
  return `${plural(posted, "listing", "listings")} posted. ${left === 1 ? "1 item is" : left + " items are"} kept here; try again.`;
}

// ---- NEW: the path of an item photo the analyze route cut (owner only, 32 hex characters).
export function itemPhotoPath(photoId: string): string {
  const ok = typeof photoId === "string" && /^[0-9a-f]{32}$/.test(photoId);
  return ok ? `/api/analyze/photo/${photoId}` : "";
}

// ---- CARRIED (entry 12): the upload size, the web camera size, the turn of a still photo
// X32 (D28, X30b): the app uploads at Muse's image limit, about 2376 px on
// the long side (4,096 image tokens at 16:9) at JPEG 0.9, in place of 1600 px
// at 0.85: the counts and small labels hold, and the server's item crops
// get more pixels. About 1 MB; the server limit is 5 MB.
export const UPLOAD = { maxEdge: 2376, quality: 0.9 };

// X32d (T9 2026-10-27): the web camera opens at the browser's default size
// (18 of 18 app uploads were 480x640). After it opens, ask the track for a
// bigger size. X32f: the camera's size shrunk to the upload size (maxEdge,
// aspect kept), not the sensor's (a 3000x4000 preview made the phone slow;
// the still photo is full size anyway); no capabilities: maxEdge wide.
export type SizeCaps = { width?: { max?: number }; height?: { max?: number } };
export function streamTarget(caps?: SizeCaps | null, maxEdge = UPLOAD.maxEdge) {
  const w = Number(caps?.width?.max), h = Number(caps?.height?.max);
  if (!(w > 0 && h > 0)) return { width: { ideal: maxEdge }, height: { ideal: Math.round(maxEdge * 3 / 4) } };
  const t = targetSize(w, h, maxEdge);
  return { width: { ideal: t.width }, height: { ideal: t.height } };
}

// A still photo is used only when it is turned the same way as the preview
// (both portrait or both landscape); else the preview frame is used, so a
// photo is never sideways.
export function sameTurn(aW: number, aH: number, bW: number, bH: number) {
  if (!(aW > 0 && aH > 0 && bW > 0 && bH > 0)) return false;
  return aW >= aH === bW >= bH;
}

// ---- NEW: the size a photo is scaled to. The long side is at most `maxEdge`,
// the shape stays the same, and a photo is never made larger. A size that is
// not positive gives 0 x 0.
export function targetSize(width: number, height: number, maxEdge = 1600): { width: number; height: number } {
  if (!(width > 0 && height > 0)) return { width: 0, height: 0 };
  const longSide = width >= height ? width : height;
  const k = longSide > maxEdge ? maxEdge / longSide : 1;
  return { width: Math.round(width * k), height: Math.round(height * k) };
}

// ---- NEW: the address of an API path. On the web the base is "" (the same
// origin as the app); on a phone it is an absolute origin. A path that is
// already absolute is kept.
export const joinUrl = (base: string, path: string): string => {
  if (path.startsWith("http://") || path.startsWith("https://")) return path;
  let origin = base;
  while (origin.endsWith("/")) origin = origin.slice(0, -1);
  return origin + (path.charAt(0) === "/" ? path : "/" + path);
};

// ---- NEW: the line the app shows for a failed request. The service's own
// text ({error: "..."}) wins; the analyze screen picks its sheet from the
// answer's code (analyzeKind), not from this line.
const FIXED_TEXT: Record<number, string> = {
  401: "Please log in again",
  429: "Too many requests, wait a minute",
};
const GATEWAY = [502, 503, 504];
export function errorMessage(status: number, body: unknown, path = ""): string {
  const said = body !== null && typeof body === "object" ? (body as { error?: unknown }).error : undefined;
  if (typeof said === "string") return said;
  if (FIXED_TEXT[status]) return FIXED_TEXT[status];
  if (GATEWAY.includes(status)) {
    return path.includes("/analyze") ? "The food scanner is busy, try again" : "The service is busy, try again";
  }
  return "Request failed (" + status + ")";
}
