// Tests of src/logic.ts, written new for V3RDE (NEW_WORK_UI.md, UI 4).
import { test } from "node:test";
import { strict as assert } from "node:assert";
import {
  answerToItems, labelReport, diffEdits, addEdits, clipBytes, fitReport, deviceLocale, analyzeKind, retryText, NO_ITEMS_TEXT, CLIENT, sendAgain, SEND_TRIES, SEND_AGAIN_MS, plural, foundTitle, postTitle, postingText,
  notPostedTitle, postedSummary, itemPhotoPath, UPLOAD, streamTarget, sameTurn, targetSize, joinUrl, errorMessage,
  scanStep, scanPoll, itemDetails, notPostedItems, stillOpen, SCAN_HOLD_MS, SCAN_POLL_MS,
} from "../src/logic.ts"; // run with Node type stripping

const ID = "0123456789abcdef0123456789abcdef";

test("answerToItems: one card per item, servings clamped to 1-99, title cut at 80", () => {
  const items = answerToItems({
    items: [
      { item: 1, fields: { name: "Bananas", number_of_serving: 6, description: "ripe" }, photo_id: ID },
      { item: 2, fields: { name: "x".repeat(90), number_of_serving: 250 } },
      { fields: { name: "Rice", number_of_serving: 0 } },
    ],
  });
  assert.equal(items.length, 3);
  assert.deepEqual(items[0], { item: 1, title: "Bananas", servings: "6", photo_id: ID, description: "ripe", err: "",
    details: { description: "ripe" } });
  assert.equal(items[1].title.length, 80);
  assert.equal(items[1].servings, "99");
  assert.equal(items[1].photo_id, "");
  assert.equal(items[2].item, 3);
  assert.equal(items[2].servings, "1");
});

test("answerToItems (R2): a barcode answer keeps no meal_condition (a fixed value); a vision answer keeps it", () => {
  const f = { name: "Yogurt", number_of_serving: 1, description: "Strawberry yogurt", serving_type: "individual",
    meal_condition: "ready_to_eat", weight: 0.15 };
  const bar = answerToItems({ source: "barcode", items: [{ item: 1, fields: f, photo_id: ID }] });
  assert.deepEqual(bar[0].details, { description: "Strawberry yogurt", serving_type: "individual", weight: 0.15 });
  const vis = answerToItems({ source: "vision", items: [{ item: 1, fields: f, photo_id: ID }] });
  assert.equal(vis[0].details?.meal_condition, "ready_to_eat");
  assert.equal(answerToItems({ items: [{ item: 1, fields: f }] })[0].details?.meal_condition, "ready_to_eat");
  // a barcode answer with only the fixed value has no details at all
  assert.equal(answerToItems({ source: "barcode", items: [{ fields: { name: "Chips", meal_condition: "ready_to_eat" } }] })[0].details, undefined);
  assert.equal(f.meal_condition, "ready_to_eat"); // the answer itself is not changed
});

test("answerToItems (I1): in a photo with codes and other food, a catalog item keeps no meal_condition; a vision item keeps it", () => {
  const f = { name: "Yogurt", meal_condition: "ready_to_eat", weight: 0.15 };
  const mixed = answerToItems({ source: "vision", items: [{ item: 1, fields: f, photo_id: ID, source: "barcode" }, { item: 2, fields: f, photo_id: ID }] });
  assert.deepEqual(mixed[0].details, { weight: 0.15 });
  assert.equal(mixed[1].details?.meal_condition, "ready_to_eat");
});

test("itemDetails (X48d): the scanner's fields that the listings service takes; a wrong one is left out", () => {
  const f = { name: "Lasagna", number_of_serving: 6, description: " Baked pasta ", is_non_veg: 1, serving_type: "bulk",
    meal_condition: "ready_to_eat", weight: 2.5, meal_cost: 0 };
  assert.deepEqual(itemDetails(f), { description: "Baked pasta", is_non_veg: 1, serving_type: "bulk",
    meal_condition: "ready_to_eat", weight: 2.5, meal_cost: 0 });
  assert.deepEqual(itemDetails({ is_non_veg: 2, serving_type: "tray", meal_condition: "hot", weight: -1, meal_cost: Infinity,
    description: "x".repeat(250) }), { description: "x".repeat(200) });
  assert.equal(itemDetails({ name: "Rice", description: "  " }), undefined);
  assert.equal(itemDetails(null), undefined);
  assert.equal(itemDetails({ weight: "2" }), undefined);
  // code points, not UTF-16 units: 250 emoji give 200; a control character is a space
  assert.equal(Array.from(itemDetails({ description: "🍕".repeat(250) })!.description!).length, 200);
  assert.equal(itemDetails({ description: "a\nb" })!.description, "a b");
});

