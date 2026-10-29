// Tests of the listing rules (src/listing.ts), written new for V3RDE (NEW_WORK_UI.md, UI 12).
import test from "node:test";
import { strict as assert } from "node:assert";
import { addPage, cameraFirst, chatLink, clock, isGone, mapLink, secondsLeft, timeText, withChange } from "../src/listing.ts";

const T = Date.parse("2026-10-29T12:00:00Z");
const at = (s: number) => new Date(T + s * 1000).toISOString();
const L = (id: string, s: number, status = "active") => ({ id, title: id, number_of_serving: 1, pickup_text: "p", status, expires_at: at(s), photo_url: "" });

test("time left and the clock", () => {
  assert.equal(secondsLeft(L("a", 90), T), 90);
  assert.equal(secondsLeft(L("a", -5), T), 0);
  assert.equal(secondsLeft({ expires_at: "not a time" }, T), 0);
  assert.equal(clock(90), "1:30");
  assert.equal(clock(5), "0:05");
  assert.equal(clock(3725), "1:02:05");
});

test("gone: collected or out of time", () => {
  assert.equal(isGone(L("a", 60), T), false);
  assert.equal(isGone(L("a", 60, "collected"), T), true);
  assert.equal(isGone(L("a", 0), T), true);
  const w = { collected: "Collected", expired: "Expired" };
  assert.equal(timeText(L("a", 61), w, T), "1:01");
  assert.equal(timeText(L("a", -1), w, T), "Expired");
  assert.equal(timeText(L("a", 61, "collected"), w, T), "Collected");
});

test("a new page adds only the listings not shown", () => {
  const r = addPage([L("a", 9), L("b", 9)], { items: [L("b", 9), L("c", 9), L("c", 9)], next: "x" });
  assert.deepEqual(r.items.map((l) => l.id), ["a", "b", "c"]);
  assert.equal(r.next, "x");
  assert.equal(addPage([], {}).next, "");
});

test("a grab changes only its listing", () => {
  const r = withChange([L("a", 9), L("b", 9)], { id: "b", status: "collected" });
  assert.deepEqual(r.map((l) => l.status), ["active", "collected"]);
});

test("links", () => {
  assert.equal(mapLink(" 12 Main St "), "https://www.google.com/maps/search/?api=1&query=12%20Main%20St");
  assert.equal(chatLink("Hi & bye"), "https://wa.me/?text=Hi%20%26%20bye");
});

test("Give opens the camera first only when nothing of the giver is up", () => {
  assert.equal(cameraFirst([], T), true);
  assert.equal(cameraFirst([L("a", -1), L("b", 9, "collected")], T), true);
  assert.equal(cameraFirst([L("a", 9)], T), false);
});
