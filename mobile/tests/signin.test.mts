// Tests of the sign-in words and rules (src/signin.ts), written new for V3RDE (NEW_WORK_UI.md, UI 8).
import test from "node:test";
import { strict as assert } from "node:assert";
import { EMAIL_FORM, EMAIL_LOOSE, MIN_PASSWORD, SIGN_IN, cleanEmail, failure, formProblem } from "../src/signin.ts";

test("the words name the same password length as the rule", () => {
  assert.equal(MIN_PASSWORD, 8);
  assert.equal(SIGN_IN.rule, "8 characters or more");
  assert.equal(SIGN_IN.incomplete, "Enter your email and a password of 8 characters or more");
});

test("the form is incomplete without an email or with a short password", () => {
  assert.equal(formProblem("", "abcdefgh"), SIGN_IN.incomplete);
  assert.equal(formProblem("   ", "abcdefgh"), SIGN_IN.incomplete);
  assert.equal(formProblem("sam@example.com", "abcdefg"), SIGN_IN.incomplete);
  assert.equal(formProblem("sam@example.com", "abcdefgh"), "");
  assert.equal(formProblem("  sam@example.com ", "abcdefgh"), "");
  assert.equal(cleanEmail("  sam@example.com "), "sam@example.com");
});

test("the email must have the form name@domain.end (the same check as the auth service)", () => {
  for (const ok of ["sam@example.com", "a@b.co", "donor@example.org", "x.y+z@mail.example.co.uk", "ade@company.ng", "priya@school.in",
    "korede@thehungreeapp.com", "o'brien@example.ie"]) assert.ok(EMAIL_FORM.test(ok), ok);
  // X49w: stricter (T9): a real ending of 2+ letters, no empty or bad domain parts, no odd characters
  for (const bad of ["sam", "sam@", "sam@example", "@example.com", "sam@@example.com", "sam @example.com", "sam@example.",
    "sam@example.c", "sam@example.c0m", "sam@.example.com", "sam@example..com", "sam@-example.com", "sam@example-.com",
    "sam(at)example.com", "sam@exa mple.com", "sam@example.com.", "sam,x@example.com"]) {
    assert.ok(!EMAIL_FORM.test(bad), bad);
  }
  assert.equal(formProblem("sam@example", "abcdefgh"), SIGN_IN.badEmail);
  // X49w review: the strict form is for sign-up; sign-in and the reset keep the loose form (older accounts)
  assert.equal(formProblem("sam@example.c", "abcdefgh", "signup"), SIGN_IN.badEmail);
  assert.equal(formProblem("sam@example.c", "abcdefgh", "login"), "");
  assert.equal(formProblem("josé@example.com", "abcdefgh", "login"), "");
  assert.ok(EMAIL_LOOSE.test("user@mail.xn--p1ai") && !EMAIL_LOOSE.test("sam @example.com"));
});

test("X49w: an unknown email offers Create an account; a wrong password offers Forgot password? (T9)", () => {
  assert.deepEqual(failure("login", 401, "x", "no_account"), { offer: "create", text: "No account uses this email." });
  assert.deepEqual(failure("login", 401, "x", "wrong_password"), { offer: "forgot", text: "Wrong password." });
  assert.deepEqual(failure("login", 401, "x"), { offer: "forgot", text: "Wrong email or password." }); // an older service
  assert.deepEqual(failure("signup", 401, "Unauthorized"), { offer: "", text: "Unauthorized" });
  assert.deepEqual(failure("login", 0, "Network error, check the connection"), { offer: "", text: "Network error, check the connection" });
  assert.deepEqual(failure("login", undefined, "boom"), { offer: "", text: "boom" });
  assert.equal(SIGN_IN.create, "Create an account");
  assert.ok(!("newHere" in SIGN_IN));
});

test("X49w: before a try only About shows; the one link and About share one row", async () => {
  const fs = await import("node:fs");
  const login = fs.readFileSync(new URL("../src/screens/Login.tsx", import.meta.url), "utf8");
  assert.match(login, /offer === "create" \? <TextLink testID="create"[^\n]*\n\s*: offer === "forgot" \? <TextLink testID="forgot"[^\n]*\n\s*: <View \/>\}/);
  assert.equal((login.match(/testID="forgot"/g) ?? []).length, 1);
  assert.equal((login.match(/testID="create"/g) ?? []).length, 1);
});