test("answerToItems: no items array gives no cards", () => {
  assert.deepEqual(answerToItems(null), []);
  assert.deepEqual(answerToItems({ items: "no" }), []);
});

test("labelReport: posted items carry the user's final title and servings; removed items are not kept", () => {
  const answer = answerToItems({ items: [{ item: 1, fields: { name: "Soup", number_of_serving: 2 } },
    { item: 2, fields: { name: "Bread", number_of_serving: 1 } }] });
  const final = [{ ...answer[0], title: " Tomato soup ", servings: "4" }];
  const r = labelReport("req1", answer, final, "posted", "en-US");
  assert.equal(r.request_id, "req1");
  assert.equal(r.outcome, "posted");
  assert.equal(r.locale, "en-US");
  assert.deepEqual(r.items[0], { item: 1, photo_id: "", model_title: "Soup", final_title: "Tomato soup",
    model_servings: 2, final_servings: 4, kept: true });
  assert.equal(r.items[1].kept, false);
  assert.equal(r.items[1].final_title, "Bread");
});

test("labelReport: a retake keeps nothing", () => {
  const answer = answerToItems({ items: [{ item: 1, fields: { name: "Soup", number_of_serving: 2 } }] });
  assert.equal(labelReport("r", answer, answer, "retake").items[0].kept, false);
});

test("deviceLocale: a language tag, or empty", () => {
  assert.equal(deviceLocale(() => "es-MX"), "es-MX");
  assert.equal(deviceLocale(() => "not a tag!"), "");
  assert.equal(deviceLocale(() => { throw new Error("no Intl"); }), "");
});

test("analyzeKind: each error code picks its sheet; any other answer is a retry", () => {
  const cases: Array<[string, string]> = [["not_food", "not_food"], ["model_no_food_single", "nofood_single"],
    ["model_no_food_multi", "nofood_multi"], ["already_listed", "already_listed"], ["daily_cap_reached", "daily_cap"],
    ["gate_unavailable", "unavailable"], ["busy", "retry"], ["rate_limited", "retry"]];
  for (const [code, kind] of cases) assert.equal(analyzeKind({ code }), kind, code);
  assert.equal(analyzeKind(null), "retry");
});

test("retryText: the photo is always kept", () => {
  assert.match(retryText(503, { code: "busy" }), /^The scanner is busy\./);
  assert.match(retryText(429, {}), /^Too many photos this minute\./);
  assert.match(retryText(504, { code: "timeout" }), /^This photo took too long\./);
  assert.equal(retryText(400, { error: "bad photo" }), "bad photo — your photo is kept.");
  assert.equal(retryText(418, null), "Request failed (418) — your photo is kept.");
});

// X49r (T9 2026-10-31, a tester's scans): "the food scanner is busy" was the text of every failure.
test("retryText: one cause, one sentence", () => {
  const kept = / — your photo is kept\.$/;
  const cases: Array<[number, any, RegExp]> = [
    [502, { code: "upstream_error" }, /^The scanner could not read this photo\. Try again, or retake it closer/],
    [0, { code: CLIENT.offline }, /^No connection\. Check your internet, then try again/],
    [0, { code: CLIENT.timeout }, /^No answer in time\. Check your internet, then try again/],
    [0, { code: CLIENT.lost }, /^The scan stopped\. Try again/],
    [0, null, /^The scan stopped\. Try again/],
    [500, { code: "internal" }, /^Something went wrong on our side\. Try again/],
    [503, null, /^Something went wrong on our side\./],
  ];
  for (const [status, body, want] of cases) {
    assert.match(retryText(status, body), want);
    assert.match(retryText(status, body), kept);
    assert.doesNotMatch(retryText(status, body), /busy/);
  }
  assert.doesNotMatch(NO_ITEMS_TEXT, /busy/);
  assert.match(NO_ITEMS_TEXT, /^The scanner found no items in this photo\./);
});

