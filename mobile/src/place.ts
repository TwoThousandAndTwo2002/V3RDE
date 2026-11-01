// The place and the distance rules of Receive and Give, written new for V3RDE (X49e, #74; T9 2026-10-30, the
// approved "distance" mockup v2). The receiver picks 1, 5 or 10 mi; food farther than that shows in gray (up to
// 25 mi, the server's limit), and a grab of a gray item asks first. The place is the phone location. X49q (T9
// 2026-10-31: "lets drop zip code", "Everywhere"): no ZIP code; with location off the list has no distance.
// Pure functions, except `readPlaceChoice`, `savePlaceChoice` and `currentPosition`.
import type { Listing } from "./api";
import { UNITS } from "./words";

/** Whole seconds until expires_at (0 after it, or when it does not parse); the rule of listing.secondsLeft. Here again
 *  because the tests run this file without a bundler (type imports only). */
function secondsLeft(l: Pick<Listing, "expires_at">, now: number): number {
  const end = Date.parse(l.expires_at);
  return Number.isNaN(end) ? 0 : Math.max(0, Math.ceil((end - now) / 1000));
}

/** The distances the user can pick (miles); the first pick is 5. */
export const REACHES = [1, 5, 10] as const;
export type Reach = (typeof REACHES)[number];
export const FIRST_REACH: Reach = 5;

/** A place for a request: a point (the phone location, or the point of the chosen pickup address). */
export type Place = { lat: number; lng: number };

/** What the user chose, kept on this device: the location, and the distance. */
export type PlaceChoice = { how: "location" };
export type Saved = { choice: PlaceChoice | null; reach: Reach };

/** The query or form fields of a place: lat and lng. */
export function placeFields(p: Place): Array<[string, string]> {
  return [["lat", p.lat.toFixed(6)], ["lng", p.lng.toFixed(6)]];
}

/** "?lat=..&lng=..", with the cursor when there is one; no place: only the cursor (the list has no distance). */
export function placeQuery(p: Place | null, after = ""): string {
  const fields = p ? placeFields(p) : [];
  if (after) fields.push(["after", after]);
  return fields.length ? "?" + fields.map(([k, v]) => k + "=" + encodeURIComponent(v)).join("&") : "";
}

/** The distance on a card: one decimal ("0.4 mi"); under 0.05 mi it is "0.1 mi or less". */
export function milesText(d: number | undefined): string {
  if (d === undefined || !Number.isFinite(d)) return "";
  if (d < 0.05) return UNITS.near;
  return UNITS.miles((Math.round(d * 10) / 10).toFixed(1));
}

/** Far: farther than the distance the user picked (T9: "if its more then the preset the choose then its a far grab"). */
export function isFar(l: Pick<Listing, "distance_mi">, reach: number): boolean {
  return l.distance_mi !== undefined && l.distance_mi > reach;
}

/** The list in two parts, each in the server's order (nearest first): within the distance, and farther. */
export function splitByReach<T extends Pick<Listing, "distance_mi">>(items: T[], reach: number): { near: T[]; far: T[] } {
  return { near: items.filter((l) => !isFar(l, reach)), far: items.filter((l) => isFar(l, reach)) };
}

/** Whole minutes left on the timer, at least 1 while it runs (for "The timer ends in 12 min"). */
export function minutesLeft(l: Pick<Listing, "expires_at">, now = Date.now()): number {
  const s = secondsLeft(l, now);
  return s === 0 ? 0 : Math.max(1, Math.round(s / 60));
}

/** The user can give the grab back until the timer ends (T9: "yes until the timer ends"). */
export function canRelease(l: Pick<Listing, "status" | "collected_by" | "expires_at">, userId: string, now = Date.now()): boolean {
  return l.status === "collected" && l.collected_by === userId && secondsLeft(l, now) > 0;
}

/** The saved choice, from text (a bad or old value is no choice). */
export function parseSaved(text: string | null): Saved {
  const none: Saved = { choice: null, reach: FIRST_REACH };
  if (!text) return none;
  try {
    const v = JSON.parse(text) as { how?: unknown; reach?: unknown };
    const reach = (REACHES as readonly number[]).includes(v.reach as number) ? (v.reach as Reach) : FIRST_REACH;
    if (v.how === "location") return { choice: { how: "location" }, reach };
    return { choice: null, reach }; // X49q: an old "zip" choice is no choice: the app asks for the location again
  } catch {
    return none;
  }
}

const KEY = "v3rde.place";

/** The saved choice of this device (web: localStorage; it can be missing or blocked: then nothing is saved). */
export function readPlaceChoice(): Saved {
  try {
    return parseSaved(globalThis.localStorage?.getItem(KEY) ?? null);
  } catch {
    return parseSaved(null);
  }
}

export function savePlaceChoice(s: Saved): void {
  try {
    globalThis.localStorage?.setItem(KEY, JSON.stringify({ ...(s.choice ?? {}), reach: s.reach }));
  } catch {
    // a private window or blocked storage: the choice lives until the page closes
  }
}

/** Why there is no location: the user said no, or this device cannot give one. */
export class NoLocation extends Error {
  code: "denied" | "unavailable";
  constructor(code: "denied" | "unavailable") { super(code); this.code = code; }
}

type Geo = { getCurrentPosition: (ok: (p: { coords: { latitude: number; longitude: number } }) => void,
  bad: (e: { code: number }) => void, opts: object) => void };

/** The location now (the browser asks the user the first time). A phone build has no location module yet: "unavailable". */
export function currentPosition(geo: Geo | undefined = (globalThis.navigator as { geolocation?: Geo } | undefined)?.geolocation,
  timeoutMs = 15_000): Promise<{ lat: number; lng: number }> {
  return new Promise((resolve, reject) => {
    if (!geo) return reject(new NoLocation("unavailable"));
    geo.getCurrentPosition((p) => resolve({ lat: p.coords.latitude, lng: p.coords.longitude }),
      (e) => reject(new NoLocation(e.code === 1 ? "denied" : "unavailable")), // 1 = PERMISSION_DENIED
      { enableHighAccuracy: false, timeout: timeoutMs, maximumAge: 60_000 });
  });
}
