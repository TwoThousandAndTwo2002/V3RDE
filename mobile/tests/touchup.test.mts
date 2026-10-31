// Tests of the touch-up rules (src/touchup.ts), written new for V3RDE (NEW_WORK_UI.md, UI 16).
import test from "node:test";
import { strict as assert } from "node:assert";
import { GIVE_UP_MS, afterPoll, photoToPost, touchError } from "../src/touchup.ts";

test("the job state after a poll", () => {
  assert.deepEqual(afterPoll({ status: "done", photo_id: "e1" }, "t", 0, 5000), { kind: "offer", id: "t", photoId: "e1", chosen: "original" });
  assert.deepEqual(afterPoll({ status: "kept_original" }, "t", 0, 5000), { kind: "kept" });
  assert.deepEqual(afterPoll({ status: "running" }, "t", 0, 5000), { kind: "working", id: "t", since: 0 });
  assert.deepEqual(afterPoll({ status: "running" }, "t", 0, GIVE_UP_MS), { kind: "failed" });
  assert.deepEqual(afterPoll({ status: "failed" }, "t", 0, 5000), { kind: "failed" });
  assert.deepEqual(afterPoll({ status: "done" }, "t", 0, 5000), { kind: "failed" }); // done with no photo
});

test("the original is the default photo", () => {
  assert.equal(photoToPost("o1", { kind: "offer", id: "t", photoId: "e1", chosen: "original" }), "o1");
  assert.equal(photoToPost("o1", { kind: "offer", id: "t", photoId: "e1", chosen: "edit" }), "e1");
  assert.equal(photoToPost("o1", { kind: "kept" }), "o1");
});

test("error words", () => {
  const w = { busy: "B", paused: "P", failed: "F" };
  assert.equal(touchError(503, "daily_cap_reached", w), "P");
  assert.equal(touchError(429, "rate_limited", w), "B");
  assert.equal(touchError(503, "busy", w), "B");
  assert.equal(touchError(404, "not_found", w), "F");
});

test("U18: a queued job is still working; the limit of one user pauses the touch-up", () => {
  assert.deepEqual(afterPoll({ status: "queued" }, "t", 0, 5000), { kind: "working", id: "t", since: 0 });
  const w = { busy: "B", paused: "P", failed: "F" };
  assert.equal(touchError(429, "caller_daily_cap_reached", w), "P");
  assert.equal(touchError(429, "rate_limited", w), "B");
});

// X48r (T9 2026-10-30: "we want one button"): one touch-up button, and the app asks for the auto touch-up.
// X49: the button is a ChipBtn ("Touch up", glyph sparkle), not a text link.
test("one touch-up button, the auto touch-up", async () => {
  const { readFileSync } = await import("node:fs");
  const screen = readFileSync(new URL("../src/screens/TouchUp.tsx", import.meta.url), "utf8");
  assert.equal((screen.match(/<ChipBtn testID=\{"touchup-"/g) ?? []).length, 1);
  assert.doesNotMatch(screen, /<TextLink /);
  assert.match(screen, /TOUCH\.start/);
  assert.doesNotMatch(screen, /TOUCH\.(light|background)/);
  const api = readFileSync(new URL("../src/api.ts", import.meta.url), "utf8");
  assert.match(api, /"\/api\/touchup\?mode=background&photo_id="/);
  const { TOUCH } = await import("../src/words.ts");
  assert.equal(TOUCH.start, "Touch up photo");
  assert.equal(TOUCH.short, "Touch up");
  assert.ok(!("light" in TOUCH) && !("background" in TOUCH));
});
