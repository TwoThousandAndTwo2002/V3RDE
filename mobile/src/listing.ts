// Listing rules of the Give and Receive screens, written new for V3RDE (NEW_WORK_UI.md, UI 12 and 13).
// Pure functions (no React, no network), so the tests run them directly.
import type { Listing } from "./api";

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

/** The feed after a grab: the grabbed listing takes the fields the services sent back. */
export function withChange(items: Listing[], change: Partial<Listing> & { id: string }): Listing[] {
  return items.map((l) => (l.id === change.id ? { ...l, ...change } : l));
}

/** A map search for the pickup text (a plain link: no map SDK in the app). */
export function mapLink(pickup: string): string {
  return "https://www.google.com/maps/search/?api=1&query=" + encodeURIComponent(pickup.trim());
}

/** A WhatsApp message to the giver, made from the listing (a plain link: wa.me). */
export function chatLink(message: string): string {
  return "https://wa.me/?text=" + encodeURIComponent(message);
}

/** Give: open the camera first when the giver has no listing that is still up. */
export function cameraFirst(mine: Pick<Listing, "status" | "expires_at">[], now = Date.now()): boolean {
  return mine.every((l) => isGone(l, now));
}
