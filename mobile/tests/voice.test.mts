// Tests of the voice rules (src/voice.ts), written new for V3RDE (NEW_WORK_UI.md, UI 17).
import test from "node:test";
import { strict as assert } from "node:assert";
import { applyProposal, canUndo, changeCount, keepOne, changesSomething, chatTurn, liveMarks, proposalLines, proposalOf, undoTo, voiceError, voiceItems } from "../src/voice.ts";

const it = (item: number, title: string, servings: string) => ({ item, title, servings, photo_id: "", description: "", err: "" });
const ITEMS = [it(1, "Pasta", "6"), it(2, "Salad", "2"), it(3, "Bread", "4")];
const W = {
  rename: (n: number, a: string, b: string) => `${n}: ${a} > ${b}`,
  servings: (n: number, a: string, b: number) => `${n}: ${a} > ${b}`,
  remove: (n: number, t: string) => `${n}: remove ${t}`,
  pickup: (t: string) => `pickup ${t}`,
  question: (t: string) => `? ${t}`,
  notAdded: (t: string, s: number) => `new ${t} ${s}`,
  removedBefore: "gone",
  dropped: (t: string) => `dropped ${t}`,
  none: "none",
};

test("the proposal from the answer", () => {
  const p = proposalOf({ transcript: "six trays", changes: [{ item: 1, number_of_serving: 6 }, { bad: 1 }], new_items: [{ title: "Cake", number_of_serving: 1, needs_photo: true }], unclear: "" });
  assert.equal(p.changes.length, 1);
  assert.equal(p.new_items.length, 1);
  assert.equal(p.unclear, undefined);
  assert.deepEqual(proposalOf(null), { transcript: "", changes: [], new_items: [], pickup_text: undefined, unclear: undefined, dropped: [] });
  assert.deepEqual(proposalOf({ dropped: ["price", 3, ""] }).dropped, ["price"]);
});

test("apply: change, remove, pickup; servings stay 1 to 99", () => {
  const p = proposalOf({ changes: [{ item: 1, number_of_serving: 120 }, { item: 2, remove: true }, { item: 3, title: " Rye bread " }], pickup_text: "Back door" });
  const r = applyProposal(ITEMS, "Lobby", p);
  assert.deepEqual(r.items.map((x) => [x.item, x.title, x.servings]), [[1, "Pasta", "99"], [3, "Rye bread", "4"]]);
  assert.equal(r.pickup, "Back door");
  assert.equal(applyProposal(ITEMS, "Lobby", proposalOf({})).pickup, "Lobby");
});

test("an empty title goes as Item n", () => {
  assert.deepEqual(voiceItems([it(4, "  ", "x"), it(5, "Soup", "3")], (n) => "Item " + n),
    [{ item: 4, title: "Item 1", number_of_serving: 1 }, { item: 5, title: "Soup", number_of_serving: 3 }]);
});

test("the lines of a proposal", () => {
  const p = proposalOf({ changes: [{ item: 2, remove: true }, { item: 1, title: "Penne", number_of_serving: 8 }], pickup_text: "Back door",
    new_items: [{ title: "Cake", number_of_serving: 2, needs_photo: true }], unclear: "Which salad?" });
  assert.deepEqual(proposalLines(ITEMS, "Lobby", p, W), ["1: Pasta > Penne", "1: 6 > 8", "2: remove Salad", "pickup Back door", "new Cake 2", "? Which salad?"]);
  assert.deepEqual(proposalLines(ITEMS, "Lobby", proposalOf({}), W), ["none"]);
  assert.deepEqual(proposalLines(ITEMS.slice(0, 1), "Lobby", proposalOf({ changes: [{ item: 3, remove: true }], dropped: ["a price"] }), W), ["gone", "dropped a price"]);
  assert.equal(changesSomething(ITEMS, "Lobby", p), true);
  assert.equal(changesSomething(ITEMS, "Lobby", proposalOf({ changes: [{ item: 1, number_of_serving: 6 }] })), false);
});