test("sendAgain: only a lost connection, with time left", () => {
  const now = 1_000_000, until = now + 60_000;
  assert.equal(sendAgain(CLIENT.offline, 1, now, until, false), true);
  assert.equal(sendAgain(CLIENT.offline, SEND_TRIES - 1, now, until, false), true);
  assert.equal(sendAgain(CLIENT.offline, SEND_TRIES, now, until, false), false); // the last try
  assert.equal(sendAgain(CLIENT.timeout, 1, now, until, false), false); // a slow upload is not sent twice
  assert.equal(sendAgain("upstream_error", 1, now, until, false), false);
  assert.equal(sendAgain(CLIENT.offline, 1, until - SEND_AGAIN_MS, until, false), false); // no time left
  assert.equal(sendAgain(CLIENT.offline, 1, now, until, true), false); // the user went back
});

test("the words of the review list", () => {
  assert.equal(plural(1, "item", "items"), "1 item");
  assert.equal(foundTitle(3), "3 items found");
  assert.equal(postTitle(1), "Post 1 listing");
  assert.equal(postingText(3, 6), "Posting 3 of 6…");
  assert.equal(notPostedTitle(1), "1 item not posted");
  assert.equal(postedSummary(5, 1), "5 listings posted. 1 item is kept here; try again.");
  assert.equal(postedSummary(1, 2), "1 listing posted. 2 items are kept here; try again.");
});

test("itemPhotoPath: only a 32-hex photo ID", () => {
  assert.equal(itemPhotoPath(ID), "/api/analyze/photo/" + ID);
  assert.equal(itemPhotoPath("../etc"), "");
  assert.equal(itemPhotoPath(ID.toUpperCase()), "");
});

test("streamTarget: the camera asks for the upload size, shape kept", () => {
  assert.deepEqual(streamTarget(null), { width: { ideal: UPLOAD.maxEdge }, height: { ideal: Math.round(UPLOAD.maxEdge * 3 / 4) } });
  assert.deepEqual(streamTarget({ width: { max: 4000 }, height: { max: 3000 } }), { width: { ideal: 2376 }, height: { ideal: 1782 } });
  assert.deepEqual(streamTarget({ width: { max: 1280 }, height: { max: 720 } }), { width: { ideal: 1280 }, height: { ideal: 720 } });
});

test("sameTurn: both portrait or both landscape", () => {
  assert.equal(sameTurn(3000, 4000, 720, 1280), true);
  assert.equal(sameTurn(4000, 3000, 720, 1280), false);
  assert.equal(sameTurn(0, 10, 10, 10), false);
});

test("targetSize: long side at most maxEdge, never larger, 0 x 0 for a bad size", () => {
  assert.deepEqual(targetSize(4032, 3024), { width: 1600, height: 1200 });
  assert.deepEqual(targetSize(3000, 4000, 2376), { width: 1782, height: 2376 });
  assert.deepEqual(targetSize(640, 480), { width: 640, height: 480 });
  assert.deepEqual(targetSize(0, 480), { width: 0, height: 0 });
  assert.deepEqual(targetSize(Number.NaN, 600), { width: 0, height: 0 });
});

test("joinUrl: same origin on the web, absolute on a phone", () => {
  assert.equal(joinUrl("", "/api/listings/mine"), "/api/listings/mine");
  assert.equal(joinUrl("https://api.example.com/", "/api/listings"), "https://api.example.com/api/listings");
  assert.equal(joinUrl("https://api.example.com//", "api/listings"), "https://api.example.com/api/listings");
  assert.equal(joinUrl("https://a.example", "https://b.example/x"), "https://b.example/x");
});

test("errorMessage: the service's text first, then a plain line", () => {
  assert.equal(errorMessage(400, { error: "Title must be 1-80 characters" }), "Title must be 1-80 characters");
  assert.equal(errorMessage(401, undefined), "Please log in again");
  assert.equal(errorMessage(429, { code: "rate_limited" }), "Too many requests, wait a minute");
  assert.equal(errorMessage(503, null, "/api/analyze"), "The food scanner is busy, try again");
  assert.equal(errorMessage(502, null, "/api/listings"), "The service is busy, try again");
  assert.equal(errorMessage(418, "teapot"), "Request failed (418)");
});

