// Rules of voice changes on the review, written new for V3RDE (NEW_WORK_UI.md, UI 17). The voice service (branch
// services, post/voice.go) turns the clip into text (Muse Voice Transcribe) and the text into a proposal of changes
// to the found items (Muse Spark). The app shows the proposal, and changes nothing until the user applies it;
// nothing posts until Post. Pure functions, so the tests run them directly.
import type { ItemForm } from "./logic";

export type Change = { item: number; title?: string; number_of_serving?: number; remove?: boolean };
export type NewItem = { title: string; number_of_serving: number; needs_photo: boolean };
export type Proposal = {
  transcript: string; changes: Change[]; new_items: NewItem[]; pickup_text?: string; unclear?: string; dropped: string[];
  /** I4: the names of missed items the app asks the find service for (at most 3). */
  find: string[];
};

/** X59 (T9 Nov 2: "there is no chat history that is fed into the prompt"): the last turn of the chat that ended in a
 *  question. The next line takes it to the service (earlier_said, earlier_asked), so a short answer ("the O's")
 *  finishes the request. */
export type Earlier = { said: string; asked: string; turns: number; list: { items: ItemForm[]; pickup: string } };
/** At most this many questions in a row carry the earlier turn; then the chat starts fresh. */
export const EARLIER_MAX_TURNS = 2;
/** The longest earlier text the app sends (the service cuts at 500 bytes too: VoiceMaxEarlier). */
export const EARLIER_MAX_SAID = 500;

/** The earlier turn after an answer: kept only when the model asked a question (and fewer than EARLIER_MAX_TURNS in a
 *  row), with what was said in this turn and before it, and the list as the answer left it (`list`); otherwise none,
 *  so old words never change a new request. */
export function nextEarlier(prev: Earlier | null, said: string, p: Proposal, list: { items: ItemForm[]; pickup: string }): Earlier | null {
  const n = (prev?.turns ?? 0) + 1;
  if (!p.unclear || n > EARLIER_MAX_TURNS) return null;
  const all = (prev ? prev.said + " / " : "") + said.trim();
  return { said: all.length > EARLIER_MAX_SAID ? all.slice(all.length - EARLIER_MAX_SAID) : all, asked: p.unclear, turns: n, list };
}

/** X59 review: the earlier turn to send with the next line: none when the list changed since that answer (a hand
 *  edit or an Undo), so the model never applies again what the donor took back. */
export function earlierToSend(e: Earlier | null, items: ItemForm[], pickup: string): Earlier | null {
  return e && sameList(e.list, items, pickup) ? e : null;
}

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
    find: [...new Set(list<unknown>(b.find).filter((d): d is string => typeof d === "string" && d.trim().length > 0).map((d) => d.trim().slice(0, 40)))].slice(0, 3),
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

/** U18: the words of an error answer of the voice service (post/voice.go): its code picks the line. */
export function voiceError(status: number, code: string, words: { failed: string; noSpeech: string; busy: string; paused: string }): string {
  if (code === "no_speech") return words.noSpeech;
  if (code === "daily_cap_reached" || code === "caller_daily_cap_reached") return words.paused;
  if (code === "busy" || code === "rate_limited" || status === 429) return words.busy;
  return words.failed;
}

// ---- X48s: the chat bar (T9 2026-10-30, the approved "V3RDE Chat Bar Mockup"). Talk or type; the changes go
// into the list at once; the chat keeps what was said or typed and what changed; Undo only on the last change.

/** How the user gave a chat line. */
export type How = "said" | "typed";
/** One exchange of the chat: the user's line, the app's answer, the list before and after (for Undo). */
export type Turn = {
  how: How; text: string; lines: string[]; count: number;
  before: { items: ItemForm[]; pickup: string }; after: { items: ItemForm[]; pickup: string };
  marks: Record<number, string>; pickupMarked: boolean; undone: boolean;
  /** X48u review: the line named a pickup (changed or not): its words may hold an address. */
  pickupSaid: boolean;
  /** I4: the missed items this line asked to find. */
  find?: string[];
};

/** The words of the chat marks on the cards. */
export type MarkWords = { title: string; servings: (from: string, to: string) => string; mark: (parts: string[]) => string };

/** The number of real changes in a proposal (renames, servings, removed items, the pickup). */
export function changeCount(items: ItemForm[], pickup: string, p: Proposal): number {
  const after = applyProposal(items, pickup, p);
  let n = items.length - after.items.length + (after.pickup !== pickup ? 1 : 0);
  for (const a of after.items) {
    const b = items.find((x) => x.item === a.item);
    if (b && a.title !== b.title) n++;
    if (b && a.servings !== b.servings) n++;
  }
  return n;
}

