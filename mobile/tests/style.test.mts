// The style gate of V3RDE (mobile/docs/style_guide.md), written new for V3RDE (NEW_WORK_UI.md, UI 5).
// Every colour, font, text size, corner radius and fractional value lives in src/theme.ts; the parts and screens
// take them from there. The guide lists every token of theme.ts with its value, and no token that is not there.
import test from "node:test";
import { strict as assert } from "node:assert";
import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";

const ROOT = new URL("..", import.meta.url).pathname;
const THEME = readFileSync(join(ROOT, "src/theme.ts"), "utf8");
const GUIDE = readFileSync(join(ROOT, "docs/style_guide.md"), "utf8");

function sources(dir: string): string[] {
  const out: string[] = [];
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) out.push(...sources(p));
    else if (/\.(tsx?|mts)$/.test(name)) out.push(p);
  }
  return out;
}
const UI_FILES = [...sources(join(ROOT, "src")), join(ROOT, "App.tsx")]
  .filter((p) => existsSync(p) && !p.endsWith("src/theme.ts"));

/** The source without comments, so a comment may name a colour or a size. */
function code(p: string): string {
  return readFileSync(p, "utf8").replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|[^:])\/\/.*$/gm, "$1");
}
/** The source without comments and without the text in quotes (words on the screen may hold numbers). */
function bare(p: string): string {
  return code(p).replace(/"(?:[^"\\\n]|\\.)*"|'(?:[^'\\\n]|\\.)*'|`(?:[^`\\]|\\.)*`/g, '""');
}

