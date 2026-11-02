// Tests of the new-listing rules (src/capture.ts), written new for V3RDE (NEW_WORK_UI.md, UI 14).
import test from "node:test";
import { strict as assert } from "node:assert";
import { analyzeShare, barcodeOf, codesFound, codesToSend, elapsedText, MAX_CODES, READ_WAIT_MS, listingProblem, servingsInput, sourceNote, stepServings, FORM_WORDS } from "../src/capture.ts";

test("the timer and the progress bar", () => {
  assert.equal(elapsedText(2440), "2.4 s");
  assert.equal(elapsedText(-5), "0.0 s");
  assert.equal(elapsedText(12000), "12.0 s");
  assert.equal(analyzeShare(0), 0);
  assert.equal(analyzeShare(2400), 0.032); // the X25 "Analyzing… 2.4 s" bar: about 3 % full
  assert.equal(analyzeShare(999_999), 1);
});

test("barcodes: only the retail lengths", () => {
  assert.equal(barcodeOf("0 78742 37002 6"), "078742370026");
  assert.equal(barcodeOf("96385074"), "96385074");
  assert.equal(barcodeOf("12345"), "");
  assert.equal(barcodeOf(null), "");
});

test("the form checks follow the listing limits", () => {
  assert.equal(listingProblem("Pasta", "6", "Back door"), "");
  assert.equal(listingProblem("  ", "6", "Back door"), FORM_WORDS.title);
  assert.equal(listingProblem("x".repeat(81), "6", "Back door"), FORM_WORDS.title);
  assert.equal(listingProblem("Pasta", "0", "Back door"), FORM_WORDS.servings);
  assert.equal(listingProblem("Pasta", "100", "Back door"), FORM_WORDS.servings);
  assert.equal(listingProblem("Pasta", "2.5", "Back door"), FORM_WORDS.servings);
  assert.equal(listingProblem("Pasta", 6, ""), FORM_WORDS.pickup);
  assert.equal(listingProblem("Pasta", 6, "x".repeat(201)), FORM_WORDS.pickup);
});

test("the servings stepper and field", () => {
  assert.equal(stepServings("6", 1), "7");
  assert.equal(stepServings("1", -1), "1");
  assert.equal(stepServings("99", 1), "99");
  assert.equal(stepServings("", 1), "2");
  assert.equal(servingsInput("1a2b3"), "12");
});

test("the source line", () => {
  assert.equal(sourceNote("078742370026", "a jar"), "From the barcode 078742370026");
  assert.equal(sourceNote("", "one head of romanesco"), "The scanner says: one head of romanesco");
  assert.equal(sourceNote("", ""), "");
});

// I1: many codes in one still photo
const read = (data: string, x = 10, y = 10, w = 80, h = 40) => ({ data, bounds: { origin: { x, y }, size: { width: w, height: h } } });
test("I1: the codes of a still: retail codes once each, with the box in the still's pixels", () => {
  const got = codesFound([read("0 78742 37002 6", 10.4, 20.6), read("078742370026"), read("12345"), read("0034000002405", 580, 0, 20, 10)], 600, 400);
  assert.deepEqual(got, [
    { code: "078742370026", x: 10, y: 21, w: 80, h: 40, fw: 600, fh: 400 },
    { code: "0034000002405", x: 580, y: 0, w: 20, h: 10, fw: 600, fh: 400 },
  ]);
  // a box outside the still, or a still with no size: the code goes with no box
  assert.deepEqual(codesFound([read("96385074", 590, 0, 20, 10)], 600, 400), [{ code: "96385074", x: 0, y: 0, w: 0, h: 0, fw: 0, fh: 0 }]);
  assert.equal(codesFound([read("96385074")], 0, 0)[0].fw, 0);
  assert.equal(codesFound([{ data: "96385074" }], 600, 400)[0].fw, 0);
  const many = Array.from({ length: MAX_CODES + 3 }, (_, i) => read(String(10000000 + i)));
  assert.equal(codesFound(many, 600, 400).length, MAX_CODES);
});

test("I1: what the scan sends: the live code as today; the codes of the still for two codes, or for a boxed code with no live code", () => {
  const a = { code: "078742370026", x: 1, y: 1, w: 2, h: 2, fw: 10, fh: 10 };
  const b = { ...a, code: "0034000002405" };
  const noBox = { ...a, x: 0, y: 0, w: 0, h: 0, fw: 0, fh: 0 };
  assert.deepEqual(codesToSend("", []), { barcode: "", barcodes: [], shown: 0 });
  assert.deepEqual(codesToSend("078742370026", [a]), { barcode: "078742370026", barcodes: [], shown: 1 }); // the old one-code scan (fast path)
  assert.deepEqual(codesToSend("", [a]), { barcode: "", barcodes: [a], shown: 1 }); // a plate with one packet: the scanner runs too
  assert.deepEqual(codesToSend("", [noBox]), { barcode: "", barcodes: [], shown: 1 }); // no box: as a photo with no code
  assert.deepEqual(codesToSend("", [a, b]), { barcode: "", barcodes: [a, b], shown: 2 });
  assert.deepEqual(codesToSend("96385074", [a]), { barcode: "96385074", barcodes: [a], shown: 2 }); // live + still = two codes
  assert.ok(READ_WAIT_MS > 0 && READ_WAIT_MS <= 3000);
});
