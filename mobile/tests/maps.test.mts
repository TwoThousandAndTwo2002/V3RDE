// Tests of the pickup address rules (src/maps.ts) and its words, X49j (#80).
import test from "node:test";
import { strict as assert } from "node:assert";
import { ADDRESS_FOLLOW_M, MAPS_LOAD_MS, NAME_LIMIT_MS, NAME_WAIT_MS, MAP_ZOOM, SUGGEST_MIN, addressLines, firstPrediction, linesOf, metersBetween, scriptUrl } from "../src/maps.ts";
import { PICKUP, VOICE, grabMessage, pickupLine } from "../src/words.ts";
import { listingProblem } from "../src/capture.ts";

test("the Maps script URL: the key, the weekly channel, async loading, the callback", () => {
  assert.equal(scriptUrl("k&1", "cb"), "https://maps.googleapis.com/maps/api/js?key=k%261&v=weekly&loading=async&callback=cb");
  assert.equal(MAP_ZOOM, 16);
  assert.equal(SUGGEST_MIN, 4);
});

test("X49q: no ZIP anywhere: the address search has no country limit and asks for no address components", async () => {
  const fs = await import("node:fs");
  const maps = fs.readFileSync(new URL("../src/maps.ts", import.meta.url), "utf8");
  assert.doesNotMatch(maps, /includedRegionCodes/);
  assert.match(maps, /fetchFields\(\{ fields: \["formattedAddress", "location"\] \}\)/);
  for (const f of ["../src/maps.ts", "../src/place.ts", "../src/rescue.ts", "../src/api.ts", "../src/screens/Review.tsx",
    "../src/screens/Receive.tsx", "../src/screens/Rescue.tsx"]) {
    const src = fs.readFileSync(new URL(f, import.meta.url), "utf8").split("\n").filter((l) => !/^\s*(\/\/|\*|\/\*\*)/.test(l)).map((l) => l.replace(/\s\/\/ .*$/, "")).join("\n");
    assert.doesNotMatch(src, /zipOf|address_zip|"zip"|\{ zip|PLACE\.\w*[zZ]ip|RESCUE\.zip/, f);
  }
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
  assert.match(review, /if \(byAddress && !picked\) \{ setProblem\(""\); setNeedSeq\(\(n\) => n \+ 1\); return setNeedAddress\(true\); \}/);
  assert.match(review, /address: byAddress && picked \? \{ text: picked\.text \} : undefined/);
  assert.match(api, /form\.append\("address", n\.address\.text\)/);
  // the feed card: the instructions only; the address only in "Your pickup" and the grab sheet
  assert.match(receive, /\{l\.pickup_text \? <Muted lines=\{1\}/);
  assert.equal((receive.match(/pickupLine\(/g) ?? []).length, 2);
});

// X49j review: Maps can fail (no script, a refused key, the search fails): the review goes back to the place row.
test("a Maps failure: the location at Post; X49q: the map moves, the current location, the error under the field", async () => {
  const fs = await import("node:fs");
  const review = fs.readFileSync(new URL("../src/screens/Review.tsx", import.meta.url), "utf8");
  const pickup = fs.readFileSync(new URL("../src/screens/Pickup.tsx", import.meta.url), "utf8");
  assert.equal(MAPS_LOAD_MS, 10_000);
  assert.match(review, /onFail=\{\(\) => \{ setMapsKey\(""\); setPicked\(null\); \}\}/);
  assert.match(review, /return setProblem\(PLACE\.offPost\)/);
  assert.match(pickup, /loadMaps\(mapsKey\)\.catch\(\(\) => \{ if \(on\) onFail\(\); \}\)/);
  assert.match(pickup, /height: h \}\} \/>/); // the map element has its own size
  assert.match(pickup, /const h = tall \? size\.mapTall : size\.mapH;/);
  assert.match(pickup, /<IconBtn testID="pickup-here" glyph="locate" label=\{finding \? PICKUP\.finding : PICKUP\.here\} onPress=\{here\} \/>/);
  assert.match(pickup, /onChangeText=\{setText\} right=\{hereBtn\} \/>/); // X49w: the icon is in the address field
  assert.doesNotMatch(pickup, /TextLink/);
  assert.match(pickup, /need \? <ErrorText>\{PICKUP\.need\}<\/ErrorText>/);
  assert.match(review, /setNeedAddress\(true\)/);
  assert.doesNotMatch(review, /PLACE\.hereNow/);
  const maps = fs.readFileSync(new URL("../src/maps.ts", import.meta.url), "utf8");
  assert.match(maps, /gestureHandling: "greedy"/);
  assert.match(maps, /importLibrary\("geocoding"\)/);
  assert.equal(PICKUP.here, "Use my current location");
});

// X49q (T9 2026-10-31): the hints take turns in the empty chat box; each fits the narrow desktop box.
test("the chat box hints: three, short, the first is the old hint; they turn every 4 s while the chat is empty", async () => {
  const fs = await import("node:fs");
  const voice = fs.readFileSync(new URL("../src/screens/Voice.tsx", import.meta.url), "utf8");
  assert.equal(VOICE.hints.length, 3);
  assert.equal(VOICE.hints[0], VOICE.hint);
  for (const h of VOICE.hints) assert.ok(h.length <= 22, h);
  assert.match(voice, /HINT_SWAP_MS = 4000/);
  assert.match(voice, /placeholder=\{turns\.length === 0 \? VOICE\.hints\[tip\] : VOICE\.hint\}/);
  assert.doesNotMatch(voice, /VOICE\.examples/);
  assert.match(voice, /if \(turns\.length > 0 \|\| !empty\) return;/); // no turns while the user types
});

// X49q (T9 2026-10-31): the address follows the pin when it moved more than 20 m; a tap on the address box changes it.
test("the address follows the pin: metres between two points, the 20 m rule, the box is the change button", async () => {
  assert.equal(ADDRESS_FOLLOW_M, 20);
  assert.equal(NAME_WAIT_MS, 500);
  assert.equal(NAME_LIMIT_MS, 8000);
  assert.equal(metersBetween({ lat: 6.455, lng: 3.3941 }, { lat: 6.455, lng: 3.3941 }), 0);
  const d = metersBetween({ lat: 0, lng: 0 }, { lat: 0.001, lng: 0 }); // 0.001 degree of latitude
  assert.ok(d > 111 && d < 112, String(d));
  assert.ok(metersBetween({ lat: 6.4550, lng: 3.3941 }, { lat: 6.45515, lng: 3.3941 }) < ADDRESS_FOLLOW_M); // 16.7 m
  const fs = await import("node:fs");
  const pickup = fs.readFileSync(new URL("../src/screens/Pickup.tsx", import.meta.url), "utf8");
  assert.doesNotMatch(pickup, /pickup-address-change/);
  assert.match(pickup, /testID="pickup-address-box"/);
  assert.match(pickup, /metersBetween\(named\.current, p\) <= ADDRESS_FOLLOW_M\) \{ name\(false\); return; \}/);
  assert.match(pickup, /document\.addEventListener\("click", outside, true\)/); // a scroll is no tap
  // X49q review: one map for both modes; it holds still while posting; every move cancels a lookup; Post waits
  assert.equal((pickup.match(/<MapBox /g) ?? []).length, 1);
  assert.match(pickup, /tall=\{editing\} still=\{locked\}/);
  assert.match(pickup, /const r = \+\+geoRound\.current;\n    if \(named\.current && metersBetween/);
  assert.match(pickup, /if \(typed\.current === was\) setText\(t\)/);
  assert.match(pickup, /found \|\| p\.lat\.toFixed\(5\)/);
  assert.match(pickup, /Promise\.race\(\[addressAt\(mapsKey, p\)\.catch\(\(\) => ""\), late\]\)/);
  assert.match(pickup, /useEffect\(\(\) => \(\) => \{ geoRound\.current\+\+; \}, \[\]\)/);
  const review = fs.readFileSync(new URL("../src/screens/Review.tsx", import.meta.url), "utf8");
  assert.match(review, /if \(byAddress && naming\) return setProblem\(PICKUP\.naming\)/);
  assert.match(review, /onPicked=\{\(a\) => \{ setPicked\(a\); setNeedAddress\(false\); \}\}/);
  const maps = fs.readFileSync(new URL("../src/maps.ts", import.meta.url), "utf8");
  assert.match(maps, /gestureHandling: still \? "none" : "greedy"/);
  assert.match(pickup, /scrollIntoView\?\.\(\{ block: "start"/);
  assert.match(pickup, /onPicked\(\{ text: t, lat: p\.lat, lng: p\.lng \}\)/); // the found address keeps the point of its move
});

// X49w (T9 2026-10-31): Post with no address scrolls the page to the address field, each time.
test("Post with no address brings the address field into view", async () => {
  const fs = await import("node:fs");
  const review = fs.readFileSync(new URL("../src/screens/Review.tsx", import.meta.url), "utf8");
  const pickup = fs.readFileSync(new URL("../src/screens/Pickup.tsx", import.meta.url), "utf8");
  assert.match(review, /need=\{needAddress\} needSeq=\{needSeq\}/);
  assert.match(pickup, /const seenNeed = useRef\(needSeq\);/);
  assert.match(pickup, /if \(needSeq === seenNeed\.current \|\| Platform\.OS !== "web"\) return;\s*seenNeed\.current = needSeq;\s*\(block\.current[^;]*scrollIntoView\?\.\(\{ block: "center", behavior: "smooth" \}\);\s*\}, \[needSeq\]\);/);
});
