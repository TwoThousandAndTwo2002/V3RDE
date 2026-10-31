// R5 tests: the rescue rules of the app (src/rescue.ts) and the words of the popup and the pickup field.
import test from "node:test";
import { strict as assert } from "node:assert";
import { canRescue, needsPlace, placeLine, rescueView, safeLink, RESCUE_POLL_MS, RESCUE_WAIT_MS, type Rescue } from "../src/rescue.ts";
import { RESCUE, REVIEW } from "../src/words.ts";

const T = Date.parse("2026-10-31T12:00:00Z");
const at = (s: number) => new Date(T + s * 1000).toISOString();
const R = (over: Partial<Rescue>): Rescue => ({ rescue_id: "r1", listing_id: "l_1", title: "Bagels", number_of_serving: 6, places: [], ...over });

test("canRescue: an active listing whose time is over; not a live, a collected or a bad one", () => {
  assert.equal(canRescue({ status: "active", expires_at: at(-1) }, T), true);
  assert.equal(canRescue({ status: "active", expires_at: at(0) }, T), true);
  assert.equal(canRescue({ status: "active", expires_at: at(60) }, T), false);
  assert.equal(canRescue({ status: "collected", expires_at: at(-60) }, T), false);
  assert.equal(canRescue({ status: "active", expires_at: "soon" }, T), false);
});

test("rescueView: looking while running or before the first answer; places, none or failed after", () => {
  assert.equal(rescueView(null), "looking");
  assert.equal(rescueView(R({ status: "running" })), "looking");
  assert.equal(rescueView(R({ status: "done", places: [{ id: "a", name: "A" }] })), "places");
  assert.equal(rescueView(R({ status: "done" })), "none");
  assert.equal(rescueView(R({ status: "" , places: [{ id: "a", name: "A" }] })), "places"); // the one-request route has no status
  assert.equal(rescueView(R({ status: "failed", places: [{ id: "a", name: "A" }] })), "failed");
});

test("placeLine: the type words and the distance, one decimal; no distance from a web search", () => {
  assert.equal(placeLine({ id: "1", name: "x", type: "food_bank", distance_miles: 0.84 }), "Food bank · 0.8 mi");
  assert.equal(placeLine({ id: "1", name: "x", type: "pantry", distance_miles: 2 }), "Food pantry · 2 mi");
  assert.equal(placeLine({ id: "1", name: "x", type: "shelter" }), "Shelter");
  assert.equal(placeLine({ id: "1", name: "x", type: "restaurant", distance_miles: 0 }), "Food program");
});

test("safeLink: only the service's own link kinds open (no javascript:, no other site as directions)", () => {
  assert.equal(safeLink("https://www.google.com/maps/dir/?api=1&destination=A", "https"), "https://www.google.com/maps/dir/?api=1&destination=A");
  assert.equal(safeLink("https://evil.example.org/maps/dir/", "https"), "");
  assert.equal(safeLink("javascript:alert(1)", "https"), "");
  assert.equal(safeLink("tel:3035550101", "tel"), "tel:3035550101");
  assert.equal(safeLink("javascript:tel:1", "tel"), "");
  assert.equal(safeLink("mailto:?subject=x", "mailto"), "mailto:?subject=x");
  assert.equal(safeLink(undefined, "mailto"), "");
});

test("needsPlace: the service's no-place answer offers the giver's location (X49q: never a ZIP)", () => {
  assert.equal(needsPlace(400, "the pickup address has no ZIP: send zip, or lat and lng"), true);
  assert.equal(needsPlace(400, "send zip, or lat and lng"), true);
  assert.equal(needsPlace(400, "the listing timer has not run out"), false);
  assert.equal(needsPlace(502, "no ZIP"), false);
});

test("the poll waits longer than the service job (90 s), once a second", () => {
  assert.equal(RESCUE_POLL_MS, 1000);
  assert.ok(RESCUE_WAIT_MS > 90_000);
});

test("words: the one-word pill; the pickup example has no ZIP (X49q)", () => {
  assert.equal(RESCUE.pill, "Rescue");
  assert.match(RESCUE.pillName("Bagels"), /^Rescue Bagels/);
  assert.equal(REVIEW.pickup, "Pickup address and instructions");
  assert.doesNotMatch(REVIEW.pickupHint, /\d{5}/);
  for (const w of Object.values(RESCUE)) if (typeof w === "string") assert.ok(w.length > 0 && w.length <= 120, w);
});
