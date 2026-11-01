// V3RDE API client, written new for V3RDE (NEW_WORK_UI.md, UI 4; docs/sources/U4.md).
// The routes and fields are those of the services on branch `services`:
// auth (/api/auth), listings (/api/listings), analyze (/api/analyze) and voice (/api/voice).
//
// The web app is served from the same origin as the API, so its paths stay
// relative and the session cookie goes with each request. A phone build calls
// EXPO_PUBLIC_API_BASE (mobile/.env.local, not committed, or an EAS variable).
// Photo uploads from a phone are React Native FormData parts ({uri, name, type});
// mobile/.env keeps React Native's fetch as the global fetch for them (U2 D5).
import { Platform as RN } from "react-native";
import { CLIENT, SCAN_WAIT_MS, SEND_AGAIN_MS, errorMessage, joinUrl, scanPoll, scanStep, sendAgain } from "./logic";
import { placeFields, placeQuery, type Place } from "./place";
import { RESCUE_POLL_MS, RESCUE_PUSH_POLL_MS, RESCUE_WAIT_MS, type Rescue } from "./rescue";
import { pollMs, pushNow, pushWait, type Ticket } from "./push";

export const API_BASE: string = RN.OS === "web" ? "" : (process.env.EXPO_PUBLIC_API_BASE ?? "");

// A tunnel host on a free plan shows a warning page unless the request has this
// header; other hosts ignore it.
const TUNNEL_HEADERS: Record<string, string> = { "ngrok-skip-browser-warning": "1" };

/** A failed request: status 0 = no answer (network or time limit). `body` is the JSON answer, if any. */
class ApiError extends Error {
  readonly status: number;
  readonly body: any;
  constructor(status: number, message: string, body: any = null) {
    super(message);
    this.status = status;
    this.body = body;
  }
}
export { ApiError };

/** A file to upload: a Blob on the web; a file URI with its name and type on a phone. */
export interface Upload { blob?: Blob; uri: string; name: string; type: string }
export interface User { id: string; email: string }
/** A listing as the listings service sends it. */
export type Listing = {
  id: string; title: string; number_of_serving: number; pickup_text: string; status: string;
  expires_at: string; photo_url: string; owner_id?: string; collected_by?: string;
  /** X49e: miles from the place of the request (only when the request had a place). */
  distance_mi?: number;
  /** X49j: the address the giver chose; only for the owner and for the user who grabbed it. */
  pickup_address?: { text: string; zip?: string; lat: number; lng: number };
};
/** A new listing: the photo is an item photo of the analyze route (photo_id) or the photo itself. */
export type NewListing = {
  title: string; number_of_serving: number; pickup_text: string; request_id?: string; photo?: Upload; photo_id?: string;
  details?: object; // X48d: the scanner's fields (logic.itemDetails), kept with the listing
  place: Place; // X49e: the pickup place (the server keeps it rounded to 0.01 mi and never sends it)
  /** X49j: the address the giver chose (its point is `place`); then pickup_text is only the instructions. */
  address?: { text: string };
};
/** One item of the review list, in the listing contract (voice posting). */
export type VoiceItem = { item: number; title: string; number_of_serving: number };

type Options = { json?: unknown; form?: FormData; timeoutMs?: number };

async function request<T>(method: string, path: string, opt: Options = {}): Promise<T> {
  const abort = new AbortController();
  const timer = setTimeout(() => abort.abort(), opt.timeoutMs ?? 20_000);
  const headers: Record<string, string> = { ...TUNNEL_HEADERS };
  let payload: BodyInit | undefined;
  if (opt.json !== undefined) {
    headers["Content-Type"] = "application/json";
    payload = JSON.stringify(opt.json);
  } else if (opt.form) {
    payload = opt.form;
  }
  let res: Response;
  try {
    res = await fetch(joinUrl(API_BASE, path), { method, headers, body: payload, credentials: "include", signal: abort.signal });
  } catch (err) {
    const timedOut = (err as { name?: string } | null)?.name === "AbortError";
    throw new ApiError(0, timedOut ? "Timed out, try again" : "Network error, check the connection",
      { code: timedOut ? CLIENT.timeout : CLIENT.offline });
  } finally {
    clearTimeout(timer);
  }
  const isJson = (res.headers.get("Content-Type") ?? "").includes("application/json");
  const data: any = isJson ? await res.json().catch(() => null) : null;
  if (!res.ok) throw new ApiError(res.status, errorMessage(res.status, data, path), data);
  return data as T; // the JSON answer (null when the answer has no JSON)
}

