// Tests of the app words (src/words.ts), written new for V3RDE (NEW_WORK_UI.md, UI 10).
import test from "node:test";
import { strict as assert } from "node:assert";
import { GIVE, RECEIVE, ROLE, postedText, roleCard, servings } from "../src/words.ts";

test("the role sheet uses the words T9 agreed", () => {
  assert.equal(ROLE.title, "What do you want to do?");
  assert.deepEqual(roleCard("give"), { title: "Give", line: "Share your food", spoken: "Give. Share your food" });
  assert.deepEqual(roleCard("receive"), { title: "Receive", line: "Find food near you", spoken: "Receive. Find food near you" });
  assert.equal(ROLE.logout, "Log out");
  assert.equal(roleCard("give", true).spoken, "Give. Share your food. Selected");
});

test("every word line is short (ASD-STE100: 20 words or fewer in a sentence)", () => {
  for (const v of Object.values(ROLE)) assert.ok(v.split(/\s+/).length <= 20, v);
});

test("the Receive words", () => {
  assert.equal(servings(1), "1 serving");
  assert.equal(servings(6), "6 servings");
  for (const v of Object.values(RECEIVE)) assert.ok(v.split(/\s+/).length <= 20, v);
});

test("the Give words", () => {
  assert.equal(GIVE.title, "Your listings");
  assert.equal(postedText(1), "Listed. It stays up for 30 minutes.");
  assert.equal(postedText(3), "3 listings are posted. They stay up for 30 minutes.");
});
