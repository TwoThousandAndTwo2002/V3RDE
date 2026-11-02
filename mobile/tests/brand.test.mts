// LG1 tests: the V3RDE logo (mobile/src/brand.tsx, the logo tokens of src/theme.ts, the About and Learn pages), written
// new for V3RDE in the Hackathon (T9 Nov 1: the mark T1 "V³" and the wordmark G; the approved "V3RDE Logo Mockup").
// The icon files are checked by the Go tool's tests (mobile/tools/brand). Each test fails when its rule breaks.
import test from "node:test";
import { strict as assert } from "node:assert";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const ROOT = new URL("..", import.meta.url).pathname;
const read = (p: string) => readFileSync(join(ROOT, p), "utf8");
const THEME = read("src/theme.ts"); // read as text: theme.ts imports react-native, which node cannot load

/** The logo tokens of theme.ts: numbers (and "a / b") or strings. */
const logo = (() => {
  const block = THEME.match(/export const logo = \{([\s\S]*?)\n\} as const;/);
  assert.ok(block, "theme.ts has the logo group");
  const out: Record<string, any> = {};
  for (const m of block[1].matchAll(/^\s*(\w+):\s*(?:"([^"]*)"|([^,\n]+?)),?\s*(?:\/\/.*)?$/gm)) {
    out[m[1]] = m[2] !== undefined ? m[2] : m[3].includes("/") ? Number(m[3].split("/")[0]) / Number(m[3].split("/")[1]) : Number(m[3]);
  }
  return out;
})();
const colour = (mode: string, key: string) => THEME.match(new RegExp(`\\n  ${mode}: \\{[\\s\\S]*?\\b${key}: "(#[0-9A-F]{6})"`))?.[1];
/** The source without comments. */
const code = (p: string) => read(p).replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|[^:])\/\/.*$/gm, "$1");
const BRAND = code("src/brand.tsx");

test("the approved geometry is in the logo tokens (T1 and G)", () => {
  assert.equal(logo.v, "10,10 38,10 60,62 82,10 110,10 74,90 46,90");
  assert.deepEqual([logo.viewW, logo.viewH, logo.depth1, logo.depth2], [130, 118, 8, 16]);
  assert.equal(logo.leaf, "M2 22 C2 10 10 2 22 2 C22 14 14 22 2 22 Z");
  assert.deepEqual([logo.leafBox, logo.leafEm, logo.leafGapEm, logo.iconShare], [24, 0.36, 0.04, 0.46]);
  // the three V's stay inside the box: the deepest V's lowest point is inside viewH, its right point inside viewW
  const pts = logo.v.split(" ").map((p) => p.split(",").map(Number));
  assert.ok(Math.max(...pts.map((p) => p[0])) + logo.depth2 <= logo.viewW);
  assert.ok(Math.max(...pts.map((p) => p[1])) + logo.depth2 <= logo.viewH);
});

test("the mark draws the token V three times, back to front: accentSoft, accent, fg", () => {
  assert.doesNotMatch(BRAND, /\d+,\d+ \d+,\d+/, "no polygon written in brand.tsx: it comes from logo.v");
  const layers = [...BRAND.matchAll(/<Polygon points=\{logo\.v\} fill=\{col\.(\w+)\} \/>/g)].map((m) => m[1]);
  assert.deepEqual(layers, ["accentSoft", "accent", "fg"]);
  assert.match(BRAND, /translate\(\$\{logo\.depth2\}, \$\{logo\.depth2\}\)[\s\S]*translate\(\$\{logo\.depth1\}, \$\{logo\.depth1\}\)/);
  assert.match(BRAND, /viewBox=\{`0 0 \$\{logo\.viewW\} \$\{logo\.viewH\}`\}/);
  assert.match(BRAND, /<Path d=\{logo\.leaf\} fill=\{col\.accent\} \/>/);
  assert.match(BRAND, /marginBottom: t\.lineHeight \/ 2 - logo\.baselineEm \* t\.fontSize/, "the leaf sits on the baseline");
});