/** Adds a file part: the Blob on the web, the React Native {uri, name, type} part on a phone. */
function addFile(form: FormData, field: string, file: Upload): void {
  if (RN.OS === "web" && file.blob) {
    form.append(field, file.blob, file.name);
    return;
  }
  form.append(field, { uri: file.uri, name: file.name, type: file.type } as unknown as Blob);
}

/** A form with the given text fields; a field whose value is `undefined` or "" is left out only when it is optional. */
function formOf(required: Array<[string, string]>, optional: Array<[string, string | undefined]> = []): FormData {
  const form = new FormData(); // text fields first, then the file part
  for (const [name, value] of required) form.append(name, value);
  for (const [name, value] of optional) if (value) form.append(name, value);
  return form;
}

export const auth = Object.freeze({
  me: () => request<User>("GET", "/api/auth/me"),
  signup: (email: string, password: string) => request<User>("POST", "/api/auth/signup", { json: { email, password } }),
  login: (email: string, password: string) => request<User>("POST", "/api/auth/login", { json: { email, password } }),
  logout: () => request<unknown>("POST", "/api/auth/logout"),
  /** WS1: a one-time ticket for the push socket; null when the box has push off (404). */
  pushTicket: () => request<Ticket>("POST", "/api/push/ticket").catch((err) => {
    if (err instanceof ApiError && err.status === 404) return null;
    throw err;
  }),
  /** Asks for a reset link by email (202 also for an unknown email). */
  resetRequest: (email: string) => request<unknown>("POST", "/api/auth/reset", { json: { email } }),
  /** Sets a new password with the token of the link; signs in (200 user + session). */
  resetConfirm: (token: string, password: string) => request<User>("POST", "/api/auth/reset/confirm", { json: { token, password } }),
});

export const listings = Object.freeze({
  /** The live listings, one page at a time ("after" = the cursor of the page before). */
  active(after = "") {
    const query = after ? "?after=" + encodeURIComponent(after) : "";
    return request<{ items: Listing[]; next: string }>("GET", "/api/listings" + query);
  },
  /** X49e: the live listings within 25 mi of the place, nearest first, with distance_mi. */
  near: (place: Place | null, after = "") => request<{ items: Listing[]; next: string }>("GET", "/api/listings" + placeQuery(place, after)),
  mine: () => request<{ items: Listing[] }>("GET", "/api/listings/mine"),
  /** X49j: the app config (the Maps browser key; "" = no map). */
  config: () => request<{ maps_key?: string }>("GET", "/api/listings/config"),
  /** X49e: the user's grabs whose timer still runs ("Your pickup"). */
  grabbed: (place: Place | null) => request<{ items: Listing[] }>("GET", "/api/listings/grabbed" + placeQuery(place)),
  grab: (id: string) => request<Listing>("POST", "/api/listings/" + encodeURIComponent(id) + "/grab"),
  /** X49e: gives the grab back while the timer runs (409 when it ended). */
  release: (id: string) => request<Listing>("POST", "/api/listings/" + encodeURIComponent(id) + "/release"),
  /** X48t: the owner deletes a listing nobody has grabbed (204; 409 when it was grabbed). */
  remove: (id: string) => request<null>("DELETE", "/api/listings/" + encodeURIComponent(id)),
  create(n: NewListing) {
    const form = formOf([["title", n.title], ["number_of_serving", String(n.number_of_serving)],
      ["pickup_text", n.pickup_text], ...placeFields(n.place)], [["request_id", n.request_id]]);
    if (n.details && Object.keys(n.details).length > 0) form.append("details", JSON.stringify(n.details));
    if (n.address) form.append("address", n.address.text); // X49j; X49q: no ZIP
    if (n.photo_id) form.append("photo_id", n.photo_id);
    else if (n.photo) addFile(form, "photo", n.photo);
    return request<Listing>("POST", "/api/listings", { form, timeoutMs: 30_000 });
  },
});