test("voiceError (U18): the code of the answer picks the line", () => {
  const w = { failed: "F", noSpeech: "N", busy: "B", paused: "P" };
  assert.equal(voiceError(422, "no_speech", w), "N");
  assert.equal(voiceError(429, "caller_daily_cap_reached", w), "P");
  assert.equal(voiceError(503, "daily_cap_reached", w), "P");
  assert.equal(voiceError(503, "busy", w), "B");
  assert.equal(voiceError(429, "rate_limited", w), "B");
  assert.equal(voiceError(502, "upstream_error", w), "F");
  assert.equal(voiceError(0, "", w), "F");
});

// X48s: the chat bar. A turn applies at once, marks the changed cards, and Undo (the last turn only) gives the list back.
const M = { title: "title", servings: (a: string, b: string) => `${a} to ${b} servings`, mark: (p: string[]) => "chat: " + p.join(", ") };
test("a chat turn: count, marks, undo on the last turn only", () => {
  const p = proposalOf({ changes: [{ item: 1, title: "Ripe pasta", number_of_serving: 8 }, { item: 3, remove: true }], pickup_text: "Back door" });
  assert.equal(changeCount(ITEMS, "Front", p), 4); // title, servings, a removed item, the pickup
  const t = chatTurn(ITEMS, "Front", p, "said", "eight servings of ripe pasta", W, M);
  assert.equal(t.count, 4);
  assert.deepEqual(t.marks, { 1: "chat: title, 6 to 8 servings" });
  assert.equal(t.pickupMarked, true);
  assert.equal(t.after.items.length, 2);
  const turns = [t];
  assert.equal(canUndo(turns, t.after.items, t.after.pickup), true);
  assert.deepEqual(liveMarks(turns, t.after.items, t.after.pickup), { marks: t.marks, pickup: true });
  assert.deepEqual(undoTo(turns), { items: ITEMS, pickup: "Front" });
  // a hand edit after the turn ends Undo and the marks
  const edited = t.after.items.map((x) => (x.item === 2 ? { ...x, servings: "5" } : x));
  assert.equal(canUndo(turns, edited, t.after.pickup), false);
  assert.deepEqual(liveMarks(turns, edited, t.after.pickup), { marks: {}, pickup: false });
  // a second turn: only it can be undone
  const p2 = proposalOf({ changes: [{ item: 2, number_of_serving: 4 }] });
  const t2 = chatTurn(t.after.items, t.after.pickup, p2, "typed", "salad 4", W, M);
  assert.equal(canUndo([t, t2], t2.after.items, t2.after.pickup), true);
  assert.deepEqual(undoTo([t, t2])?.items, t.after.items);
  assert.equal(canUndo([t, { ...t2, undone: true }], t.after.items, t.after.pickup), false);
  // no change: no lines but "none", nothing to undo
  const t3 = chatTurn(ITEMS, "Front", proposalOf({ changes: [] }), "typed", "hello", W, M);
  assert.equal(t3.count, 0);
  assert.deepEqual(t3.lines, []);
  assert.equal(canUndo([t3], ITEMS, "Front"), false);
});

test("a line may not remove every item", () => {
  const all = proposalOf({ changes: [{ item: 1, remove: true }, { item: 2, remove: true }, { item: 3, remove: true }, { item: 1, number_of_serving: 2 }] });
  const kept = keepOne(ITEMS, all, "not all");
  assert.equal(kept.changes.some((c) => c.remove), false);
  assert.deepEqual(kept.dropped, ["not all"]);
  assert.equal(applyProposal(ITEMS, "", kept).items.length, 3);
  const one = [ITEMS[0]];
  assert.equal(keepOne(one, proposalOf({ changes: [{ item: 1, remove: true }] }), "x").changes.length, 0); // the one-item form
  const some = proposalOf({ changes: [{ item: 2, remove: true }] });
  assert.equal(keepOne(ITEMS, some, "x"), some); // removing some items is fine
});
