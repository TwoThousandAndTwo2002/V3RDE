// Tests of the listing rules (src/listing.ts), written new for V3RDE (NEW_WORK_UI.md, UI 12).
import test from "node:test";
import { strict as assert } from "node:assert";
import { addPage, cameraFirst, canDelete, clock, isGone, loadOthers, mapLink, othersOnly, secondsLeft, timeText, withChange } from "../src/listing.ts";

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

test("othersOnly (U19): Receive shows no listing of the user", () => {
  const a = { ...L("a", 60), owner_id: "u1" }, b = { ...L("b", 60), owner_id: "u2" }, c = L("c", 60);
  assert.deepEqual(othersOnly([a, b, c], "u1").map((l) => l.id), ["b", "c"]);
  assert.deepEqual(othersOnly([a, b, c], "").map((l) => l.id), ["a", "b", "c"]);
  assert.deepEqual(othersOnly([a, b, c], undefined).map((l) => l.id), ["a", "b", "c"]);
});

test("loadOthers (U19): pages of only own listings do not stop the feed", async () => {
  const own = (id: string) => ({ ...L(id, 60), owner_id: "me" }), other = (id: string) => ({ ...L(id, 60), owner_id: "x" });
  const pages: Record<string, { items: any[]; next: string }> = {
    "": { items: [own("a"), own("b")], next: "b" }, b: { items: [own("c")], next: "c" }, c: { items: [other("d")], next: "d" },
    d: { items: [other("e")], next: "" },
  };
  const asked: string[] = [];
  const page = async (after: string) => { asked.push(after); return pages[after]; };
  const got = await loadOthers(page, [], "", "me");
  assert.deepEqual(asked, ["", "b", "c"], "three pages in one load: the third brings food of others");
  assert.deepEqual(got!.items.map((l) => l.id), ["a", "b", "c", "d"]);
  assert.equal(got!.next, "d");
  // the end of the feed stops it; max stops it; a refresh makes it stale
  const all = await loadOthers(async () => ({ items: [own("z")], next: "" }), [], "", "me");
  assert.deepEqual(all, { items: [{ ...L("z", 60), owner_id: "me" }], next: "" });
  let n = 0;
  const loop = await loadOthers(async () => ({ items: [own("p" + n++)], next: "more" }), [], "", "me", () => false, 3);
  assert.equal(n, 3);
  assert.equal(loop!.next, "more");
  assert.equal(await loadOthers(page, [], "", "me", () => true), null);
  // no user id: one page, as before
  asked.length = 0;
  await loadOthers(page, [], "", undefined);
  assert.deepEqual(asked, [""]);
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
});

test("Give opens the camera first only when nothing of the giver is up", () => {
  assert.equal(cameraFirst([], T), true);
  assert.equal(cameraFirst([L("a", -1), L("b", 9, "collected")], T), true);
  assert.equal(cameraFirst([L("a", 9)], T), false);
});

// X48t: delete before a grab only; the words of the delete flow.
test("delete: only before a grab", async () => {
  assert.equal(canDelete({ status: "active" }), true);
  assert.equal(canDelete({ status: "collected" }), false);
  const { GIVE } = await import("../src/words.ts");
  assert.equal(GIVE.confirm("Ripe bananas"), "Delete \u201cRipe bananas\u201d?");
  assert.equal(GIVE.removeName("Ripe bananas"), "Delete Ripe bananas");
  const ui = (await import("node:fs")).readFileSync(new URL("../src/screens/Give.tsx", import.meta.url), "utf8");
  assert.match(ui, /<Popup testID="delete-confirm" small /); // T9: the small popup class on a phone
});