test("scanStep (U18): wait while the scan job runs, then its answer or its error", () => {
  assert.deepEqual(scanStep({ status: "running", stage: "naming" }), { kind: "wait" });
  assert.deepEqual(scanStep(null), { kind: "wait" });
  const answer = { request_id: ID, items: [] };
  assert.deepEqual(scanStep({ status: "done", http_status: 200, answer }), { kind: "answer", body: answer });
  const no = { request_id: ID, error: "no food detected in the image", code: "not_food" };
  assert.deepEqual(scanStep({ status: "failed", http_status: 422, answer: no }), { kind: "error", status: 422, body: no });
  assert.deepEqual(scanStep({ status: "failed", http_status: 0 }), { kind: "error", status: 502, body: null });
});

test("scanPoll (X48c): a held poll at the version the app has; a second's wait with no version", () => {
  const p = "/api/analyze/scan/" + ID;
  assert.deepEqual(scanPoll(p, { status: "running", version: 3 }),
    { path: p + "?since=3&wait=" + SCAN_HOLD_MS, sleepMs: 0, timeoutMs: SCAN_HOLD_MS + 10_000 });
  assert.equal(scanPoll(p, { version: 0 }).path, p + "?since=0&wait=" + SCAN_HOLD_MS);
  for (const old of [{ scan_id: ID, status: "running" }, null, { version: "2" }, { version: -1 }, { version: 1.5 }]) {
    assert.deepEqual(scanPoll(p, old), { path: p, sleepMs: SCAN_POLL_MS, timeoutMs: 10_000 });
  }
  assert.deepEqual(scanPoll(p, null, true), { path: p, sleepMs: 0, timeoutMs: 10_000 }, "the first poll: at once");
  assert.ok(SCAN_HOLD_MS + 10_000 < 30_000, "under the 30 s limit of the public name");
});

test("analyzeKind (U18): the limit of one user for the day gives the daily-limit sheet", () => {
  assert.equal(analyzeKind({ code: "caller_daily_cap_reached" }), "daily_cap");
  assert.equal(analyzeKind({ code: "daily_cap_reached" }), "daily_cap");
});

// X48u: every change in order, how it was made; typing is one edit; the pickup text is never kept.
test("edits: diff, merge of typing, and the report", () => {
  const it = (item: number, title: string, servings: string) => ({ item, title, servings, photo_id: "", description: "", err: "" });
  const a = [it(1, "Bananas", "2"), it(2, "Pears", "1")];
  const b = [it(1, "Ripe bananas", "6")];
  assert.deepEqual(diffEdits(a, b, "Front", "Back door", "voice"), [
    { item: 1, field: "title", from: "Bananas", to: "Ripe bananas", how: "voice" },
    { item: 1, field: "servings", from: "2", to: "6", how: "voice" },
    { item: 2, field: "remove", from: "Pears", to: "", how: "voice" },
    { item: 0, field: "pickup", from: "", to: "", how: "voice" }]);
  assert.deepEqual(diffEdits(b, a, "", "", "undo").map((e) => e.field), ["title", "servings", "remove"]); // the removed item is back
  assert.deepEqual(diffEdits(b, [...b, it(4, "White rice", "2")], "", "", "text"), [{ item: 4, field: "found", from: "", to: "White rice", how: "text" }]); // I4: found from the chat
  let log = addEdits([], diffEdits(a, [it(1, "B", "2"), a[1]], "", "", "hand"));
  log = addEdits(log, diffEdits([it(1, "B", "2"), a[1]], [it(1, "Ba", "2"), a[1]], "", "", "hand"));
  assert.deepEqual(log, [{ item: 1, field: "title", from: "Bananas", to: "Ba", how: "hand" }]); // typing = one edit
  log = addEdits(log, [{ item: 1, field: "title", from: "Ba", to: "Bananas!", how: "text" }]);
  assert.equal(log.length, 2);
  assert.equal(addEdits([], Array.from({ length: 70 }, (_, k) => ({ item: 1, field: "servings" as const, from: "", to: String(k), how: "voice" as const }))).length, 64);
  const r = labelReport("r", a, b, "posted", "", { edits: log, chat: [{ how: "voice", text: "x".repeat(600), changes: 3 }],
    photos: { 1: { photo: "touched_up", touchup: "done" }, 2: { photo: "original", touchup: "" } } });
  assert.equal(r.items[0].photo, "touched_up");
  assert.equal(r.items[1].photo, ""); // not posted
  assert.equal(r.edits?.length, 2);
  assert.equal(r.chat?.[0].text.length, 500);
  assert.equal("edits" in labelReport("r", a, b, "posted"), false); // nothing new: the X32g report as before
});