/** X48s review: a line may not remove every item (the review needs one): those removes are not used, and say so. */
export function keepOne(items: ItemForm[], p: Proposal, notAll: string): Proposal {
  const removes = p.changes.filter((c) => c.remove && items.some((it) => it.item === c.item));
  if (items.length === 0 || removes.length < items.length) return p;
  return { ...p, changes: p.changes.filter((c) => !c.remove), dropped: [...p.dropped, notAll] };
}

/** The exchange for a proposal: the list after it, and a mark for each changed card ("title, 2 to 6 servings"). */
export function chatTurn(items: ItemForm[], pickup: string, p: Proposal, how: How, text: string, w: ProposalWords, m: MarkWords): Turn {
  const after = applyProposal(items, pickup, p);
  const marks: Record<number, string> = {};
  for (const a of after.items) {
    const b = items.find((x) => x.item === a.item);
    if (!b) continue;
    const parts: string[] = [];
    if (a.title !== b.title) parts.push(m.title);
    if (a.servings !== b.servings) parts.push(m.servings(b.servings, a.servings));
    if (parts.length) marks[a.item] = m.mark(parts);
  }
  const count = changeCount(items, pickup, p);
  return { how, text, lines: count || p.unclear || p.dropped.length || p.new_items.length ? proposalLines(items, pickup, p, w) : [], count,
    before: { items, pickup }, after, marks, pickupMarked: after.pickup !== pickup, undone: false, pickupSaid: p.pickup_text !== undefined,
    ...(p.find.length ? { find: p.find } : {}) };
}

/** JSON with the keys in order (two details with the same values in another order are the same). */
export function stableJson(v: unknown): string {
  if (v === undefined) return "";
  if (v === null || typeof v !== "object") return JSON.stringify(v);
  if (Array.isArray(v)) return "[" + v.map(stableJson).join(",") + "]";
  const o = v as Record<string, unknown>;
  return "{" + Object.keys(o).filter((k) => o[k] !== undefined).sort().map((k) => JSON.stringify(k) + ":" + stableJson(o[k])).join(",") + "}";
}

// X49 review: the details too (a change of the details after a chat turn ends its Undo)
const sameList = (a: { items: ItemForm[]; pickup: string }, items: ItemForm[], pickup: string) =>
  a.pickup === pickup && a.items.length === items.length &&
  a.items.every((x, k) => x.item === items[k].item && x.title === items[k].title && x.servings === items[k].servings &&
    stableJson(x.details) === stableJson(items[k].details));

/** Undo is on the last turn only, and only while the list is as that turn left it (a later hand edit ends it). */
export function canUndo(turns: Turn[], items: ItemForm[], pickup: string): boolean {
  const last = turns[turns.length - 1];
  return !!last && !last.undone && last.count > 0 && sameList(last.after, items, pickup);
}

/** The card and pickup marks to show: those of the last turn, while the list is as it left it. */
export function liveMarks(turns: Turn[], items: ItemForm[], pickup: string): { marks: Record<number, string>; pickup: boolean } {
  return canUndo(turns, items, pickup) ? { marks: turns[turns.length - 1].marks, pickup: turns[turns.length - 1].pickupMarked }
    : { marks: {}, pickup: false };
}

/** The list after Undo: the list just before the last turn (Undo shows only while the list is as that turn left it). */
export function undoTo(turns: Turn[]): { items: ItemForm[]; pickup: string } | null {
  const last = turns[turns.length - 1];
  return last && !last.undone ? last.before : null;
}

/** I4: the items of a find answer join the list after `top`, the highest item number used so far in this review
 *  (the scan's, and every found item's, also one the user removed since): a number is never used twice, so
 *  touch-up, marks and the label report of a removed item never mix with a new one. */
export function joinFound(items: ItemForm[], top: number, found: ItemForm[]): { items: ItemForm[]; added: ItemForm[]; top: number } {
  let n = Math.max(top, ...items.map((x) => x.item));
  const added = found.map((f) => ({ ...f, item: ++n }));
  return { items: [...items, ...added], added, top: n };
}

/** I4 review: found items join every turn that is not undone (before and after), so Undo and the card marks of
 *  a turn stay while a find adds to the list, and an Undo does not drop a found item. */
export function turnsWithFound(turns: Turn[], added: ItemForm[]): Turn[] {
  return turns.map((t) => (t.undone ? t : { ...t, before: { ...t.before, items: [...t.before.items, ...added] },
    after: { ...t.after, items: [...t.after.items, ...added] } }));
}
