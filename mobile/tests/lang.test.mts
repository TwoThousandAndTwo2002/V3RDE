// X49x (T9 Nov 1): the app's words in the chosen language (src/lang.ts), with English when a text has no translation.
import test from "node:test";
import { strict as assert } from "node:assert";
import { readFileSync } from "node:fs";
import { SOURCE, TABLES, applyTexts, fill, languageNow, switchLanguage, languagesOf, shortName, startLanguage, textsOf, valuesOf } from "../src/lang.ts";
import { COUNT, GIVE, LANG, UNITS, VOICE, servings } from "../src/words.ts";
import { SIGN_IN } from "../src/signin.ts";
import { retryText } from "../src/logic.ts";

// the English templates of a round (the ids and texts as the extractor gives them) and a made-up language "xx"
const en = {
  "SIGN_IN.title": "Share or find a meal",
  "SIGN_IN.rule": "{MIN_PASSWORD} characters or more",
  "GIVE.removeName": "Delete {title}",
  "VOICE.rename": "Item {n}: “{from}” to “{to}”",
  "UNITS.servings.1_serving": "1 serving",
  "UNITS.servings.servings": "{n} servings",
  "COUNT.found.1_item_found": "1 item found",
  "COUNT.found.items_found": "{n} items found",
  "VOICE.hints.talk_or_type_a": "Talk or type a change",
  "VOICE.hints.try_6_servings": "Try “6 servings”",
  "SCAN.busy": "The scanner is busy. Wait a few seconds — your photo is kept.",
  "LANG.title": "Language",
};
const xx = {
  "SIGN_IN.title": "XX meal",
  "SIGN_IN.rule": "XX {MIN_PASSWORD} or more",
  "GIVE.removeName": "XX delete «{title}»",
  "VOICE.rename": "{to} ← {from} (XX {n})", // another word order
  "UNITS.servings.1_serving": "XX one serving",
  "UNITS.servings.servings": "XX {n} servings",
  "COUNT.found.items_found": "XX {n} found", // the one-item text has no translation: it stays English
  "VOICE.hints.talk_or_type_a": "XX talk",
  "SCAN.busy": "   ", // an empty translation is no translation
  "LANG.title": "XX language",
};

test("fill and valuesOf: the values of an English text go into the translated template", () => {
  assert.equal(fill("Item {n} of {m}", { n: "2", m: "5" }), "Item 2 of 5");
  assert.equal(fill("Hi {name}", {}), "Hi {name}");
  assert.deepEqual(valuesOf("Item {n}: “{from}” to “{to}”", "Item 2: “Rice” to “Jollof rice”"),
    { n: "2", from: "Rice", to: "Jollof rice" });
  assert.deepEqual(valuesOf("{d} mi", "0.4 mi"), { d: "0.4" });
  assert.deepEqual(valuesOf("1 item found", "1 item found"), {});
  assert.equal(valuesOf("{n} items found", "1 item found"), null);
  assert.deepEqual(valuesOf("a.b (c)? {x}", "a.b (c)? *"), { x: "*" }); // special characters in the template are literal
});

test("a language changes every table in place; a missing text stays English; English comes back", () => {
  applyTexts("xx", en, xx);
  assert.equal(languageNow(), "xx");
  assert.equal(SIGN_IN.title, "XX meal");
  assert.equal(SIGN_IN.rule, "XX 8 or more"); // the value of the English string (8) goes back in
  assert.equal(GIVE.removeName("Rice"), "XX delete «Rice»");
  assert.equal(VOICE.rename(2, "Rice", "Jollof"), "Jollof ← Rice (XX 2)");
  assert.equal(servings(1), "XX one serving");
  assert.equal(UNITS.servings(6), "XX 6 servings");
  assert.equal(COUNT.found(3), "XX 3 found");
  assert.equal(COUNT.found(1), "1 item found"); // no translation for this case
  assert.equal(VOICE.hints[0], "XX talk");
  assert.equal(VOICE.hints[1], "Try “6 servings”");
  assert.match(retryText(503, { code: "busy" }), /^The scanner is busy\./); // blank translation: English
  assert.equal(LANG.title, "XX language");
  assert.equal(SIGN_IN.email, "Email"); // not in the file: English
  applyTexts("en", {}, {});
  assert.equal(languageNow(), "en");
  assert.equal(SIGN_IN.title, "Share or find a meal");
  assert.equal(SIGN_IN.rule, "8 characters or more");
  assert.equal(GIVE.removeName("Rice"), "Delete Rice");
  assert.equal(VOICE.rename(2, "Rice", "Jollof"), "Item 2: “Rice” to “Jollof”");
  assert.equal(COUNT.found(3), "3 items found");
  assert.equal(VOICE.hints[0], "Talk or type a change");
});