test("edits review fixes: typed back is no edit; bytes; pickup lines; the size limit", () => {
  const e = (to: string) => ({ item: 1, field: "title" as const, from: "Soup", to, how: "hand" as const });
  assert.deepEqual(addEdits(addEdits([], [e("Sou")]), [e("Soup")]), []); // typed back to the model's value
  assert.equal(clipBytes("\u65e5\u672c\u8a9e", 7), "\u65e5\u672c"); // 3 bytes each: two fit in 7
  assert.equal(clipBytes("abc", 10), "abc");
  const it = (item: number) => ({ item, title: "T" + item, servings: "1", photo_id: "", description: "", err: "" });
  const r = labelReport("r", [it(1)], [it(1)], "posted", "", { chat: [{ how: "text", text: "12 Elm Rd back door", changes: 1, pickup: true }] });
  assert.equal(r.chat?.[0].text, "");
  const big = labelReport("r", [it(1)], [it(1)], "posted", "", {
    chat: Array.from({ length: 16 }, () => ({ how: "voice" as const, text: "\u65e5".repeat(500), changes: 1 })),
    edits: Array.from({ length: 64 }, (_, k) => ({ item: 1, field: "servings" as const, from: "\u65e5".repeat(80), to: String(k), how: "voice" as const })) });
  const fit = fitReport(big, 8 * 1024);
  assert.ok(new TextEncoder().encode(JSON.stringify(fit)).length <= 8 * 1024);
  assert.equal(fit.items.length, 1); // the item labels always stay
  assert.ok((fit.chat?.length ?? 0) < 16);
});

test("edits (X49): a change of the details gives condition, diet, portions and weight edits; the report sends them", () => {
  const it = (details?: object) => ({ item: 1, title: "Bananas", servings: "2", photo_id: "", description: "", err: "", details });
  const a = [it({ meal_condition: "ready_to_eat", is_non_veg: 0, weight: 1.2, meal_cost: 4, description: "Ripe" })];
  const b = [it({ meal_condition: "cooking_required", is_non_veg: 1, serving_type: "bulk", meal_cost: 4, description: "Ripe" })];
  const got = diffEdits(a, b, "", "", "hand");
  assert.deepEqual(got, [
    { item: 1, field: "condition", from: "ready_to_eat", to: "cooking_required", how: "hand" },
    { item: 1, field: "diet", from: "0", to: "1", how: "hand" },
    { item: 1, field: "portions", from: "", to: "bulk", how: "hand" },
    { item: 1, field: "weight", from: "1.2", to: "", how: "hand" }]);
  assert.deepEqual(diffEdits([it()], [it({ weight: 3 })], "", "", "hand"), [{ item: 1, field: "weight", from: "", to: "3", how: "hand" }]);
  assert.deepEqual(diffEdits(a, a, "", "", "hand"), []); // the same details: no edit (the cost and the description are not edits)
  const r = labelReport("r", a, b, "posted", "", { edits: got });
  assert.deepEqual(r.edits?.map((e) => e.field), ["condition", "diet", "portions", "weight"]);
});

test("X49 review: the details close when their item is gone; a failed post keeps the edits made while it ran", () => {
  const it = (item: number, title: string, details?: object) => ({ item, title, servings: "1", photo_id: "", description: "", err: "", details });
  const list = [it(1, "A"), it(2, "B")];
  assert.equal(stillOpen(2, list), 2);
  assert.equal(stillOpen(3, list), null);
  assert.equal(stillOpen(null, list), null);
  const failed = [it(2, "B", { weight: 1 })];
  const now = [it(1, "A"), it(2, "B", { weight: 5 })]; // changed while the post ran
  assert.deepEqual(notPostedItems(failed, now, "E"), [{ ...now[1], err: "E" }]);
  assert.deepEqual(notPostedItems(failed, [it(1, "A")], "E"), []); // removed by the user during the post: it stays removed
});

test("an item removed during the post does not come back when its post fails", () => {
  const it = (item: number) => ({ item, title: "T" + item, servings: "1", photo_id: "", description: "", err: "" });
  assert.deepEqual(notPostedItems([it(1), it(2)], [it(1)], "not posted").map((x) => x.item), [1]);
});
