// Rules of voice changes on the review, written new for V3RDE (NEW_WORK_UI.md, UI 17). The voice service (branch
// services, post/voice.go) turns the clip into text (Muse Voice Transcribe) and the text into a proposal of changes
// to the found items (Muse Spark). The app shows the proposal, and changes nothing until the user applies it;
// nothing posts until Post. Pure functions, so the tests run them directly.
import type { ItemForm } from "./logic";

export type Change = { item: number; title?: string; number_of_serving?: number; remove?: boolean };
export type NewItem = { title: string; number_of_serving: number; needs_photo: boolean };
export type Proposal = {
  transcript: string; changes: Change[]; new_items: NewItem[]; pickup_text?: string; unclear?: string; dropped: string[];
};

/** The items as the voice service takes them: an empty title goes as "Item n" (the service needs a title). */
export function voiceItems(items: ItemForm[], unnamed: (n: number) => string) {
  return items.map((it, k) => ({ item: it.item, title: it.title.trim() || unnamed(k + 1), number_of_serving: Number(it.servings) || 1 }));
}

/** The proposal from the JSON answer (fields that are not there are empty). */
export function proposalOf(body: unknown): Proposal {
  const b = (body ?? {}) as Record<string, unknown>;
  const list = <T,>(v: unknown): T[] => (Array.isArray(v) ? (v as T[]) : []);
  return {
    transcript: typeof b.transcript === "string" ? b.transcript : "",
    changes: list<Change>(b.changes).filter((c) => Number.isInteger(c?.item)),
    new_items: list<NewItem>(b.new_items).filter((n) => typeof n?.title === "string"),
    pickup_text: typeof b.pickup_text === "string" ? b.pickup_text : undefined,
    unclear: typeof b.unclear === "string" && b.unclear ? b.unclear : undefined,
    dropped: list<unknown>(b.dropped).filter((d): d is string => typeof d === "string" && d.length > 0),
  };
}

/** The items and the pickup after the user applies the proposal (servings stay 1 to 99). */
export function applyProposal(items: ItemForm[], pickup: string, p: Proposal): { items: ItemForm[]; pickup: string } {
  const byItem = new Map(p.changes.map((c) => [c.item, c]));
  const out: ItemForm[] = [];
  for (const it of items) {
    const c = byItem.get(it.item);
    if (c?.remove) continue;
    const n = c?.number_of_serving;
    out.push({
      ...it,
      title: typeof c?.title === "string" && c.title.trim() ? c.title.trim() : it.title,
      servings: typeof n === "number" && Number.isFinite(n) ? String(Math.min(99, Math.max(1, Math.round(n)))) : it.servings,
    });
  }
  return { items: out, pickup: p.pickup_text !== undefined ? p.pickup_text : pickup };
}

/** The words of the lines that show a proposal. */
export type ProposalWords = {
  rename: (n: number, from: string, to: string) => string;
  servings: (n: number, from: string, to: number) => string;
  remove: (n: number, title: string) => string;
  pickup: (text: string) => string;
  question: (text: string) => string;
  notAdded: (title: string, servings: number) => string;
  removedBefore: string;
  dropped: (text: string) => string;
  none: string;
};

/** One line for each change, in the order of the list ("Item 2" = the second card). */
export function proposalLines(items: ItemForm[], pickup: string, p: Proposal, w: ProposalWords): string[] {
  const lines: string[] = [];
  items.forEach((it, k) => {
    const c = p.changes.find((x) => x.item === it.item);
    if (!c) return;
    if (c.remove) return void lines.push(w.remove(k + 1, it.title));
    if (typeof c.title === "string" && c.title.trim() && c.title.trim() !== it.title) lines.push(w.rename(k + 1, it.title, c.title.trim()));
    if (typeof c.number_of_serving === "number" && String(c.number_of_serving) !== it.servings) lines.push(w.servings(k + 1, it.servings, c.number_of_serving));
  });
  if (p.changes.some((c) => !items.some((it) => it.item === c.item))) lines.push(w.removedBefore);
  if (p.pickup_text !== undefined && p.pickup_text !== pickup) lines.push(w.pickup(p.pickup_text));
  for (const n of p.new_items) lines.push(w.notAdded(n.title, n.number_of_serving));
  if (p.unclear) lines.push(w.question(p.unclear));
  for (const d of p.dropped) lines.push(w.dropped(d));
  return lines.length ? lines : [w.none];
}

/** True when applying the proposal changes something. */
export function changesSomething(items: ItemForm[], pickup: string, p: Proposal): boolean {
  const after = applyProposal(items, pickup, p);
  return after.pickup !== pickup || after.items.length !== items.length ||
    after.items.some((it, k) => it.title !== items[k].title || it.servings !== items[k].servings);
}