test("the sizes and names of the approved places (b top bar, c phone header, b sign-in)", () => {
  assert.match(BRAND, /export function TopBrand\(\) \{\s*return <Row t=\{type\.title\}/);
  assert.match(BRAND, /export function PhoneBrand\(\)[\s\S]*?accessibilityRole="image" aria-label=\{FRAME\.brand\}><Mark h=\{type\.title\.lineHeight\} \/>/);
  assert.match(BRAND, /export function SignInBrand\(\)[\s\S]*?marginBottom: space\.pad[\s\S]*?<Row t=\{type\.display\}/);
  assert.match(BRAND, /accessibilityRole="header" aria-label=\{FRAME\.brand\}[\s\S]*?gap: space\.gap[\s\S]*?<Mark h=\{t\.lineHeight\} \/>\s*<Word t=\{t\} \/>/);
  assert.match(BRAND, /<Text style=\{\[t, \{ color: col\.fg \}\]\}>\{FRAME\.brand\}<\/Text>/, "the word in fg");
});

test("the frame, sign-in and the three reset screens show the logo, not the word", () => {
  const frame = code("src/screens/Frame.tsx");
  assert.match(frame, /<TopBrand \/>/);
  assert.match(frame, /<PhoneBrand \/>/);
  assert.doesNotMatch(frame, /\{FRAME\.brand\}/, "no plain-text brand in the frame");
  assert.match(code("src/screens/Login.tsx"), /<SignInBrand \/>\s*<Title>\{SIGN_IN\.title\}<\/Title>/);
  const reset = code("src/screens/Reset.tsx");
  for (const t of ["sentTitle", "askTitle", "newTitle"]) assert.match(reset, new RegExp(`<SignInBrand />\\s*<Title>\\{RESET\\.${t}\\}</Title>`), t);
});

for (const page of ["public/about/index.html", "public/learn/index.html"]) {
  test(`${page}: the same logo, from the same tokens`, () => {
    const html = read(page);
    const body = html.slice(html.indexOf("<body>"));
    const a = body.match(/<a class="[^"]*\bbrand\b[^"]*" href="\/" aria-label="V3RDE">([\s\S]*?)<\/a>/);
    assert.ok(a, "the brand link to the app, named V3RDE");
    assert.doesNotMatch(body, /class="[^"]*brand[^"]*"[^>]*>V3RDE<\/a>/, "no bare-text brand link");
    const polys = [...a[1].matchAll(/<polygon class="(m\d)"(?: transform="translate\((\d+) (\d+)\)")? points="([^"]+)"\/>/g)];
    assert.deepEqual(polys.map((m) => [m[1], m[2] ?? "0", m[4]]),
      [["m2", String(logo.depth2), logo.v], ["m1", String(logo.depth1), logo.v], ["m0", "0", logo.v]]);
    assert.match(a[1], new RegExp(`viewBox="0 0 ${logo.viewW} ${logo.viewH}" aria-hidden="true"`));
    assert.ok(a[1].includes(`<path d="${logo.leaf}"/>`), "the leaf");
    assert.match(html, /\.mark \.m0 \{ fill: var\(--fg\); \} \.mark \.m1 \{ fill: var\(--accent\); \} \.mark \.m2 \{ fill: var\(--accentSoft\); \}/);
    assert.ok(html.includes(`.brand .leaf { width: ${logo.leafEm}em; height: ${logo.leafEm}em; margin-left: ${logo.leafGapEm}em; fill: var(--accent); }`));
    assert.match(html, /\.brand \.word \{ display: none;[\s\S]*@media \(min-width: 840px\) \{[\s\S]*?\.brand \.word \{ display: inline-flex; \}/,
      "phone: the mark only; wide: the mark and the word");
  });
}

test("the icon colours are palette tokens (no new colour)", () => {
  assert.equal(colour("light", "accent"), "#BC202C");
  assert.equal(colour("dark", "accentSoft"), "#4A1D22");
  const tool = readFileSync(join(ROOT, "tools/brand/brand.go"), "utf8");
  assert.match(tool, /"light", "accent"\}, \{&l\.Face, "light", "onAccent"\}, \{&l\.Deep1, "light", "fg"\}, \{&l\.Deep2, "dark", "accentSoft"\}/);
  assert.doesNotMatch(tool.replace(/\/\/.*$/gm, ""), /0x[0-9A-Fa-f]{2}, 0x[0-9A-Fa-f]{2}/, "the tool takes no colour of its own");
});
