// Tests of the voice rules (src/voice.ts), written new for V3RDE (NEW_WORK_UI.md, UI 17).
import test from "node:test";
import { strict as assert } from "node:assert";
import { applyProposal, canUndo, changeCount, keepOne, changesSomething, chatTurn, joinFound, turnsWithFound, liveMarks, proposalLines, proposalOf, stableJson, undoTo, voiceError, voiceItems } from "../src/voice.ts";

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
  assert.deepEqual(proposalOf(null), { transcript: "", changes: [], new_items: [], pickup_text: undefined, unclear: undefined, dropped: [], find: [] });
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

test("a line that names the pickup, changed or not, is marked", () => {
  const same = chatTurn(ITEMS, "9 Elm Rd", proposalOf({ changes: [], pickup_text: "9 Elm Rd" }), "said", "pickup at 9 Elm Rd", W, M);
  assert.equal(same.pickupMarked, false);
  assert.equal(same.pickupSaid, true);
  assert.equal(chatTurn(ITEMS, "", proposalOf({ changes: [] }), "typed", "hi", W, M).pickupSaid, false);
});

// X49 review: a change of the details after a chat turn ends Undo (Undo would wipe the new details)
test("a details change after a chat turn ends Undo; the same details in another key order do not", () => {
  const withD = ITEMS.map((x) => (x.item === 2 ? { ...x, details: { meal_condition: "sealed" as const, weight: 1.2 } } : x));
  const t = chatTurn(withD, "Front", proposalOf({ changes: [{ item: 1, number_of_serving: 8 }] }), "typed", "8", W, M);
  assert.equal(canUndo([t], t.after.items, t.after.pickup), true);
  const changed = t.after.items.map((x) => (x.item === 2 ? { ...x, details: { meal_condition: "ready_to_eat" as const, weight: 1.2 } } : x));
  assert.equal(canUndo([t], changed, t.after.pickup), false);
  const reordered = t.after.items.map((x) => (x.item === 2 ? { ...x, details: { weight: 1.2, meal_condition: "sealed" as const } } : x));
  assert.equal(canUndo([t], reordered, t.after.pickup), true);
  assert.equal(stableJson({ b: 1, a: [2, { d: 3, c: undefined }] }), '{"a":[2,{"d":3}],"b":1}');
  assert.equal(stableJson(undefined), "");
});

// I4: the chat asks the find service for a missed item
test("I4: find names in the proposal: strings, trimmed, once each, at most 3; the turn keeps them", () => {
  assert.deepEqual(proposalOf({ find: [" rice ", "rice", 3, "", "bread", "cake", "pie"] }).find, ["rice", "bread", "cake"]);
  const t = chatTurn(ITEMS, "Lobby", proposalOf({ find: ["rice"] }), "typed", "you missed the rice", W, M);
  assert.deepEqual(t.find, ["rice"]);
  assert.equal(t.count, 0);
  assert.equal(chatTurn(ITEMS, "Lobby", proposalOf({}), "typed", "x", W, M).find, undefined);
});

test("I4: found items join after the highest number used so far; a removed found item's number is not used again", () => {
  const now = [it(1, "Pasta", "6")]; // the scan had items 1 to 3
  const j = joinFound(now, 3, [it(1, "Rice", "2"), it(2, "Beans", "1")]);
  assert.deepEqual(j.items.map((x) => [x.item, x.title]), [[1, "Pasta"], [4, "Rice"], [5, "Beans"]]);
  assert.deepEqual(j.added.map((x) => x.item), [4, 5]);
  assert.equal(j.top, 5);
  const removed = j.items.filter((x) => x.item !== 5); // the user removes Beans (5)
  assert.deepEqual(joinFound(removed, j.top, [it(1, "Cake", "1")]).added.map((x) => x.item), [6]);
});

test("I4 review: found items join every live turn, so Undo keeps them and the marks stay", () => {
  const p = proposalOf({ changes: [{ item: 1, number_of_serving: 8 }], find: ["rice"] });
  const t = chatTurn(ITEMS, "Lobby", p, "typed", "8 pasta, and rice is missing", W, M);
  const rice = it(4, "Rice", "2");
  const [u] = turnsWithFound([t], [rice]);
  assert.ok(canUndo([u], [...t.after.items, rice], "Lobby"));
  assert.deepEqual(undoTo([u])!.items.map((x) => x.title), ["Pasta", "Salad", "Bread", "Rice"]);
  assert.equal(turnsWithFound([{ ...t, undone: true }], [rice])[0].after.items.length, 3);
});
