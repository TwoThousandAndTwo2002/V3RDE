// Tests of the new-listing rules (src/capture.ts), written new for V3RDE (NEW_WORK_UI.md, UI 14).
import test from "node:test";
import { strict as assert } from "node:assert";
import { analyzeShare, barcodeOf, elapsedText, listingProblem, servingsInput, sourceNote, stepServings, FORM_WORDS } from "../src/capture.ts";

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
