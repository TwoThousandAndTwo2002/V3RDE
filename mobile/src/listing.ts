// Listing rules of the Give and Receive screens, written new for V3RDE (NEW_WORK_UI.md, UI 12 and 13).
// Pure functions (no React, no network), so the tests run them directly.
import type { Listing, ListingChange } from "./api";
import { FORM_WORDS, LIMITS } from "./capture";

/** Whole seconds until the listing expires (0 when it has expired or the time is not known). */
export function secondsLeft(l: Pick<Listing, "expires_at">, now = Date.now()): number {
  const end = Date.parse(l.expires_at);
  if (Number.isNaN(end)) return 0;
  return Math.max(0, Math.ceil((end - now) / 1000));
}

/** "m:ss" for a time under an hour, "h:mm:ss" above. */
export function clock(seconds: number): string {
  const all = Math.floor(Math.max(0, seconds));
  const h = Math.floor(all / 3600), m = Math.floor((all % 3600) / 60), r = all % 60;
  const two = (v: number) => String(v).padStart(2, "0");
  return h > 0 ? `${h}:${two(m)}:${two(r)}` : `${m}:${two(r)}`;
}

/** A listing that nobody can grab any more: collected, or its time is over. */
export function isGone(l: Pick<Listing, "status" | "expires_at">, now = Date.now()): boolean {
  return l.status === "collected" || secondsLeft(l, now) === 0;
}

/** X48t: the owner can delete a listing until someone grabs it (the service says the same: 409). */
export function canDelete(l: Pick<Listing, "status">): boolean {
  return l.status !== "collected";
}

/** X60 (T9 2026-11-01: change the name and the servings, reset the timer, post an ended listing again): the change
 *  that the popup sends (only the fields that changed), a problem text for a bad field, or null when nothing changes.
 *  An ended listing always sends reset (the service refuses it otherwise). */
export function listingChange(l: Pick<Listing, "title" | "number_of_serving" | "status" | "expires_at">, title: string,
  servings: string, reset: boolean, now = Date.now()): { change: ListingChange } | { problem: string } | null {
  const t = title.trim(), n = Number(servings);
  if (t.length === 0 || t.length > LIMITS.title) return { problem: FORM_WORDS.title };
  if (!Number.isInteger(n) || n < 1 || n > LIMITS.servingsMax) return { problem: FORM_WORDS.servings };
  const change: ListingChange = {};
  if (t !== l.title) change.title = t;
  if (n !== l.number_of_serving) change.number_of_serving = n;
  if (reset || isGone(l, now)) change.reset = true;
  return Object.keys(change).length ? { change } : null;
}

/** The text of the time pill of a card. */
export function timeText(l: Pick<Listing, "status" | "expires_at">, words: { collected: string; expired: string }, now = Date.now()): string {
  if (l.status === "collected") return words.collected;
  const left = secondsLeft(l, now);
  return left === 0 ? words.expired : clock(left);
}

/** One more page of the feed: the new listings after the ones already shown (no listing two times). */
export function addPage(shown: Listing[], page: { items?: Listing[]; next?: string }): { items: Listing[]; next: string } {
  const ids = new Set(shown.map((l) => l.id));
  const fresh = (page.items ?? []).filter((l) => !ids.has(l.id) && (ids.add(l.id), true));
  return { items: [...shown, ...fresh], next: page.next ?? "" };
}

/** U19 (T9 2026-10-29: "for receive i dont want to see my own listings"): the feed without the user's own
 *  listings; with no user id, all of them. */
export function othersOnly(items: Listing[], userId: string | undefined): Listing[] {
  return userId ? items.filter((l) => l.owner_id !== userId) : items;
}

/** U19: one load of the Receive feed. It asks for pages until a page brings a listing of someone else, the feed
 *  ends, or `max` pages: a page of only the user's own listings adds nothing on the screen, so no scroll and no
 *  size change would ask again. `stale()` true (a refresh started): null. The page fetch is the only network part. */
export async function loadOthers(page: (after: string) => Promise<{ items?: Listing[]; next?: string }>,
  shown: Listing[], after: string, userId: string | undefined, stale: () => boolean = () => false, max = 5,
): Promise<{ items: Listing[]; next: string } | null> {
  let got = { items: shown, next: after };
  for (let k = 0; k < max; k++) {
    const before = got.items.length;
    got = addPage(got.items, await page(got.next));
    if (stale()) return null;
    if (got.next === "" || othersOnly(got.items.slice(before), userId).length > 0) break;
  }
  return got;
}

/** The feed after a grab: the grabbed listing takes the fields the services sent back. */
export function withChange(items: Listing[], change: Partial<Listing> & { id: string }): Listing[] {
  return items.map((l) => (l.id === change.id ? { ...l, ...change } : l));
}

/** A map search for the pickup text (a plain link: no map SDK in the app). */
export function mapLink(pickup: string): string {
  return "https://www.google.com/maps/search/?api=1&query=" + encodeURIComponent(pickup.trim());
}

/** Give: open the camera first when the giver has no listing that is still up. */
export function cameraFirst(mine: Pick<Listing, "status" | "expires_at">[], now = Date.now()): boolean {
  return mine.every((l) => isGone(l, now));
}
