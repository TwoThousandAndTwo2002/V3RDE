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
