// Tests of the voice rules (src/voice.ts), written new for V3RDE (NEW_WORK_UI.md, UI 17).
import test from "node:test";
import { strict as assert } from "node:assert";
import { applyProposal, changesSomething, proposalLines, proposalOf, voiceItems } from "../src/voice.ts";

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
