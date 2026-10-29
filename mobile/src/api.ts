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
import { errorMessage, joinUrl } from "./logic";

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
};
/** A new listing: the photo is an item photo of the analyze route (photo_id) or the photo itself. */
export type NewListing = {
  title: string; number_of_serving: number; pickup_text: string; request_id?: string; photo?: Upload; photo_id?: string;
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
    throw new ApiError(0, timedOut ? "Timed out, try again" : "Network error, check the connection");
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
});

export const listings = Object.freeze({
  /** The live listings, one page at a time ("after" = the cursor of the page before). */
  active(after = "") {
    const query = after ? "?after=" + encodeURIComponent(after) : "";
    return request<{ items: Listing[]; next: string }>("GET", "/api/listings" + query);
  },
  mine: () => request<{ items: Listing[] }>("GET", "/api/listings/mine"),
  grab: (id: string) => request<Listing>("POST", "/api/listings/" + encodeURIComponent(id) + "/grab"),
  create(n: NewListing) {
    const form = formOf([["title", n.title], ["number_of_serving", String(n.number_of_serving)],
      ["pickup_text", n.pickup_text]], [["request_id", n.request_id]]);
    if (n.photo_id) form.append("photo_id", n.photo_id);
    else if (n.photo) addFile(form, "photo", n.photo);
    return request<Listing>("POST", "/api/listings", { form, timeoutMs: 30_000 });
  },
});

/** The one analysis route (session cookie): the photo, the barcode read on the phone (optional), and how the
 *  photo was taken (the server logs at most 512 bytes of it). The service stops at 55 s; the app waits 75 s. */
export function analyze(photo: Upload, barcode = "", capture?: Record<string, unknown>) {
  const form = formOf([], [["barcode", barcode]]);
  addFile(form, "image", photo);
  if (capture && Object.keys(capture).length > 0) form.append("capture", JSON.stringify(capture).slice(0, 512));
  return request<any>("POST", "/api/analyze", { form, timeoutMs: 75_000 });
}

/** The user's corrections to an answer (logic.labelReport). Best effort: a failed report never stops a post. */
export function analyzeLabels(report: object): Promise<void> {
  return request<void>("POST", "/api/analyze/labels", { json: report, timeoutMs: 10_000 }).catch(() => undefined);
}

/** Voice posting: one WAV clip and the review list (item, title, servings). The answer is a proposal; the app
 *  posts nothing until the donor taps Post. The service stops at 25 s; the app waits 35 s. */
export function voice(clip: Upload, items: VoiceItem[], pickupText = "", lang = "") {
  const form = formOf([], [["pickup_text", pickupText], ["lang", lang]]);
  addFile(form, "audio", clip);
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
