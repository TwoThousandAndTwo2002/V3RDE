// Tests of the sign-in words and rules (src/signin.ts), written new for V3RDE (NEW_WORK_UI.md, UI 8).
import test from "node:test";
import { strict as assert } from "node:assert";
import { EMAIL_FORM, MIN_PASSWORD, SIGN_IN, cleanEmail, failure, formProblem } from "../src/signin.ts";

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
  for (const ok of ["sam@example.com", "a@b.co", "donor@example.org", "x.y+z@mail.example.co.uk"]) assert.ok(EMAIL_FORM.test(ok), ok);
  for (const bad of ["sam", "sam@", "sam@example", "@example.com", "sam@@example.com", "sam @example.com", "sam@example."]) {
    assert.ok(!EMAIL_FORM.test(bad), bad);
  }
  assert.equal(formProblem("sam@example", "abcdefgh"), SIGN_IN.badEmail);
});

test("a 401 on sign-in offers to create an account; other failures show their text", () => {
  assert.deepEqual(failure("login", 401, "x"), { offer: true, text: "" });
  assert.deepEqual(failure("signup", 401, "Unauthorized"), { offer: false, text: "Unauthorized" });
  assert.deepEqual(failure("login", 0, "Network error, check the connection"), { offer: false, text: "Network error, check the connection" });
  assert.deepEqual(failure("login", undefined, "boom"), { offer: false, text: "boom" });
});
