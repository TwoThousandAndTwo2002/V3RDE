// Tests of the reset words and the link parser (src/signin.ts), written new for V3RDE (NEW_WORK_UI.md, UI 9).
import test from "node:test";
import { strict as assert } from "node:assert";
import { RESET, RESET_MINUTES, resetToken } from "../src/signin.ts";

test("the token comes from the web link and from the app link", () => {
  const t43 = "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789-_AbCd"; // 32 bytes, unpadded base64url: 43 characters
  assert.equal(resetToken("https://v3rde.example/reset#token=" + t43 + "x"), t43 + "x");
  assert.equal(resetToken("https://v3rde.example/reset#token=abc_DEF-123"), "abc_DEF-123");
  assert.equal(resetToken("http://127.0.0.1:8779/reset#token=xyz"), "xyz");
  assert.equal(resetToken("/reset#token=xyz"), "xyz");
  assert.equal(resetToken("/reset/#x=1&token=xyz"), "xyz");
  assert.equal(resetToken("v3rde://reset#token=abc"), "abc");
});

test("other links give no token", () => {
  for (const u of [null, undefined, "", "https://v3rde.example/", "https://v3rde.example/reset", "https://v3rde.example/reset#token=",
    "https://v3rde.example/reset?token=abc", "https://v3rde.example/reset#token=abc=", "https://v3rde.example/reset#token=a+b",
    "https://v3rde.example/other#token=abc", "https://evil.example/x/reset#token=abc", "v3rde://login"]) {
    assert.equal(resetToken(u), "", String(u));
  }
});

test("the words name the link time and the password rule", () => {
  assert.equal(RESET_MINUTES, 30);
  assert.ok(RESET.sent("sam@example.com").includes("sam@example.com") && RESET.sent("a@b.co").includes("30 minutes"));
  assert.equal(RESET.newLine, "Choose a password of 8 characters or more");
});