test("a second language replaces the first (the English is the base, not the last language)", () => {
  applyTexts("xx", en, xx);
  applyTexts("yy", en, { "SIGN_IN.title": "YY meal" });
  assert.equal(SIGN_IN.title, "YY meal");
  assert.equal(SIGN_IN.rule, "8 characters or more");
  assert.equal(GIVE.removeName("Rice"), "Delete Rice");
  applyTexts("en", {}, {});
});

test("the start language: the saved choice, then the browser languages, then English", () => {
  const have = languagesOf({ languages: [{ code: "en", name: "English", native: "English", dir: "ltr" },
    { code: "es-419", name: "Spanish (Latin America)", native: "español (Latinoamérica)", dir: "ltr" },
    { code: "pt-BR", name: "Portuguese (Brazil)", native: "português (Brasil)", dir: "ltr" },
    { code: "zh-Hans", name: "Chinese (Simplified)", native: "中文（简体）", dir: "ltr" },
    { code: "ar", name: "Arabic (Modern Standard)", native: "العربية", dir: "rtl" },
    { code: "yo", name: "Yoruba", native: "Yorùbá", dir: "ltr" }] });
  assert.equal(startLanguage("yo", ["es-MX"], have), "yo");
  assert.equal(startLanguage("de", ["es-MX"], have), "es-419"); // a saved code the box no longer has
  assert.equal(startLanguage(null, ["es-MX", "en-US"], have), "es-419");
  assert.equal(startLanguage(null, ["pt-PT"], have), "pt-BR");
  assert.equal(startLanguage(null, ["zh-CN"], have), "zh-Hans");
  assert.equal(startLanguage(null, ["zh-TW", "fr"], have), "en"); // traditional script is not zh-Hans
  assert.equal(startLanguage(null, ["AR-eg"], have), "ar");
  assert.equal(startLanguage(null, [], have), "en");
  assert.equal(have.find((l) => l.code === "ar")?.dir, "rtl");
});

test("the language button shows the short name (T9: español, not español (Latinoamérica))", () => {
  assert.equal(shortName("espa\u00f1ol (Latinoam\u00e9rica)"), "espa\u00f1ol");
  assert.equal(shortName("portugu\u00eas (Brasil)"), "portugu\u00eas");
  assert.equal(shortName("\u4e2d\u6587\uff08\u7b80\u4f53\uff09"), "\u4e2d\u6587");
  assert.equal(shortName("Yor\u00f9b\u00e1"), "Yor\u00f9b\u00e1");
  assert.equal(shortName("(x)"), "(x)");
  const button = readFileSync(new URL("../src/language.tsx", import.meta.url), "utf8");
  assert.match(button, /\{shortName\(now\.native\)\}<\/Text><\/Pressable>/);
});

test("bad files: English only; the source language is always in the list", () => {
  assert.deepEqual(languagesOf(null), [SOURCE]);
  assert.deepEqual(languagesOf({ languages: [{ code: "yo", name: "Yoruba", native: "Yorùbá", dir: "up" }] }),
    [SOURCE, { code: "yo", name: "Yoruba", native: "Yorùbá", dir: "ltr" }]);
  assert.deepEqual(textsOf({ messages: { a: "x", b: 3 } }), { a: "x" });
  assert.deepEqual(textsOf("nope"), {});
});

