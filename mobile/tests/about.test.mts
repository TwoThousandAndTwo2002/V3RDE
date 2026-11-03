// Tests of the About page (mobile/public/about/index.html), written new for V3RDE (NEW_WORK.md #78, branch about).
// The page is static HTML, so the S3 style test (src/ and App.tsx only) does not read it. These tests hold it to the
// same rule (mobile/docs/style_guide.md "About page"): every colour, text style, length, opacity and doodle value is
// a token of src/theme.ts; the backdrop is the app's doodle wallpaper; the sign-in screen links to it.
import test from "node:test";
import { strict as assert } from "node:assert";
import { existsSync, readFileSync } from "node:fs";
import { createHash } from "node:crypto";
import { join } from "node:path";
import vm from "node:vm";
import { ABOUT_PATH, ABOUT_URL, SIGN_IN } from "../src/signin.ts";
import { startGrid, stepUp, wallpaper } from "../src/automata.ts";

const ROOT = new URL("..", import.meta.url).pathname;
const PAGE = readFileSync(join(ROOT, "public/about/index.html"), "utf8");
const THEME = readFileSync(join(ROOT, "src/theme.ts"), "utf8");
const GUIDE = readFileSync(join(ROOT, "docs/style_guide.md"), "utf8");
const STYLE = PAGE.slice(PAGE.indexOf("<style>"), PAGE.indexOf("</style>")).replace(/\/\*[\s\S]*?\*\//g, "");
const SCRIPT = PAGE.slice(PAGE.indexOf("<script>"), PAGE.indexOf("</script>"));
const BODY = PAGE.slice(PAGE.indexOf("<body>"), PAGE.indexOf("<script>"));

/** One token group of theme.ts as { key: "value" } (the text of the value, as the style test reads it). */
function group(name: string): Record<string, string> {
  const g = THEME.match(new RegExp("export const " + name + " = \\{([\\s\\S]*?)\\} as const;"));
  assert.ok(g, name);
  const out: Record<string, string> = {};
  for (const m of g[1].matchAll(/^\s*(\w+):\s*("([^"]*)"|[^,\n]+?),?\s*(\/\/.*)?$/gm)) out[m[1]] = m[3] ?? m[2].trim();
  return out;
}
function mode(name: "light" | "dark"): Record<string, string> {
  const block = THEME.match(new RegExp(name + ": \\{([\\s\\S]*?)\\n  \\}"));
  assert.ok(block, name);
  const out: Record<string, string> = {};
  for (const m of block[1].matchAll(/(\w+): "([^"]+)"/g)) out[m[1]] = m[2];
  return out;
}
/** The custom properties of one CSS block of the page: { "--bg": "#F6F5F5", … }. */
function vars(block: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const m of block.matchAll(/(--[\w-]+):\s*([^;]+);/g)) out[m[1]] = m[2].trim();
  return out;
}
const LIGHT = vars(STYLE.match(/:root \{([\s\S]*?)\n\}/)![1]);
const DARK_MEDIA = vars(STYLE.match(/:root:not\(\[data-theme="light"\]\) \{([\s\S]*?)\n  \}/)![1]);
const DARK_SET = vars(STYLE.match(/:root\[data-theme="dark"\] \{([\s\S]*?)\n\}/)![1]);
const px = (v: string) => v.replace(/px$/, "");

test("every colour of the page is a palette token, in light and in dark mode", () => {
  const light = mode("light"), dark = mode("dark");
  const colours = Object.entries(LIGHT).filter(([, v]) => v.startsWith("#"));
  assert.ok(colours.length >= 10, "colours: " + colours.length);
  for (const [k, v] of colours) assert.equal(v, light[k.slice(2)], `light ${k}`);
  for (const d of [DARK_MEDIA, DARK_SET]) {
    for (const [k, v] of Object.entries(d).filter(([, x]) => x.startsWith("#"))) assert.equal(v, dark[k.slice(2)], `dark ${k}`);
    assert.deepEqual(Object.keys(d).sort(), Object.keys(DARK_MEDIA).sort(), "the two dark blocks set the same names");
  }
  for (const k of Object.keys(DARK_MEDIA)) assert.ok(k in LIGHT, `${k} has a light value`);
  // No colour outside the three token blocks (CSS, markup and script).
  const rest = (STYLE.replace(/:root \{[\s\S]*?\n\}/, "").replace(/@media \(prefers-color-scheme: dark\) \{[\s\S]*?\n\}/, "")
    .replace(/:root\[data-theme="dark"\] \{[\s\S]*?\n\}/, "") + BODY.replace(/<!--[\s\S]*?-->/g, "") + SCRIPT);
  assert.deepEqual(rest.match(/#[0-9a-fA-F]{3,8}\b|\b(rgba?|hsla?)\(/g), null);
});

test("every length, opacity and text style of the page is a token", () => {
  const space = group("space"), radius = group("radius"), size = group("size"), glyph = group("glyph");
  const map: Record<string, string> = {
    "--hair": space.hair, "--inset": space.inset, "--gap": space.gap, "--item": space.item, "--group": space.group,
    "--grid": space.grid, "--gutter": space.gutter, "--foot": space.foot, "--pad": space.pad, "--bar": space.bar,
    "--r-photo": radius.photo, "--r-card": radius.card, "--r-sheet": radius.sheet, "--r-round": radius.round,
    "--control": size.control, "--iconBig": size.iconBig, "--topBar": size.topBar, "--header": size.header,
    "--border": size.border, "--tabPadX": size.tabPadX, "--pillPadX": size.pillPadX, "--focus": size.focusBorder,
    "--glyphBox": glyph.iconBox, "--glyphStroke": glyph.stroke, "--glyphSmall": glyph.box, "--iconSmall": size.iconSmall,
  };
  const lengths = Object.entries(LIGHT).filter(([k, v]) => !v.startsWith("#") && k !== "--doodleOpacity" && k !== "--maxWidth");
  for (const [k, v] of lengths) {
    assert.ok(k in map, `${k} is not mapped to a token`);
    assert.equal(px(v), map[k], `${k}`);
  }
  const doodle = group("doodle");
  assert.equal(LIGHT["--doodleOpacity"], doodle.opacity);
  assert.equal(DARK_MEDIA["--doodleOpacity"], doodle.opacityDark);
  // layoutFor: the breakpoints and the widest content of each width.
  const lay = THEME.slice(THEME.indexOf("export function layoutFor"));
  const bounds = [...lay.matchAll(/width >= (\d+)/g)].map((m) => m[1]).sort();
  const widths = [...lay.matchAll(/maxWidth: (\d+)/g)].map((m) => m[1]).sort();
  assert.deepEqual([...STYLE.matchAll(/@media \(min-width: (\d+)px\) \{ :root \{ --maxWidth: (\d+)px/g)].map((m) => m[1]).sort(), bounds);
  assert.deepEqual([...STYLE.matchAll(/--maxWidth: (\d+)px/g)].map((m) => m[1]).sort(), widths);
  // Outside the token block, the only numbers in px are text sizes (checked next) and the breakpoints.
  const outside = STYLE.replace(/:root \{[\s\S]*?\n\}/, "").replace(/--maxWidth: \d+px/g, "").replace(/@font-face[^\n]*/g, "");
  const typeRows = [...THEME.matchAll(/^\s*(\w+): \{ fontFamily: font\.(\w+), fontSize: (\d+), lineHeight: (\d+) \}/gm)]
    .map((m) => ({ name: m[1], weight: m[2] === "semibold" ? "600" : "400", size: m[3], line: m[4] }));
  const allowed = new Set([...bounds, ...typeRows.flatMap((t) => [t.size, t.line])]);
  for (const m of outside.matchAll(/(\d+(?:\.\d+)?)px/g)) assert.ok(allowed.has(m[1]), `a length that is not a token: ${m[0]}`);
  // Each text class is one of the type tokens (weight, size and line height together).
  for (const m of STYLE.matchAll(/font-weight: (\d+); font-size: (\d+)px; line-height: (\d+)px/g)) {
    assert.ok(typeRows.some((t) => t.weight === m[1] && t.size === m[2] && t.line === m[3]), `not a type token: ${m[0]}`);
  }
  const display = typeRows.find((t) => t.name === "display");
  assert.deepEqual(display, { name: "display", weight: "600", size: "22", line: "28" }, "type.display (T9, Oct 30: 22)");
  assert.match(STYLE, /\.display \{ font-weight: 600; font-size: 22px; line-height: 28px;/);
  assert.match(BODY, /<h1 class="display"/);
  // Opacity: hover and pressed are the opacity tokens; nothing else uses a fraction.
  const opacity = group("opacity");
  assert.match(STYLE, new RegExp("\\.pill:hover \\{ opacity: " + opacity.hover.replace(".", "\\.") + "; \\}"));
  assert.match(STYLE, new RegExp("\\.pill:active \\{ opacity: " + opacity.pressed.replace(".", "\\.") + "; \\}"));
  const fractions = outside.replace(/opacity: (0\.92|0\.85);/g, "").replace(/--doodleOpacity: [\d.]+;/g, "").match(/(?<![\w.#-])\d*\.\d+(?!\w)/g);
  assert.equal(fractions, null, `a fraction that is not a token: ${fractions}`);
});

test("the guide lists type.display with its value", () => {
  const row = GUIDE.split("\n").find((l) => l.startsWith("| `type.display` |"));
  assert.ok(row, "style_guide.md has no type.display row");
  assert.ok(row.includes("| 22 / 28 |"), row);
  assert.match(GUIDE, /^## About page \(`mobile\/public\/about\/index\.html`/m);
});

test("the backdrop is the app's doodle wallpaper: the same drawings, rule, seed and tokens", () => {
  const doodles = readFileSync(join(ROOT, "src/doodles.tsx"), "utf8");
  const app = [...doodles.matchAll(/^\s+"(M[^"]+)",\s*\/\//gm)].map((m) => m[1]);
  const page = [...SCRIPT.slice(SCRIPT.indexOf("var D = ["), SCRIPT.indexOf("];")).matchAll(/"(M[^"]+)"/g)].map((m) => m[1]);
  assert.equal(app.length, 20);
  assert.deepEqual(page, app);
  const d = group("doodle");
  const t = SCRIPT.match(/var T = \{([^}]*)\}/)![1];
  const want: Record<string, string> = { cell: d.cell, glyph: d.glyph, units: d.units, stroke: d.stroke, jitter: d.jitter,
    turn: d.turn, steps: d.turnSteps, seed: d.seed, density: d.rowDensity, faint: d.faint, stepMs: d.stepMs };
  for (const [k, v] of Object.entries(want)) assert.match(t, new RegExp("\\b" + k + ": " + v.replace(".", "\\.") + "\\b"), k);
  // The automaton: the hash and the generator of automata.ts, rule 30, one row up each step, still with reduce motion.
  const auto = readFileSync(join(ROOT, "src/automata.ts"), "utf8");
  for (const k of ["1664525", "1013904223", "2166136261", "16777619", "0x85ebca6b", "0xc2b2ae35"]) {
    assert.ok(auto.includes(k) && SCRIPT.includes(k), k);
  }
  assert.match(SCRIPT, /prefers-reduced-motion: reduce/);
  assert.match(PAGE, /<svg class="backdrop" aria-hidden="true" focusable="false"><\/svg>/);
  assert.match(STYLE, /svg\.backdrop \{ position: fixed; inset: 0;[^}]*opacity: var\(--doodleOpacity\)/);
  assert.match(SCRIPT, /style="stroke: var\(--doodle\)"/); // the colour follows the mode (a CSS variable, not an attribute)
  assert.match(STYLE, /\nbody \{\n  margin: 0; background: var\(--bg\);/);
});

test("the backdrop draws the same pattern as the app, and each step moves it up one row", () => {
  // The pure part of the page script runs here and is compared with automata.ts (the app's Doodles uses it).
  const pure = SCRIPT.slice(SCRIPT.indexOf("// ---- pure: start"), SCRIPT.indexOf("// ---- pure: end"));
  const defs = SCRIPT.slice(SCRIPT.indexOf("var D = ["), SCRIPT.indexOf("// ---- pure: start"));
  assert.doesNotMatch(pure, /\bdocument\.|\bwindow\.|\bsvg\.|matchMedia|setInterval/, "the pure part does not touch the page");
  const page = vm.runInNewContext(defs + pure + "({ startGrid: startGrid, stepUp: stepUp, marks: marks, T: T })");
  const d = group("doodle");
  const plan = { cell: Number(d.cell), drawings: 20, jitter: Number(d.jitter), turn: Number(d.turn), steps: Number(d.turnSteps),
    seed: Number(d.seed), density: Number(d.rowDensity) };
  for (const [w, h] of [[390, 844], [1440, 900], [38, 38], [77, 400]]) {
    const grid = page.startGrid(w, h);
    assert.deepEqual(JSON.parse(JSON.stringify(grid)), startGrid(w, h, plan), `grid ${w}x${h}`);
    assert.deepEqual(JSON.parse(JSON.stringify(page.marks(grid))), wallpaper(w, h, plan), `drawings ${w}x${h}`);
    let a = grid, b = startGrid(w, h, plan);
    for (let i = 0; i < 5; i++) { a = page.stepUp(a); b = stepUp(30, b); }
    assert.deepEqual(JSON.parse(JSON.stringify(a)), b, `5 steps ${w}x${h}`);
    assert.deepEqual(JSON.parse(JSON.stringify(page.marks(a))), wallpaper(w, h, plan, b), `drawings after 5 steps ${w}x${h}`);
  }
  // The page keeps the drawings and changes only live/dead on a step (as the app: "the drawings stay").
  assert.match(SCRIPT, /grid = stepUp\(grid\);\n    var i = 0; grid\.forEach\(function \(line\) \{ line\.forEach\(function \(alive\) \{ if \(paths\[i\]\) paths\[i\]\.setAttribute\("stroke-opacity"/);
});

test("the fonts are the app's Poppins files from the web build", () => {
  const faces = [...STYLE.matchAll(/@font-face \{ font-family: "Poppins"; font-weight: (\d+);[^}]*url\("([^"]+)"\)/g)];
  assert.deepEqual(faces.map((f) => f[1]), ["400", "600"]);
  const font = group("font");
  assert.ok(faces[0][2].includes("/" + font.regular + "."), faces[0][2]);
  assert.ok(faces[1][2].includes("/" + font.semibold + "."), faces[1][2]);
  for (const f of faces) {
    assert.match(f[2], /^\/assets\/node_modules\/@expo-google-fonts\/poppins\/\w+\/\w+\.[0-9a-f]{32}\.ttf$/);
    assert.ok(existsSync(join(ROOT, "web-dist", f[2])), "web-dist has no " + f[2]);
  }
});

test("the words: T9's decisions (Oct 30) and the hackathon rules", () => {
  const text = BODY.replace(/<!--[\s\S]*?-->/g, "").replace(/<[^>]+>/g, " ");
  // X72 (T9 Nov 3): the Facebook Page exists (#88) and is linked from "How posting works" (it was "no Facebook until the
  // service account", Oct 30); the only Facebook address is the V3RDE Page
  const fb = [...BODY.matchAll(/href="(https:\/\/www\.facebook\.com\/[^"]*)"/g)].map((m) => m[1]);
  assert.deepEqual(fb, ["https://www.facebook.com/profile.php?id=61595838462229"]);
  assert.doesNotMatch(text, /374,760|products\. A hit/, "the food DB has no product count (T9)");
  assert.match(text, /Food DB/);
  assert.doesNotMatch(text, /\bsafe to eat\b|\bhealthy\b|\bnutrition|\bcalorie/i); // no safety, health or nutrition claims (anti-projects)
  assert.match(text, /written again as new code/);       // new work only: the earlier parts are named
  assert.equal((BODY.match(/<h1\b/g) ?? []).length, 1);
  assert.match(PAGE, /<html lang="en">/);
  assert.match(BODY, /<main id="main">/);
  // "Watch the demo" stays hidden until the video exists; then only an https address shows it.
  assert.match(BODY, /<a class="pill secondary label" href="https:\/\/youtu\.be\/_WzjoTMUfCg" data-testid="demo">Watch the demo<\/a>/); // X72
  const demo = SCRIPT.match(/var DEMO_URL = "([^"]*)";/);
  assert.ok(demo && (demo[1] === "" || /^https:\/\//.test(demo[1])), "DEMO_URL is empty or https");
  assert.match(SCRIPT, /\/\^https:\\\/\\\/\/\.test\(DEMO_URL\)/);
  // Pills: secondary before primary in each row (style guide: secondary left, primary right).
  const row = BODY.match(/<div class="row">([\s\S]*?)<\/div>/)![1];
  const kinds = [...row.matchAll(/class="pill( secondary)? label"/g)].map((m) => (m[1] ? "secondary" : "primary"));
  assert.deepEqual(kinds, ["secondary", "secondary", "primary"]);
  // Every decorative drawing is hidden from screen readers.
  for (const s of BODY.match(/<svg\b[^>]*>/g) ?? []) assert.match(s, /aria-hidden="true"/, s);
});

// ---- A2 (#81), X68 (T9 Nov 2: "update the arch diagrams on this about page and clean it up. its way too busy"; the
// mockup "V3RDE About Page Mockup" approved): the problem line; "How it is built" is one drawing of three layers, top to
// bottom; no step-by-step flows, no problem cards, no "Measured" numbers.
const ARCH = BODY.slice(BODY.indexOf('<section aria-labelledby="h-arch">'), BODY.indexOf('<section aria-labelledby="h-new">'));
const DOWN = "M10 2 V17 M5 12 L10 17 L15 12";

test("the problem is stated; the page has no problem cards, no step-by-step flows and no numbers section (X68)", () => {
  const text = BODY.replace(/<!--[\s\S]*?-->/g, "").replace(/<[^>]+>/g, " ").replace(/\s+/g, " ");
  assert.match(text, /Posting food should take seconds/);
  assert.match(text, /V3RDE uses Meta's AI models to do that work for you/);
  assert.doesNotMatch(text, /One photo, not a form|Nothing left behind|Measured, not guessed/);
  assert.doesNotMatch(BODY, /<details\b|class="stats"|h-num|class="node/);
  assert.doesNotMatch(STYLE, /\.more |\.flow|\.stats|\.node/); // no CSS left for the parts that went
  assert.ok(ARCH.length > 0, "the drawing is before the Hackathon section");
  // X68 review: the page says "Seven languages"; it must stay true of i18n/languages.json (X49x: T9 added Yoruba)
  const langs = JSON.parse(readFileSync(join(ROOT, "../i18n/languages.json"), "utf8")).languages as { name: string }[];
  assert.equal(langs.length, 7, "the page says Seven languages");
  assert.match(text, /Seven languages/);
});

test("X68: How it is built is one drawing: phone or browser, our server (four groups), Meta's AI models", () => {
  const layers = [...ARCH.matchAll(/<div class="layer"><p class="label">([^<]+)<\/p>/g)].map((m) => m[1]);
  assert.deepEqual(layers, ["Your phone or browser", "Our server", "Meta's AI models"]);
  const groups = [...ARCH.matchAll(/<div class="grp"><p class="label">([^<]+)<\/p>/g)].map((m) => m[1]);
  assert.deepEqual(groups, ["Front door", "Your food", "Your listings", "What we learn"]);
  // the parts of #115 that the old drawing did not have (read from the code; NEW_WORK.md 93-94, 97, 98, 69)
  for (const part of ["Finds an item it missed", "Live updates in the web app", "A daily report of donor changes", "New text translated after each update",
    "Seven languages", "Food DB for barcodes", "Rescue to a food program", "Live for 30 minutes"]) assert.ok(ARCH.includes(part), part);
  assert.match(STYLE, /\.groups \{ display: grid; grid-template-columns: 1fr;/);
  assert.match(STYLE, /@media \(min-width: 840px\) \{ \.groups \{ grid-template-columns: repeat\(4, 1fr\); \} \}/);
});

test("X68: the drawing reads top to bottom: two arrows, both down; no other drawing", () => {
  const arrows = [...ARCH.matchAll(/<div class="down" aria-hidden="true"><svg[^>]*><path d="([^"]+)"\/><\/svg><\/div>/g)];
  assert.equal(arrows.length, 2);
  const all = [...ARCH.matchAll(/<path d="([^"]+)"\/>/g)].map((m) => m[1]);
  assert.deepEqual(all, [DOWN, DOWN]);
});

test("a Meta model has the red border; the models are the ones of NEW_WORK.md; PE-Core runs on our server", () => {
  const work = readFileSync(join(ROOT, "../NEW_WORK.md"), "utf8");
  const ai = [...ARCH.matchAll(/<li class="chip ai">([^<]+)<\/li>/g)].map((m) => m[1]);
  assert.equal(ai.length, 5);
  for (const [i, model] of ["SAM 3.1", "Muse Spark 1.3", "Muse Image 1.0", "Muse Voice Transcribe 1.0", "Muse Spark Open"].entries()) {
    assert.ok(ai[i].startsWith(model + " "), ai[i]);
    assert.ok(work.includes(model), "NEW_WORK.md names " + model);
  }
  const server = ARCH.slice(ARCH.indexOf('<p class="label">Our server</p>'), ARCH.indexOf("Meta's AI models</p>"));
  assert.ok(server.includes("Food check (Meta's PE-Core)") && work.includes("PE-Core"), "PE-Core is on our server, named as Meta's");
  assert.match(ARCH, /A red border is a Meta AI model that our server calls; Meta's PE-Core runs on our server\./);
  assert.ok(!/class="chip ai"/.test(server) && !/class="chip ai"/.test(ARCH.slice(0, ARCH.indexOf('<p class="label">Our server</p>'))));
  assert.match(STYLE, /\.chip\.ai \{ border-color: var\(--accent\); \}/);
});

test("the sign-in screen links to the page", () => {
  assert.equal(SIGN_IN.about, "About");
  assert.equal(ABOUT_PATH, "/about/");
  assert.equal(ABOUT_URL, "https://v3rde.thehungreeapp.com" + ABOUT_PATH);
  const login = readFileSync(join(ROOT, "src/screens/Login.tsx"), "utf8");
  assert.match(login, /<TextLink testID="about" onPress=\{openAbout\}>\{SIGN_IN\.about\}<\/TextLink>/);
  assert.match(login, /window\.location\.assign\(ABOUT_PATH\)/);
  assert.match(login, /Linking\.openURL\(ABOUT_URL\)/);
  // The page links back to the app (the same site).
  assert.ok((BODY.match(/href="\/"/g) ?? []).length >= 3);
});

// A4 (T9 2026-10-31): the page matches the app after #81 (no ZIP code anywhere) and credits the product photos of
// a barcode scan (#83 of reach: Open Food Facts, CC BY-SA; "Credit on the About page").
test("no ZIP code on the page; the Open Food Facts photo credit is in the footer", () => {
  assert.ok(!/\bZIP\b/i.test(PAGE), "the page still names a ZIP code");
  const foot = PAGE.slice(PAGE.indexOf("<footer>"), PAGE.indexOf("</footer>"));
  assert.ok(/Open Food Facts \(openfoodfacts\.org\)/.test(foot) && foot.includes("(CC BY-SA)"), "the credit line");
  // X70 (T9 Nov 2: "remove made by the hungree app from the bottom of the about page"): no by-line; the label of the
  // earlier brand assets stays (the Hackathon rules: earlier assets are labelled)
  assert.doesNotMatch(foot, /V3RDE by|Hungree LLC/);
  assert.match(foot, /are Hungree brand assets from before the Hackathon/);
});

// X72 (T9 Nov 3: "the schematics on here are bad. lets use the images we created in the story board ... v15 for the
// video"; "there should be a technical toggle and a simple one but dont use that word"; "in the overview you add the
// summary of what this web app does"): two views, Overview first, radio buttons and CSS only.
test("X72: How it is built has two views: Overview (first, with the summary) and In depth (the six drawings of the video)", () => {
  const labels = [...ARCH.matchAll(/<label for="(v-over|v-deep)" class="label">([^<]+)<\/label>/g)].map((m) => m[1] + ":" + m[2]);
  assert.deepEqual(labels, ["v-over:Overview", "v-deep:In depth"]);
  // X72 review: one group with a name (fieldset and legend); each radio has its label right after it
  assert.match(ARCH, /<fieldset class="views"><legend class="sr">How it is built: choose a view<\/legend><input class="vr" type="radio" name="arch-view" id="v-over" checked><label for="v-over" class="label">Overview<\/label><input class="vr" type="radio" name="arch-view" id="v-deep"><label for="v-deep" class="label">In depth<\/label><\/fieldset>/);
  assert.doesNotMatch(ARCH.replace(/<!--[\s\S]*?-->/g, ""), /\b[Ss]imple\b|\b[Tt]echnical\b/); // T9: not those words
  assert.match(STYLE, /section:has\(#v-over:checked\) \.v-deep, section:has\(#v-deep:checked\) \.v-over \{ display: none; \}/);
  assert.match(STYLE, /\.vr:focus-visible \+ label \{ outline: var\(--focus\) solid var\(--link\)/);
  const over = ARCH.slice(ARCH.indexOf('<div class="view v-over">'), ARCH.indexOf('<div class="view v-deep">'));
  assert.match(over, /<p class="body summary">V3RDE gets extra food to people near it\./);
  assert.ok(over.includes('<div class="layer"><p class="label">Your phone or browser</p>'), "the three-layer drawing is the Overview");
  const deep = ARCH.slice(ARCH.indexOf('<div class="view v-deep">'));
  // X72 review: each picture opens full size (on a phone the drawing is small)
  const figs = [...deep.matchAll(/<figure class="card fig"><h3 class="label">([^<]+)<\/h3><p class="body muted">[\s\S]*?<\/p><a class="zoom" href="img\/([a-z-]+)\.png" aria-label="[^"]+: open the drawing full size"><img src="img\/\2\.png" width="(\d+)" height="(\d+)" loading="lazy" alt="([^"]+)"><\/a><\/figure>/g)];
  assert.deepEqual(figs.map((m) => m[2]), ["graph", "translate", "posting", "rescue", "learns", "under-the-hood"]);
  for (const [, title, file, w, h, alt] of figs) {
    const png = readFileSync(join(ROOT, "public/about/img/" + file + ".png"));
    assert.equal(png.readUInt32BE(16) + "x" + png.readUInt32BE(20), w + "x" + h, file + ": width and height are the file's");
    assert.ok(alt.length > 100, title + ": the picture has a full text alternative");
  }
  // the page's one inline script is the same, so the Caddyfile CSP hash still allows it (edge.test checks the build)
  const caddy = readFileSync(join(ROOT, "../deploy/Caddyfile"), "utf8");
  const body = PAGE.match(/<script>([\s\S]*?)<\/script>/)![1];
  const hash = "'sha256-" + createHash("sha256").update(body, "utf8").digest("base64") + "'";
  assert.ok(caddy.includes(hash), "the Caddyfile CSP allows the about page script " + hash);
});

