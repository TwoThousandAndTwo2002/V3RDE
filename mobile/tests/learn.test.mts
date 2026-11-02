// I3 tests: the admin learn report page (mobile/public/learn/index.html, NEW_WORK.md #98). The page is static HTML
// next to the About page, so it is held to the same rule: its token block is the About page's, byte for byte (the
// About tests check that block against src/theme.ts), and nothing outside that block adds a colour, a length or a
// fraction. The report has the donors' words, so the script writes text only through textContent.
import test from "node:test";
import { strict as assert } from "node:assert";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const ROOT = new URL("..", import.meta.url).pathname;
const PAGE = readFileSync(join(ROOT, "public/learn/index.html"), "utf8");
const ABOUT = readFileSync(join(ROOT, "public/about/index.html"), "utf8");
const THEME = readFileSync(join(ROOT, "src/theme.ts"), "utf8");
const STYLE = PAGE.slice(PAGE.indexOf("<style>"), PAGE.indexOf("</style>")).replace(/\/\*[\s\S]*?\*\//g, "");
const SCRIPT = PAGE.slice(PAGE.indexOf("<script>"), PAGE.indexOf("</script>"));
const BODY = PAGE.slice(PAGE.indexOf("<body>"), PAGE.indexOf("<script>"));
const tokens = (s: string) => s.slice(s.indexOf("@font-face"), s.indexOf("* { box-sizing"));

test("the token block (fonts, palette in both modes, space, radius, size, widths) is the About page's", () => {
  assert.ok(tokens(PAGE).length > 1000);
  assert.equal(tokens(PAGE), tokens(ABOUT));
});

test("no colour, length or fraction outside the token block", () => {
  const outside = STYLE.replace(/:root \{[\s\S]*?\n\}/, "").replace(/@media \(prefers-color-scheme: dark\) \{[\s\S]*?\n\}/, "")
    .replace(/:root\[data-theme="dark"\] \{[\s\S]*?\n\}/, "").replace(/@font-face[^\n]*/g, "").replace(/--maxWidth: \d+px/g, "");
  assert.equal((outside + BODY + SCRIPT).match(/#[0-9a-fA-F]{3,8}\b|\b(rgba?|hsla?)\(/g), null);
  const typeRows = [...THEME.matchAll(/^\s*(\w+): \{ fontFamily: font\.(\w+), fontSize: (\d+), lineHeight: (\d+) \}/gm)];
  const allowed = new Set(["600", "840", ...typeRows.flatMap((m) => [m[3], m[4]])]);
  for (const m of (outside + BODY + SCRIPT).matchAll(/(\d+(?:\.\d+)?)px/g)) assert.ok(allowed.has(m[1]), "a length that is not a token: " + m[0]);
  const fractions = outside.replace(/opacity: (0\.92|0\.85);/g, "").match(/(?<![\w.#-])\d*\.\d+(?!\w)/g);
  assert.equal(fractions, null, "a fraction that is not a token: " + fractions);
  for (const m of STYLE.matchAll(/var\((--[\w-]+)\)/g)) assert.match(STYLE, new RegExp(m[1] + ":"), m[1] + " is not defined");
});

test("the script reads only the admin route, with the session cookie, and writes text only as text", () => {
  const urls = [...SCRIPT.matchAll(/["'](\/api\/[^"']*)["']/g)].map((m) => m[1]);
  assert.deepEqual([...new Set(urls)].sort(), ["/api/analyze/learn", "/api/analyze/learn/"]);
  assert.equal((SCRIPT.match(/credentials: "same-origin"/g) || []).length, 2);
  assert.doesNotMatch(SCRIPT, /innerHTML|outerHTML|insertAdjacentHTML|document\.write|eval\(|new Function/);
  assert.match(SCRIPT, /\.filter\(\(d\) => \/\^\[0-9\]\{8\}\$\/\.test\(d\)\)/, "the days are checked before they go into a URL");
  assert.match(SCRIPT, /if \(d !== current\) return;/, "an older answer that comes later is dropped");
  assert.match(PAGE, /<meta name="robots" content="noindex">/);
  assert.match(BODY, /<h1 class="display">Learn report<\/h1>/);
  for (const words of ["Not found.", "No answer. Try again.", "No report yet. The first one is made after the first whole day.", "This report cannot be shown."]) {
    assert.ok(SCRIPT.includes(words), words);
  }
});
