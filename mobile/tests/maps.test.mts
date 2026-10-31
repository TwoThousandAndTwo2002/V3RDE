// Tests of the pickup address rules (src/maps.ts) and its words, X49j (#80).
import test from "node:test";
import { strict as assert } from "node:assert";
import { MAPS_LOAD_MS, MAP_ZOOM, SUGGEST_MIN, addressLines, firstPrediction, linesOf, scriptUrl, zipOf } from "../src/maps.ts";
import { PICKUP, grabMessage, pickupLine } from "../src/words.ts";
import { listingProblem } from "../src/capture.ts";

test("the Maps script URL: the key, the weekly channel, async loading, the callback", () => {
  assert.equal(scriptUrl("k&1", "cb"), "https://maps.googleapis.com/maps/api/js?key=k%261&v=weekly&loading=async&callback=cb");
  assert.equal(MAP_ZOOM, 16);
  assert.equal(SUGGEST_MIN, 4);
});

test("the ZIP of the address components: postal_code, 5 digits", () => {
  const comps = [{ longText: "Denver", types: ["locality"] }, { longText: "80219", shortText: "80219", types: ["postal_code"] }];
  assert.equal(zipOf(comps), "80219");
  assert.equal(zipOf([{ longText: "80219-1234", types: ["postal_code"] }]), "");
  assert.equal(zipOf([{ longText: "SW1A 1AA", types: ["postal_code"] }]), "");
  assert.equal(zipOf(undefined), "");
});

test("one suggestion only (T9): the first place prediction, in two lines", () => {
  const p1 = { mainText: { text: "12 Main St" }, secondaryText: { text: "Denver, CO, USA" }, toPlace: () => null };
  const p2 = { mainText: { text: "12 Main Ave" }, secondaryText: { text: "Denton, TX, USA" }, toPlace: () => null };
  assert.equal(firstPrediction([{ placePrediction: null }, { placePrediction: p1 }, { placePrediction: p2 }]), p1);
  assert.equal(firstPrediction([]), null);
  assert.equal(firstPrediction(undefined), null);
  assert.deepEqual(linesOf(p1), { main: "12 Main St", secondary: "Denver, CO, USA" });
  assert.deepEqual(linesOf({ text: { text: "Somewhere" }, toPlace: () => null }), { main: "Somewhere", secondary: "" });
});

test("the locked address in two lines", () => {
  assert.deepEqual(addressLines("12 Main St, Denver, CO 80219, USA"), { main: "12 Main St", rest: "Denver, CO 80219, USA" });
  assert.deepEqual(addressLines("Main St"), { main: "Main St", rest: "" });
});

test("with an address the instructions may be empty; the receiver gets the address, then the instructions", () => {
  assert.equal(listingProblem("Soup", 2, "", true), "");
  assert.notEqual(listingProblem("Soup", 2, ""), "");
  assert.notEqual(listingProblem("Soup", 2, "x".repeat(201), true), "");
  const l = { title: "Soup", number_of_serving: 2, pickup_text: "front desk", pickup_address: { text: "12 Main St, Denver, CO 80219, USA" } };
  assert.equal(pickupLine(l), "12 Main St, Denver, CO 80219, USA. front desk");
  assert.equal(pickupLine({ pickup_text: "Back door" }), "Back door");
  assert.equal(pickupLine({ pickup_text: "", pickup_address: { text: "12 Main St" } }), "12 Main St");
  assert.match(grabMessage(l, "a@b.c"), /Pickup: 12 Main St, Denver, CO 80219, USA\. front desk\./);
  for (const v of Object.values(PICKUP)) {
    const text = typeof v === "string" ? v : (v as (a: string) => string)("12 Main St");
    for (const sentence of text.split(/[.?!] /)) assert.ok(sentence.split(/\s+/).length <= 20, text);
  }
});

test("the screens: the review asks the config, the address comes first; a card before a grab never shows the address", async () => {
  const fs = await import("node:fs");
  const review = fs.readFileSync(new URL("../src/screens/Review.tsx", import.meta.url), "utf8");
  const receive = fs.readFileSync(new URL("../src/screens/Receive.tsx", import.meta.url), "utf8");
  const api = fs.readFileSync(new URL("../src/api.ts", import.meta.url), "utf8");
  assert.match(review, /listings\.config\(\)/);
  assert.match(review, /if \(byAddress && !picked\) return setProblem\(PICKUP\.need\)/);
  assert.match(review, /address: byAddress && picked \? \{ text: picked\.text, zip: picked\.zip \} : undefined/);
  assert.match(api, /form\.append\("address", n\.address\.text\)/);
  // the feed card: the instructions only; the address only in "Your pickup" and the grab sheet
  assert.match(receive, /\{l\.pickup_text \? <Muted lines=\{1\}/);
  assert.equal((receive.match(/pickupLine\(/g) ?? []).length, 2);
});

// X49j review: Maps can fail (no script, a refused key, the search fails): the review goes back to the place row.
test("a Maps failure gives the place row back; a ZIP error with an address is not the ZIP row", async () => {
  const fs = await import("node:fs");
  const review = fs.readFileSync(new URL("../src/screens/Review.tsx", import.meta.url), "utf8");
  const pickup = fs.readFileSync(new URL("../src/screens/Pickup.tsx", import.meta.url), "utf8");
  assert.equal(MAPS_LOAD_MS, 10_000);
  assert.match(review, /onFail=\{\(\) => \{ setMapsKey\(""\); setPicked\(null\); \}\}/);
  assert.match(review, /if \(!byAddress && err instanceof ApiError && err\.status === 400 && \/zip\/i/);
  assert.match(pickup, /loadMaps\(mapsKey\)\.catch\(\(\) => \{ if \(on\) onFail\(\); \}\)/);
  assert.match(pickup, /testID="pickup-use-location" onPress=\{onFail\}/);
  assert.match(pickup, /height: size\.mapH \}\} \/>/); // the map element has its own size
  assert.equal(PICKUP.useLocation, "Use my location instead");
});