/** The one analysis route (session cookie): the photo, the barcode read on the phone (optional), and how the
 *  photo was taken (the server logs at most 512 bytes of it). The scan runs as a job (?async=1): the POST answers
 *  202 with a scan_id, then the app asks for the job until it is finished (logic.scanStep): a held poll that the
 *  service answers when the job changes (X48c, logic.scanPoll), or once a second on an older service. A long scan
 *  thus never meets the time limit of one request. The service stops a photo at 25 s; the app waits 75 s in all.
 *  `stop` is true when the user went back: the app stops asking. */
export async function analyze(photo: Upload, barcode = "", capture?: Record<string, unknown>, stop: () => boolean = () => false) {
  const form = formOf([], [["barcode", barcode]]);
  addFile(form, "image", photo);
  if (capture && Object.keys(capture).length > 0) form.append("capture", JSON.stringify(capture).slice(0, 512));
  const until = Date.now() + SCAN_WAIT_MS;
  // the upload is inside the 75 s; a service with no job route answers here, with the whole scan.
  // X49r: an upload that found no connection is sent again (SEND_TRIES in all); one that timed out is not.
  // The service gives a photo it already has (same user, same bytes) its running job: no second paid scan.
  let started: any;
  for (let n = 1; ; n++) {
    try {
      started = await request<any>("POST", "/api/analyze?async=1", { form, timeoutMs: Math.max(1_000, until - Date.now()) });
      break;
    } catch (err) {
      if (!(err instanceof ApiError && sendAgain(err.body?.code, n, Date.now(), until, stop()))) throw err;
      await new Promise((go) => setTimeout(go, SEND_AGAIN_MS));
      if (stop()) throw new ApiError(0, "Stopped"); // the user went back while it waited
    }
  }
  if (typeof started?.scan_id !== "string") return started; // a service without the job route answers at once
  const path = "/api/analyze/scan/" + encodeURIComponent(started.scan_id);
  let job: unknown = null;
  for (let first = true; ; first = false) {
    const next = scanPoll(path, job, first);
    if (next.sleepMs > 0) await new Promise((go) => setTimeout(go, next.sleepMs));
    if (stop()) throw new ApiError(0, "Stopped");
    if (Date.now() > until) throw new ApiError(0, "Timed out, try again", { code: CLIENT.timeout });
    try {
      job = await request<unknown>("GET", next.path, { timeoutMs: next.timeoutMs });
    } catch (err) {
      if (err instanceof ApiError && err.status === 0) {
        job = null; // one lost answer: ask again after a second
        continue;
      }
      // 404: the job is gone (the service started again); the user tries again with the photo that is kept
      if (err instanceof ApiError && err.status === 404) throw new ApiError(0, "Try again", { code: CLIENT.lost });
      throw err; // 401: signed out
    }
    const step = scanStep(job);
    if (step.kind === "answer") return step.body;
    if (step.kind === "error") throw new ApiError(step.status, errorMessage(step.status, step.body, "/api/analyze"), step.body);
  }
}

/** R5: the rescue of an expired listing (post/rescue.go). start() answers 202 with the job (or 200 with the
 *  rescue the listing already has); wait() asks once a second until the job is done or failed. A body with a
 *  place (the giver's location) only when the listing has no pickup place (X49q: no ZIP; the service answers 400). */