test("every word table of the app is registered, and no table holds anything but words", () => {
  const src = (f: string) => readFileSync(new URL("../src/" + f, import.meta.url), "utf8");
  for (const f of ["words.ts", "signin.ts", "capture.ts", "logic.ts", "rescue.ts"]) {
    for (const m of src(f).matchAll(/^export const ([A-Z][A-Z_]+) = \{/gm)) {
      if (["LIMITS", "UPLOAD"].includes(m[1]) || m[1].endsWith("_TABLES")) continue; // numbers, and the lists themselves
      assert.ok(m[1] in TABLES, f + ": " + m[1] + " is not in lang.ts TABLES");
    }
  }
  for (const [name, t] of Object.entries(TABLES)) {
    for (const [k, v] of Object.entries(t)) {
      const ok = typeof v === "string" || typeof v === "function" || (Array.isArray(v) && v.every((x) => typeof x === "string"));
      assert.ok(ok, name + "." + k);
    }
  }
});

test("the language choice is on the sign-in row and in the role sheet, and Arabic gets right to left", () => {
  const login = readFileSync(new URL("../src/screens/Login.tsx", import.meta.url), "utf8");
  const role = readFileSync(new URL("../src/screens/Role.tsx", import.meta.url), "utf8");
  const app = readFileSync(new URL("../App.tsx", import.meta.url), "utf8");
  const lang = readFileSync(new URL("../src/lang.ts", import.meta.url), "utf8");
  assert.match(login, /<LanguageButton \/>\s*<TextLink testID="about"/);
  assert.match(role, /<LanguageButton testID="menu-language" look="pill" \/>[\s\S]*testID="logout"/);
  assert.match(app, /<LanguageSheet onPick=\{pickLanguage\}/);
  assert.match(lang, /document\.documentElement\.dir = l\.dir;/);
  // react-native-web flips start and end inside a View with dir (LocaleProvider)
  assert.match(app, /<View \{\.\.\.\(\{ dir: lang\.dir, lang: lang\.code \} as object\)\} style/);
  // X49x review: no new key on a language change (the typed email, the lists and the popups stay), and one sheet at a time
  assert.doesNotMatch(app, /key=\{lang\.code\}/);
  assert.match(app, /stage\.kind === "role" && !langSheet \?/);
  assert.match(app, /stage\.kind === "in" && stage\.menu && !langSheet \?/);
  // the text and the field slots use start and end, so they flip for Arabic
  const ui = readFileSync(new URL("../src/ui.tsx", import.meta.url), "utf8");
  assert.doesNotMatch(ui, /textAlign: center \? "center" : "left"/);
  assert.match(ui, /paddingStart: size\.fieldPadX,\s*paddingEnd: right \? size\.fieldSlot : size\.fieldPadX,/);
});

test("X49x review: every text of the app shows its translation (each string, list item and word function)", () => {
  const all = JSON.parse(readFileSync(new URL("./app_texts.json", import.meta.url), "utf8")) as Record<string, string>;
  const to: Record<string, string> = {};
  for (const [k, v] of Object.entries(all)) to[k] = "XX[" + v + "]";
  applyTexts("xx", all, to);
  const samples: unknown[][] = [[1], [2], [0], [1, 1], [2, 3], [1, 3], [3, 1], ["a.b*c$1 (x)"], ["8"], [75], [["a", "b"]], ["1", "2", "3", "4"]];
  const english: string[] = [];
  try {
    for (const [n, t] of Object.entries(TABLES)) for (const [k, v] of Object.entries(t)) {
      if (typeof v === "string" && !v.startsWith("XX[")) english.push(n + "." + k + ": " + v);
      if (Array.isArray(v)) v.forEach((x, i) => { if (!String(x).startsWith("XX[")) english.push(n + "." + k + "[" + i + "]: " + x); });
      if (typeof v === "function" && n + "." + k !== "RESCUE.linkName") { // linkName only joins two texts with ", " (no words)
        for (const a of samples) {
          let out: unknown;
          try { out = (v as (...x: unknown[]) => unknown)(...a); } catch { continue; }
          if (typeof out === "string" && out !== "" && !out.startsWith("XX[")) english.push(n + "." + k + "(" + JSON.stringify(a) + "): " + out);
        }
      }
    }
  } finally { applyTexts("en", {}, {}); }
  assert.deepEqual(english, []);
});

test("X49x review: the last choice wins over a slow load, and a file with no texts is English (page too)", async () => {
  const real = globalThis.fetch;
  const files: Record<string, { ms: number; body: unknown }> = {
    "/i18n/en.json": { ms: 0, body: { messages: { "SIGN_IN.title": "Share or find a meal" } } },
    "/i18n/slow.json": { ms: 60, body: { messages: { "SIGN_IN.title": "SLOW meal" } } },
    "/i18n/fast.json": { ms: 0, body: { messages: { "SIGN_IN.title": "FAST meal" } } },
    "/i18n/empty.json": { ms: 0, body: { messages: {} } },
  };
  globalThis.fetch = (async (path: string) => {
    const f = files[path];
    await new Promise((go) => setTimeout(go, f?.ms ?? 0));
    return f ? new Response(JSON.stringify(f.body), { status: 200 }) : new Response("", { status: 404 });
  }) as typeof fetch;
  try {
    const slow = switchLanguage({ code: "slow", name: "Slow", native: "Slow", dir: "ltr" });
    const fast = await switchLanguage({ code: "fast", name: "Fast", native: "Fast", dir: "rtl" });
    assert.equal(fast?.code, "fast");
    assert.equal(await slow, null); // the first load ended last: it changes nothing
    assert.equal(SIGN_IN.title, "FAST meal");
    assert.equal((await switchLanguage({ code: "empty", name: "Empty", native: "Empty", dir: "rtl" }))?.code, "en");
    assert.equal(SIGN_IN.title, "Share or find a meal");
    assert.equal((await switchLanguage({ code: "gone", name: "Gone", native: "Gone", dir: "ltr" }))?.code, "en"); // 404
  } finally {
    globalThis.fetch = real;
    applyTexts("en", {}, {});
  }
});

// X65 (T9 Nov 2, bug 21: "users cannot change the application language after selecting a role"; "the same oval style as
// the others"): the language pill in the role sheet (phone) and in the top bar next to Log out (wide screen)
test("X65: the language choice is an oval pill where the role changes: the role sheet and the top bar", () => {
  const src = (f: string) => readFileSync(new URL("../src/" + f, import.meta.url), "utf8");
  const frame = src("screens/Frame.tsx"), lang = src("language.tsx"), role = src("screens/Role.tsx");
  // the wide top bar: the language oval, then Log out (the top bar has no role sheet)
  assert.match(frame, /<LanguageButton testID="topbar-language" look="bar" \/>\s*<Pressable testID="topbar-logout"/);
  const bar = frame.slice(frame.indexOf("const bar = page.topBar ?"), frame.indexOf(") : ("));
  assert.ok(bar.includes("topbar-language"), "the language oval is in the top bar");
  // the role sheet: the pill is not centred as a link any more (the width of the sheet, as Log out)
  assert.doesNotMatch(role, /alignItems: "center" \}\}><LanguageButton/);
  // the ovals use the tokens of the pills they sit next to (Pill secondary; the top-bar Log out)
  assert.match(lang, /height: size\.control, borderRadius: radius\.round, borderWidth: size\.border, borderColor: col\.line/);
  assert.match(lang, /paddingHorizontal: look === "pill" \? size\.pillPadX : size\.tabPadX/);
  assert.match(lang, /backgroundColor: look === "pill" \? col\.card : isHovered\(state\) \? col\.iconFill : col\.bg/);
  assert.match(frame, /height: size\.control, paddingHorizontal: size\.tabPadX, borderRadius: radius\.round/); // its neighbour
  // the sign-in row keeps the link (the default look), and every look keeps one name for screen readers
  assert.match(src("screens/Login.tsx"), /<LanguageButton \/>/);
  assert.match(lang, /look = "link"/);
  assert.equal((lang.match(/accessibilityLabel=\{LANG\.name\(now\.native\)\}/g) || []).length, 1);
});


