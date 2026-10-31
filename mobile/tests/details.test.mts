// Tests of src/details.ts and the weight check of src/capture.ts (X49: the details of an item on the review).
import test from "node:test";
import { strict as assert } from "node:assert";
import { applyDraft, detailLines, detailsPillShows, draftOf, kgText, typeDelay } from "../src/details.ts";
import { weightInput, weightProblem } from "../src/capture.ts";
import { DETAILS } from "../src/words.ts";

test("detailLines: the condition first, then the food, the portions and the weight; then the description", () => {
  assert.deepEqual(detailLines({ meal_condition: "ready_to_eat", is_non_veg: 0, serving_type: "bulk", weight: 1.2,
    description: "A bunch of ripe yellow bananas, a few brown spots." }, DETAILS),
  ["Ready to eat, vegetarian, in bulk, about 1.2 kg.", "A bunch of ripe yellow bananas, a few brown spots."]);
  assert.deepEqual(detailLines({ meal_condition: "cooking_required", is_non_veg: 1, serving_type: "individual" }, DETAILS),
    ["Needs cooking, has meat or fish, in single portions."]);
  assert.deepEqual(detailLines({ meal_condition: "sealed" }, DETAILS), ["A sealed package."]);
});

test("detailLines: missing parts are left out; the first letter is a capital; the cost is never shown", () => {
  assert.deepEqual(detailLines({ is_non_veg: 0, weight: 0.354 }, DETAILS), ["Vegetarian, about 0.35 kg."]);
  assert.deepEqual(detailLines({ serving_type: "bulk" }, DETAILS), ["In bulk."]);
  assert.deepEqual(detailLines({ description: "Bread" }, DETAILS), ["Bread"]);
  assert.deepEqual(detailLines({ meal_cost: 12 }, DETAILS), []);
  assert.deepEqual(detailLines(undefined, DETAILS), []);
  assert.equal(detailLines({ meal_cost: 12, weight: 2 }, DETAILS).join(" ").includes("12"), false);
  assert.equal(kgText(3), "3");
  assert.equal(kgText(1.25), "1.25");
});

test("typeDelay: one letter each letterMs, a pause at the end of a line, -1 at the end", () => {
  const t = "Ab\nC";
  assert.deepEqual([0, 1, 2, 3, 4].map((k) => typeDelay(t, k, 14, 220)), [14, 14, 14, 220, -1]);
});

test("the draft of Change: the choices and the weight; cleared parts are left out; the rest stays", () => {
  const d = { meal_condition: "ready_to_eat" as const, is_non_veg: 0 as const, weight: 1.2, meal_cost: 4, description: "Ripe" };
  assert.deepEqual(draftOf(d), { condition: "ready_to_eat", diet: 0, portions: undefined, weight: "1.2" });
  assert.deepEqual(applyDraft(d, { condition: "sealed", diet: 1, portions: "bulk", weight: "2.5" }),
    { meal_condition: "sealed", is_non_veg: 1, serving_type: "bulk", weight: 2.5, meal_cost: 4, description: "Ripe" });
  assert.deepEqual(applyDraft(d, { weight: "" }), { meal_cost: 4, description: "Ripe" });
  assert.equal(applyDraft(undefined, { weight: "" }), undefined);
  assert.deepEqual(applyDraft(undefined, { weight: "0" }), { weight: 0 });
});

test("the weight field: digits and one point; 0 to 1000 kg; empty is no weight", () => {
  assert.equal(weightInput("1,25kg"), "1.25");
  assert.equal(weightInput("1.2.3"), "1.23");
  assert.equal(weightInput("123456789"), "1234567");
  assert.equal(weightProblem(""), "");
  assert.equal(weightProblem("0"), "");
  assert.equal(weightProblem("1000"), "");
  assert.equal(weightProblem("1000.5"), "Give a weight from 0 to 1000 kg");
  assert.equal(weightProblem("."), "Give a weight from 0 to 1000 kg");
});

test("the details words are short (ASD-STE100: 20 words or fewer)", () => {
  for (const v of Object.values(DETAILS)) if (typeof v === "string") assert.ok(v.split(/\s+/).length <= 20, v);
});

// X49 review fixes
test("Done with no change keeps the weight as the scanner gave it (no false edit)", () => {
  const d = { weight: 1.2345, meal_condition: "sealed" as const };
  assert.equal(draftOf(d).weight, "1.23");
  assert.deepEqual(applyDraft(d, draftOf(d)), d);
  assert.equal(applyDraft(d, { ...draftOf(d), weight: "1.2" })?.weight, 1.2);
});

test("the Details pill stays when the user cleared the scanner's details", () => {
  assert.equal(detailsPillShows(undefined, { meal_condition: "sealed" }, DETAILS), true);
  assert.equal(detailsPillShows({ weight: 2 }, undefined, DETAILS), true);
  assert.equal(detailsPillShows(undefined, undefined, DETAILS), false);
  assert.equal(detailsPillShows({ meal_cost: 3 }, { meal_cost: 3 }, DETAILS), false); // the cost alone is not shown
});

// X49i (T9 2026-10-31): Touch up and the details under one wide More button; the chat in the third column.
test("review: More holds Touch up and the details; the chat column", async () => {
  const fs = await import("node:fs");
  const review = fs.readFileSync(new URL("../src/screens/Review.tsx", import.meta.url), "utf8");
  const theme = fs.readFileSync(new URL("../src/theme.ts", import.meta.url), "utf8");
  const { MORE } = await import("../src/words.ts");
  assert.equal(MORE.title, "More");
  assert.equal(MORE.name(2), "More for item 2");
  assert.match(review, /<ChipBtn testID=\{"more-" \+ n\} wide glyph="menu"/);
  assert.doesNotMatch(review, /testID=\{"details-" \+ n\}/); // no Details button
  assert.doesNotMatch(review, /details-column/); // the details column is gone
  assert.match(review, /<DetailsView it=\{it\} noTitle /); // the details show at once in More
  assert.match(review, /<Popup key=\{sheetItem\.item\} testID="more-sheet" small /); // a phone: the bottom sheet
  assert.match(review, /width: size\.chatColumn/);
  assert.match(review, /<Frost round=\{\{ borderRadius: radius\.card \}\}/); // the chat column is frosted
  assert.match(theme, /chatColumn: 320,/);
  // the review of 79: the expanded state follows More also on a phone; the dialog is wide only with the chat column
  assert.match(review, /name=\{MORE\.name\(n\)\} on=\{open\}/);
  assert.match(review, /extraWide=\{column && items\.length > 0\}/);
  assert.match(review, /\(!!it\.photo_id && !sending\) \|\| hasDetails/);
  const details = fs.readFileSync(new URL("../src/screens/Details.tsx", import.meta.url), "utf8");
  assert.match(details, /\{locked \? null : \(/); // no Change while the listings post
  assert.doesNotMatch(details, /onClose/);
});