export const rescue = Object.freeze({
  start: (listingId: string, at: Place | null = null) => request<Rescue>("POST", "/api/rescue/" + encodeURIComponent(listingId) + "?async=1",
    at ? { json: { lat: at.lat, lng: at.lng } } : {}),
  get: (id: string) => request<Rescue>("GET", "/api/rescue/" + encodeURIComponent(id)),
  async wait(first: Rescue, stop: () => boolean = () => false): Promise<Rescue> {
    const until = Date.now() + RESCUE_WAIT_MS;
    let r = first;
    let asked = pushNow(); // WS1: a message that came since the last read of the job also ends the wait
    while (r.status === "running") {
      const id = r.rescue_id; // WS1: a push message for this rescue ends the wait at once
      await pushWait(pollMs(RESCUE_POLL_MS, RESCUE_PUSH_POLL_MS), (m) => m.kind === "rescue" && m.id === id, asked);
      asked = pushNow();
      if (stop()) throw new ApiError(0, "Stopped");
      if (Date.now() > until) throw new ApiError(0, "Timed out, try again");
      try {
        r = await rescue.get(r.rescue_id);
      } catch (err) {
        if (err instanceof ApiError && err.status === 0) continue; // one lost answer: ask again
        throw err;
      }
    }
    return r;
  },
});

/** A touch-up job (branch services, post/touchup.go): running, then done (photo_id = the edit), kept_original
 *  (the check found a change to the food) or failed. */
export type TouchupJob = { touchup_id: string; status: string; mode?: string; photo_id?: string; reason?: string };

/** Touch-up of an item photo of the scan (session cookie). The edit runs as a job; the original stays. */
export const touchup = Object.freeze({
  // X48r: one touch-up; the service gives every mode the auto touch-up (X48q), "background" names it
  start: (photoId: string) =>
    request<TouchupJob>("POST", "/api/touchup?mode=background&photo_id=" + encodeURIComponent(photoId)),
  get: (id: string) => request<TouchupJob>("GET", "/api/touchup/" + encodeURIComponent(id)),
  /** The path of the edited photo (status done only). */
  photoPath: (id: string) => "/api/touchup/" + encodeURIComponent(id) + "/photo",
});

/** The user's corrections to an answer (logic.labelReport). Best effort: a failed report never stops a post. */
export function analyzeLabels(report: object): Promise<void> {
  return request<void>("POST", "/api/analyze/labels", { json: report, timeoutMs: 10_000 }).catch(() => undefined);
}

/** Voice posting: one WAV clip, or (X48s) a typed chat line, and the review list (item, title, servings). The
 *  answer is a proposal; the app posts nothing until the donor taps Post. The service stops at 25 s; the app waits 35 s. */
export function voice(said: Upload | { text: string }, items: VoiceItem[], pickupText = "", lang = "") {
  const form = formOf([], [["pickup_text", pickupText], ["lang", lang]]);
  if ("text" in said) form.append("text", said.text);
  else addFile(form, "audio", said);
  if (items.length > 0) {
    const list = items.map((it) => ({ item: it.item, title: it.title, number_of_serving: it.number_of_serving }));
    form.append("items", JSON.stringify(list));
  }
  return request<any>("POST", "/api/voice", { form, timeoutMs: 35_000 });
}

// A photo route needs the session cookie. On the web an <img> sends it (same origin). The image loader of a
// phone does not send it, so on a phone the app fetches the photo itself and gives <Image> a data URI. One
// fetch per path for the session; a failed fetch is tried again on the next render.
const photoByPath = new Map<string, Promise<string>>();

export const photoUrl = (path: string): Promise<string> => {
  if (RN.OS === "web") return Promise.resolve(joinUrl(API_BASE, path)); // the <img> sends the cookie
  const known = photoByPath.get(path);
  if (known) return known;
  const load = fetch(joinUrl(API_BASE, path), { headers: TUNNEL_HEADERS, credentials: "include" })
    .then(async (res) => {
      if (!res.ok) throw new ApiError(res.status, "photo " + res.status);
      const blob = await res.blob();
      return new Promise<string>((resolve, reject) => {
        const reader = new FileReader();
        reader.onload = () => resolve(String(reader.result));
        reader.onerror = () => reject(new Error("photo read failed"));
        reader.readAsDataURL(blob);
      });
    });
  load.catch(() => photoByPath.delete(path));
  photoByPath.set(path, load);
  return load;
};