// X65 review: on a wide screen the language oval can open its sheet over a dialog of the role panel (a listing edit):
// only the newest popup takes Esc and the Tab focus, so one Esc closes the language sheet and not the edit under it
test("X65 review: two popups at once: only the newest takes Esc and Tab", async () => {
  const { pushPopup, isTopPopup } = await import("../src/popupstack.ts");
  const edit = {}, sheet = {};
  const offEdit = pushPopup(edit);
  assert.equal(isTopPopup(edit), true);
  const offSheet = pushPopup(sheet);
  assert.equal(isTopPopup(sheet), true);
  assert.equal(isTopPopup(edit), false); // Esc and Tab go to the language sheet only
  offSheet();
  assert.equal(isTopPopup(edit), true); // the edit gets them back
  offEdit();
  assert.equal(isTopPopup(edit), false);
  const a = {}, b = {}; const offA = pushPopup(a); const offB = pushPopup(b); offA(); // out of order
  assert.equal(isTopPopup(b), true); offB(); assert.equal(isTopPopup(b), false);
  const ui = readFileSync(new URL("../src/ui.tsx", import.meta.url), "utf8");
  assert.match(ui, /useEffect\(\(\) => pushPopup\(token\), \[token\]\);/);
  assert.match(ui, /e\.key === "Escape" && isTopPopup\(token\) && onDismiss\(\)/);
  assert.match(ui, /if \(e\.key !== "Tab" \|\| !top\(\)\) return;/);
  assert.match(ui, /trapFocus\(box\.current as unknown as HTMLElement \| null, \(\) => isTopPopup\(token\)\)/);
  // a long name or a long Log out never pushes the tabs: the right group and the oval can shrink
  const frame = readFileSync(new URL("../src/screens/Frame.tsx", import.meta.url), "utf8");
  assert.match(frame, /flexGrow: 1, flexBasis: 0, minWidth: 0, flexDirection: "row"/);
});
