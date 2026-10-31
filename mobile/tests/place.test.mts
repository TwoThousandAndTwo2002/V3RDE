// Tests of the place and distance rules (src/place.ts) and their words, X49e (#74).
import test from "node:test";
import { strict as assert } from "node:assert";
import { FIRST_REACH, NoLocation, REACHES, canRelease, currentPosition, isFar, milesText, minutesLeft, parseSaved, placeFields,
  placeQuery, splitByReach, zipOf } from "../src/place.ts";
import { PLACE } from "../src/words.ts";

const T = Date.parse("2026-10-30T18:00:00Z");
const inMin = (m: number) => new Date(T + m * 60_000).toISOString();

test("the distances T9 approved: 1, 5 and 10 mi, 5 first", () => {
  assert.deepEqual([...REACHES], [1, 5, 10]);
  assert.equal(FIRST_REACH, 5);
});

test("a ZIP code is 5 digits (spaces go)", () => {
  assert.equal(zipOf(" 10001 "), "10001");
  assert.equal(zipOf("100 01"), "10001");
  for (const bad of ["", "1000", "100011", "10001-1234", "abcde"]) assert.equal(zipOf(bad), null, bad);
});

test("the place goes as lat and lng, or as zip", () => {
  assert.deepEqual(placeFields({ zip: "10001" }), [["zip", "10001"]]);
  assert.deepEqual(placeFields({ lat: 40.7506489, lng: -73.99 }), [["lat", "40.750649"], ["lng", "-73.990000"]]);
  assert.equal(placeQuery({ zip: "10001" }), "?zip=10001");
  assert.equal(placeQuery({ lat: 1, lng: 2 }, "20"), "?lat=1.000000&lng=2.000000&after=20");
});

test("the distance on a card has one decimal", () => {
  assert.equal(milesText(0.42), "0.4 mi");
  assert.equal(milesText(3.45), "3.5 mi");
  assert.equal(milesText(24.18), "24.2 mi");
  assert.equal(milesText(10), "10.0 mi");
  assert.equal(milesText(0.04), "0.1 mi or less");
  assert.equal(milesText(undefined), "");
});

test("far = farther than the distance the user picked; the order stays", () => {
  const items = [{ id: "a", distance_mi: 0.4 }, { id: "b", distance_mi: 1 }, { id: "c", distance_mi: 1.01 }, { id: "d", distance_mi: 24 }];
  assert.equal(isFar(items[1], 1), false); // exactly 1 mi is within 1 mi
  assert.equal(isFar(items[2], 1), true);
  assert.equal(isFar({}, 1), false); // no distance: not far
  const { near, far } = splitByReach(items, 1);
  assert.deepEqual(near.map((l) => l.id), ["a", "b"]);
  assert.deepEqual(far.map((l) => l.id), ["c", "d"]);
  assert.equal(splitByReach(items, 10).far.length, 1);
});

test("the minutes in the far check", () => {
  assert.equal(minutesLeft({ expires_at: inMin(12) }, T), 12);
  assert.equal(minutesLeft({ expires_at: new Date(T + 20_000).toISOString() }, T), 1); // under a minute: 1
  assert.equal(minutesLeft({ expires_at: inMin(-1) }, T), 0);
});

test("release: only the user who grabbed it, until the timer ends", () => {
  const l = { status: "collected", collected_by: "u_r", expires_at: inMin(5) };
  assert.equal(canRelease(l, "u_r", T), true);
  assert.equal(canRelease(l, "u_s", T), false);
  assert.equal(canRelease({ ...l, status: "active" }, "u_r", T), false);
  assert.equal(canRelease({ ...l, expires_at: inMin(0) }, "u_r", T), false);
});

test("the saved choice: a bad or old value is no choice", () => {
  assert.deepEqual(parseSaved(null), { choice: null, reach: 5 });
  assert.deepEqual(parseSaved("{x"), { choice: null, reach: 5 });
  assert.deepEqual(parseSaved('{"how":"location","reach":1}'), { choice: { how: "location" }, reach: 1 });
  assert.deepEqual(parseSaved('{"how":"zip","zip":"10001","reach":10}'), { choice: { how: "zip", zip: "10001" }, reach: 10 });
  assert.deepEqual(parseSaved('{"how":"zip","zip":"1","reach":3}'), { choice: null, reach: 5 });
  assert.deepEqual(parseSaved('{"how":"gps","reach":10}'), { choice: null, reach: 10 });
});

test("the location: a point, 'denied' when the user says no, 'unavailable' without a location", async () => {
  const ok = { getCurrentPosition: (yes: (p: { coords: { latitude: number; longitude: number } }) => void) =>
    yes({ coords: { latitude: 40.75, longitude: -74 } }) };
  assert.deepEqual(await currentPosition(ok), { lat: 40.75, lng: -74 });
  const no = (code: number) => ({ getCurrentPosition: (_y: unknown, bad: (e: { code: number }) => void) => bad({ code }) });
  await assert.rejects(currentPosition(no(1)), (e: unknown) => e instanceof NoLocation && e.code === "denied");
  await assert.rejects(currentPosition(no(3)), (e: unknown) => e instanceof NoLocation && e.code === "unavailable");
  await assert.rejects(currentPosition(undefined), (e: unknown) => e instanceof NoLocation && e.code === "unavailable");
});

test("the place words are the approved mockup words, each sentence 20 words or fewer", () => {
  assert.equal(PLACE.farTitle(milesText(3.4)), "This food is 3.4 mi away");
  assert.equal(PLACE.farLine(1, 12), "It is farther than your 1 mi. The timer ends in 12 min. Grab it only if you can get there in time.");
  assert.equal(PLACE.farther(1), "Farther than 1 mi");
  assert.equal(PLACE.releaseTitle("Roma tomatoes"), "Release Roma tomatoes?");
  assert.equal(PLACE.emptyNear(1), "Nothing within 1 mi now. Look again in a few minutes.");
  for (const v of Object.values(PLACE)) {
    const text = typeof v === "string" ? v : (v as (a: unknown, b?: unknown) => string)(10, 30);
    for (const sentence of text.split(/[.?!] /)) assert.ok(sentence.split(/\s+/).length <= 20, text);
  }
});