// CSS named colours that a style could use by mistake (CSS Color 4: https://www.w3.org/TR/css-color-4/#named-colors).
const NAMED = /["'](black|white|red|green|blue|gray|grey|silver|maroon|navy|purple|teal|olive|lime|aqua|fuchsia|orange|yellow|pink|brown|transparent)["']/gi;

test("no colour outside theme.ts", () => {
  for (const p of UI_FILES) {
    const hits = [...(code(p).match(/#[0-9a-fA-F]{3,8}\b|\b(rgba?|hsla?)\(/g) ?? []), ...(code(p).match(NAMED) ?? [])];
    assert.deepEqual(hits, [], `${p}: ${hits}`);
  }
});

test("no font, text size or line height outside theme.ts", () => {
  for (const p of UI_FILES) {
    const hits = bare(p).match(/\b(fontFamily|fontSize|lineHeight|fontWeight)\s*:/g);
    assert.equal(hits, null, `${p}: ${hits}`);
  }
});

test("every corner radius comes from the radius tokens", () => {
  for (const p of UI_FILES) {
    const hits = bare(p).match(/\bborder\w*Radius\s*:(?!\s*radius\b)[^,}\n]*/g);
    assert.equal(hits, null, `${p}: ${hits}`);
  }
});

test("no fractional number in a part or a screen (opacity, motion, ratios are tokens)", () => {
  for (const p of UI_FILES.filter((f) => f.endsWith(".tsx"))) {
    const hits = bare(p).match(/(?<![\w.])\d*\.\d+\b/g);
    assert.equal(hits, null, `${p}: ${hits}`);
  }
});

/** The colours of one mode from theme.ts: { key: "#RRGGBB" }. */
function mode(name: "light" | "dark"): Record<string, string> {
  const block = THEME.match(new RegExp(name + ": \\{([\\s\\S]*?)\\n  \\}"));
  assert.ok(block, name);
  const out: Record<string, string> = {};
  for (const m of block[1].matchAll(/(\w+): "([^"]+)"/g)) out[m[1]] = m[2];
  return out;
}

test("light and dark mode have the same colour names", () => {
  assert.deepEqual(Object.keys(mode("dark")).sort(), Object.keys(mode("light")).sort());
});

// WCAG 2.1 contrast ratio: https://www.w3.org/TR/WCAG21/#dfn-contrast-ratio
function luminance(hex: string): number {
  const ch = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255)
    .map((v) => (v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4));
  return 0.2126 * ch[0] + 0.7152 * ch[1] + 0.0722 * ch[2];
}
function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

test("text colours reach 4.5:1 on their backgrounds in both modes", () => {
  const pairs: Array<[string, string]> = [["fg", "card"], ["fg", "bg"], ["muted", "card"], ["muted", "bg"],
    ["danger", "card"], ["danger", "bg"], ["onAccent", "accent"], ["fg", "iconFill"],
    ["link", "card"], ["link", "bg"],
    ["link", "accentSoft"], ["bg", "fg"], ["muted", "iconFill"], // the chosen top-bar tab; a toast; a tab under the mouse
    ["link", "iconFill"]]; // X49: a ChipBtn that is on
  for (const m of ["light", "dark"] as const) {
    const c = mode(m);
    for (const [fg, bg] of pairs) {
      const r = contrast(c[fg], c[bg]);
      assert.ok(r >= 4.5, `${m}: ${fg} ${c[fg]} on ${bg} ${c[bg]} is ${r.toFixed(2)}:1`);
    }
  }
});

/** The flat token groups of theme.ts: { "space.hair": "4", "glyph.close": "M0.835 …", … }. */
function tokens(): Record<string, string> {
  const out: Record<string, string> = {};
  for (const g of THEME.matchAll(/export const (\w+) = \{([\s\S]*?)\} as const;/g)) {
    if (g[1] === "palette" || g[1] === "type") continue;
    for (const m of g[2].matchAll(/^\s*(\w+):\s*("([^"]*)"|[^,\n]+?),?\s*(\/\/.*)?$/gm)) {
      out[g[1] + "." + m[1]] = m[3] ?? m[2].trim();
    }
  }
  for (const m of THEME.matchAll(/^\s*(\w+): \{ fontFamily: font\.(\w+), fontSize: (\d+), lineHeight: (\d+) \}/gm)) {
    out["type." + m[1]] = m[3] + " / " + m[4];
  }
  return out;
}
const rows = GUIDE.split("\n").filter((l) => l.startsWith("|"));
const cells = (l: string) => l.split("|").map((x) => x.trim().replace(/^`|`$/g, ""));

test("the style guide lists every token with its value", () => {
  const t = tokens();
  assert.ok(Object.keys(t).length > 60, "tokens parsed: " + Object.keys(t).length);
  for (const [k, v] of Object.entries(t)) {
    const row = rows.find((l) => cells(l)[1] === k);
    assert.ok(row, `style_guide.md has no row for ${k}`);
    assert.ok(cells(row).includes(v), `style_guide.md: ${k} must show ${v}: ${row}`);
  }
  const light = mode("light"), dark = mode("dark");
  for (const k of Object.keys(light)) {
    const row = rows.find((l) => cells(l)[1] === k);
    assert.ok(row, `style_guide.md has no colour row for ${k}`);
    assert.ok(cells(row).includes(light[k]) && cells(row).includes(dark[k]), `style_guide.md: ${k} must show ${light[k]} and ${dark[k]}: ${row}`);
  }
  const bounds = THEME.match(/function layoutFor[\s\S]*?\n\}/)?.[0].match(/\b\d+\b/g) ?? [];
  const layout = rows.filter((l) => l.includes("layoutFor")).join(" ");
  for (const b of bounds) assert.ok(layout.includes(b), `style_guide.md: the layoutFor rows must show ${b}`);
});

test("the style guide lists no token that theme.ts does not have", () => {
  const t = tokens(), colours = mode("light");
  for (const l of rows) {
    const k = cells(l)[1];
    if (/^[a-z]+\.[A-Za-z]+$/.test(k) && /^(space|radius|size|glyph|shade|camera|opacity|motion|font|type|doodle)\./.test(k)) {
      assert.ok(k in t, `style_guide.md lists ${k}, which theme.ts does not have`);
    }
    if (/^[a-z][A-Za-z]+$/.test(k) && l.includes("#") ) assert.ok(k in colours, `style_guide.md lists colour ${k}, which theme.ts does not have`);
  }
});

test("U19: the frame is bounded and both role screens use the one title row", () => {
  const frame = code(join(ROOT, "src/screens/Frame.tsx"));
  assert.match(frame, /<View style=\{\{ flex: 1, minHeight: 0 \}\}>\s*\{bar\}/, "the frame: flex 1 + minHeight 0 (else the list cannot scroll)");
  assert.match(frame, /nativeID="role-panel"[^>]*style=\{\{ flex: 1, minHeight: 0 \}\}/, "the role panel: flex 1 + minHeight 0");
  for (const name of ["Give", "Receive"]) {
    const src = code(join(ROOT, `src/screens/${name}.tsx`));
    assert.match(src, /<ScreenHead title=/, `${name} uses ScreenHead`);
    assert.match(src, /<ScrollView/, `${name} scrolls`);
  }
  assert.doesNotMatch(code(join(ROOT, "src/screens/Receive.tsx")), /owner_id === who/, "Receive filters with othersOnly");
});