// X49h (T9 2026-10-31): Delete is an X in a frosted round button on the photo; Rescue is a pill the size of the time pill.
test("Give cards: Delete on the photo, a small Rescue pill", async () => {
  const fs = await import("node:fs");
  const give = fs.readFileSync(new URL("../src/screens/Give.tsx", import.meta.url), "utf8");
  const ui = fs.readFileSync(new URL("../src/ui.tsx", import.meta.url), "utf8");
  assert.match(give, /<FrostBtn testID=\{"delete-" \+ l\.id\} glyph="close"/);
  assert.match(give, /corner=\{del\(l\)\}/);
  assert.match(give, /<SmallPill testID=\{"rescue-" \+ l\.id\}/);
  assert.doesNotMatch(give, /<TextLink /); // the old underlined "Delete" is gone
  assert.doesNotMatch(give, /<Pill testID=\{"rescue-/); // the big pill is gone
  // the small pill has the size and the text style of the time pill
  const pill = ui.slice(ui.indexOf("export function SmallPill"), ui.indexOf("export function SmallPill") + 900);
  const time = ui.slice(ui.indexOf("export function TimePill"), ui.indexOf("export function TimePill") + 700);
  for (const part of ["width: size.timeW", "paddingVertical: size.timePadY", "type.button"]) {
    assert.ok(pill.includes(part) && time.includes(part), part);
  }
  // the frost layers take the corners of the part (a round button showed a square blur)
  assert.match(ui, /style=\{\[StyleSheet\.absoluteFill, round\]\} \/>/);
});

// X60 (T9 2026-11-01: "add this, reset the timer, change the name"): the popup sends only what changed; a bad field is
// a problem text; nothing changed is null; an ended listing always sends reset ("post again").
test("listingChange: the owner's change of a posted listing", async () => {
  const { listingChange } = await import("../src/listing.ts");
  const { FORM_WORDS } = await import("../src/capture.ts");
  const live = { title: "Rice", number_of_serving: 8, status: "active", expires_at: at(600) };
  assert.equal(listingChange(live, " Rice ", "8", false, T), null);
  assert.deepEqual(listingChange(live, " Rice, family box ", "10", false, T), { change: { title: "Rice, family box", number_of_serving: 10 } });
  assert.deepEqual(listingChange(live, "Rice", "8", true, T), { change: { reset: true } });
  assert.deepEqual(listingChange(live, "  ", "8", true, T), { problem: FORM_WORDS.title });
  assert.deepEqual(listingChange(live, "x".repeat(81), "8", false, T), { problem: FORM_WORDS.title });
  for (const n of ["0", "100", "", "2.5"]) assert.deepEqual(listingChange(live, "Rice", n, false, T), { problem: FORM_WORDS.servings }, n);
  const ended = { ...live, expires_at: at(-60) };
  assert.deepEqual(listingChange(ended, "Rice", "8", false, T), { change: { reset: true } });
  assert.deepEqual(listingChange(ended, "Rice", "3", false, T), { change: { number_of_serving: 3, reset: true } });
});

test("Give: a pencil next to the X opens the change popup (post again on an ended card)", async () => {
  const fs = await import("node:fs");
  const give = fs.readFileSync(new URL("../src/screens/Give.tsx", import.meta.url), "utf8");
  const api = fs.readFileSync(new URL("../src/api.ts", import.meta.url), "utf8");
  const { GIVE } = await import("../src/words.ts");
  assert.match(give, /<FrostBtn testID=\{"edit-" \+ l\.id\} glyph="pencil" label=\{GIVE\.editName\(l\.title\)\}/);
  assert.match(give, /canDelete\(l\) \? \(\s*<Row tight>/); // only before a grab, with the X
  assert.match(give, /<Popup testID="listing-edit" small /);
  assert.match(give, /again \? GIVE\.againTitle : GIVE\.editTitle/);
  assert.match(give, /again \? null : \(\s*<Row[^>]*>\s*<ChipBtn testID="edit-reset"/); // no chip when it ended: reset is always on
  assert.match(give, /listings\.update\(l\.id, asked\.change\)/);
  assert.match(api, /request<Listing>\("PATCH", "\/api\/listings\/" \+ encodeURIComponent\(id\), \{ json: change \}\)/);
  assert.equal(GIVE.editName("Rice"), "Change Rice");
  assert.equal(GIVE.timerReset, "Reset the timer to 30 minutes");
  assert.equal(GIVE.changeGrabbed, "Someone is picking this up. It cannot be changed.");
  assert.equal(GIVE.changeEnded, "It ended. Tap the pencil to post it again.");
  assert.match(give, /ended409 \? GIVE\.changeEnded : conflict \? GIVE\.changeGrabbed/); // X60 review: an ended 409 is not a grab
});

// X63 (T9 2026-11-02: "we need to take whats app off or use it one or the other"; Maps only): after a grab the
// sheet and the "Your pickup" card have one pill, Maps, as the primary pill; no WhatsApp link anywhere in the app.
test("after a grab: Maps only, no WhatsApp", async () => {
  const fs = await import("node:fs");
  const path = await import("node:path");
  const recv = fs.readFileSync(new URL("../src/screens/Receive.tsx", import.meta.url), "utf8");
  assert.match(recv, /<Pill testID="maps" title=\{RECEIVE\.maps\} grow /);
  assert.match(recv, /<Pill testID=\{"maps-" \+ l\.id\} title=\{RECEIVE\.maps\} grow /);
  assert.doesNotMatch(recv, /kind="secondary" title=\{RECEIVE\.maps\}/, "Maps is the one pill, so it is the primary pill");
  const src = new URL("../src/", import.meta.url).pathname;
  const walk = (d: string): string[] => fs.readdirSync(d).flatMap((n) => {
    const p = path.join(d, n);
    return fs.statSync(p).isDirectory() ? walk(p) : [p];
  });
  for (const f of walk(src)) assert.doesNotMatch(fs.readFileSync(f, "utf8"), /wa\.me|WhatsApp/i, f);
  const { RECEIVE } = await import("../src/words.ts");
  assert.ok(!("chat" in RECEIVE));
});
