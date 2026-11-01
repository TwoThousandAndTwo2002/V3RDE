// X49w (T9 2026-10-31): the phone keyboard must not cover the field the user types in (src/keyboard.ts).
import test from "node:test";
import { strict as assert } from "node:assert";
import { readFileSync } from "node:fs";
import { KEYBOARD_MIN_PX, KEYBOARD_SETTLE_MS, keyboardInset, mustReveal, takesText } from "../src/keyboard.ts";

test("the keyboard height comes from the visual viewport; small differences are the browser bars", () => {
  assert.equal(keyboardInset(844, 844, 0), 0); // no keyboard
  assert.equal(keyboardInset(844, 508, 0), 336); // an iPhone keyboard
  assert.equal(keyboardInset(844, 508, 30), 306); // the page scrolled under the keyboard
  assert.equal(keyboardInset(844, 844 - (KEYBOARD_MIN_PX - 1), 0), 0); // the address bar, not a keyboard
  assert.equal(keyboardInset(844, 844 - KEYBOARD_MIN_PX, 0), KEYBOARD_MIN_PX);
  assert.equal(keyboardInset(0, 500, 0), 0);
  assert.equal(keyboardInset(844, 0, 0), 0);
  assert.equal(keyboardInset(844, 900, -5), 0); // never below 0
  assert.ok(KEYBOARD_SETTLE_MS >= 200 && KEYBOARD_SETTLE_MS <= 600);
  // X49w review: a pinch zoom with no field in use also makes the visual viewport smaller; that is not a keyboard
  assert.equal(keyboardInset(844, 422, 0, false), 0);
  assert.equal(keyboardInset(844, 508, 0, false), 0);
  // re-check: iOS zooms in by itself on a 14 px field (scale about 16/14); the sheet still sits on the visible bottom
  assert.equal(keyboardInset(844, 445, 120, true), 279);
});

test("only a field that takes text opens the keyboard", () => {
  assert.equal(takesText({ tagName: "INPUT", type: "email" }), true);
  assert.equal(takesText({ tagName: "input" }), true);
  assert.equal(takesText({ tagName: "TEXTAREA" }), true);
  assert.equal(takesText({ tagName: "DIV", isContentEditable: true }), true);
  assert.equal(takesText({ tagName: "INPUT", type: "checkbox" }), false);
  assert.equal(takesText({ tagName: "BUTTON" }), false);
  assert.equal(takesText({ tagName: "BODY" }), false);
  assert.equal(takesText(null), false);
});

test("a field moves only with a keyboard open, and only when it is not all in view", () => {
  assert.equal(mustReveal(0, 900, 948, 844), false); // no keyboard (a desktop): never
  assert.equal(mustReveal(336, 100, 148, 508), false); // in view: no scroll (the address block keeps its place)
  assert.equal(mustReveal(336, 600, 648, 508), true); // under the keyboard
  assert.equal(mustReveal(336, -20, 28, 508), true); // above the top
});

test("the sheet, the sign-in screen and every field use it", () => {
  const ui = readFileSync(new URL("../src/ui.tsx", import.meta.url), "utf8");
  const auth = readFileSync(new URL("../src/screens/AuthFrame.tsx", import.meta.url), "utf8");
  assert.match(ui, /const kb = useKeyboardInset\(\);/);
  // the sheet sits on the keyboard; its height limit is the visible part (react-native-web takes the window height from
  // the visual viewport, so it is not made smaller twice)
  assert.match(ui, /bottom: kb, maxHeight: Math\.round\(height \* size\.sheetLarge\)/);
  assert.match(ui, /onFocus=\{\(e\) => \{ setFocused\(true\); revealOnFocus\(e\?\.target\); onFocus\?\.\(e\); \}\}/);
  assert.match(auth, /paddingBottom: kb/); // Continue stays above the keyboard
});
