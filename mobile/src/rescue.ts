// R5 (T9 2026-10-31): the rescue of an expired listing nobody grabbed, the app side.
// Pure functions (no React, no network), so the tests run them directly. The service is post/rescue.go
// (R1-R4): POST /api/rescue/{listing_id}?async=1, then GET /api/rescue/{rescue_id} until done or failed.

/** One place of a rescue answer. */
import { UNITS } from "./words";
export type RescuePlace = {
  id: string; name: string; type?: string; address?: string; phone?: string; distance_miles?: number;
  website?: string; email?: string; source?: string; source_url?: string; tel?: string; mailto?: string; directions?: string;
};
/** A rescue as the service sends it. */
export type Rescue = {
  rescue_id: string; listing_id: string; title: string; number_of_serving: number; kind?: string;
  mode?: "email" | "dropoff" | ""; shelf_stable?: boolean; reason?: string; places: RescuePlace[]; attribution?: string;
  status?: "running" | "done" | "failed" | ""; code?: string; error?: string; found_by?: string;
};

/** The words of each place type (Feed America types and the web search types). */
export const TYPE_WORDS: Record<string, string> = {
  other: "Food program", // X49x: a type with no words of its own
  food_pantry: "Food pantry", pantry: "Food pantry", food_bank: "Food bank", mobile_pantry: "Mobile pantry",
  soup_kitchen: "Soup kitchen", meal_program: "Meal program", shelter: "Shelter",
};

/** "Food pantry · 0.5 mi" (the type, then the distance when the service has one). */
export function placeLine(p: RescuePlace): string {
  const kind = (p.type && p.type !== "other" ? TYPE_WORDS[p.type] : undefined) ?? TYPE_WORDS.other;
  const d = typeof p.distance_miles === "number" && p.distance_miles > 0 ? UNITS.miles(String(Math.round(p.distance_miles * 10) / 10)) : "";
  return d ? kind + " · " + d : kind;
}

/** Only the links the app may open: https directions, tel: and mailto: (the service makes them; a web place
 *  gives none of its own). */
export function safeLink(url: string | undefined, kind: "https" | "tel" | "mailto"): string {
  if (typeof url !== "string") return "";
  return url.startsWith(kind === "https" ? "https://www.google.com/maps/dir/" : kind + ":") ? url : "";
}

/** What the screen shows for an answer. */
export type RescueView = "looking" | "places" | "none" | "failed";
export function rescueView(r: Rescue | null): RescueView {
  if (!r || r.status === "running") return "looking";
  if (r.status === "failed") return "failed";
  return r.places.length > 0 ? "places" : "none";
}

/** The service's 400 when the listing has no pickup place (its words still say "no ZIP"): X49q: the app then offers
 *  the giver's location, never a ZIP. */
export function needsPlace(status: number, message: string): boolean {
  return status === 400 && (message.includes("no ZIP") || message.includes("lat and lng"));
}

/** R5: an expired listing nobody grabbed can be rescued. */
export function canRescue(l: { status: string; expires_at: string }, now = Date.now()): boolean {
  const end = Date.parse(l.expires_at);
  return l.status === "active" && !Number.isNaN(end) && end <= now;
}

/** The polls of a rescue job: once a second, up to RESCUE_WAIT_MS (the service job limit is 90 s). */
export const RESCUE_POLL_MS = 1000;
export const RESCUE_WAIT_MS = 100_000;

/** X49x: the word tables of this file (src/lang.ts). */
export const RESCUE_TABLES = { TYPE_WORDS } as unknown as Record<string, Record<string, unknown>>;
