// X49x (T9 Nov 1: "translations should be available for the app"; all 349 texts; Arabic right to left; Yoruba): the
// app shows its words in the language the user chooses. The box publishes /i18n/index.json (the languages) and
// /i18n/<code>.json (id -> text) after each translation round (i18n/round, NEW_WORK 69). Every app text lives in a
// word table (an exported object of strings and word functions, for example SIGN_IN or COUNT); its id is
// "<TABLE>.<key>" (or "<TABLE>.<key>.<branch>" for a function with more than one text), the same id as the
// extractor gives (i18n/extract.go). A string gets the translation with its values put back; a word function
// still runs in English (a list of strings: each one is a branch id), and its English text is matched to the English template to find the values, which then go
// into the translated template. A text with no translation stays English. No react-native import: the tests load it.
import { CAPTURE_TABLES } from "./capture";
import { LOGIC_TABLES } from "./logic";
import { RESCUE_TABLES } from "./rescue";
import { SIGNIN_TABLES } from "./signin";
import { WORD_TABLES } from "./words";

export type Language = { code: string; name: string; native: string; dir: "ltr" | "rtl" };
export type Texts = Record<string, string>;
type Table = Record<string, unknown>;

/** Every word table of the app, by its name (the first part of the id). */
export const TABLES: Record<string, Table> = { ...WORD_TABLES, ...SIGNIN_TABLES, ...CAPTURE_TABLES, ...LOGIC_TABLES, ...RESCUE_TABLES };

export const SOURCE: Language = { code: "en", name: "English", native: "English", dir: "ltr" };
/** The key of the chosen language in the browser's storage (a per-viewer convenience). */
export const SAVED_KEY = "v3rde.language";
/** Where the box publishes the translations (Caddy handle_path /i18n/*). */
export const I18N_PATH = "/i18n/";

const english = new Map<string, unknown>(); // "<TABLE>.<key>" -> the English value (string or function)
for (const [name, t] of Object.entries(TABLES)) for (const [k, v] of Object.entries(t)) english.set(name + "." + k, v);

let now = SOURCE.code;
/** The code of the language the words are in now. */
export const languageNow = () => now;

const PARAM = /\{([A-Za-z_$][A-Za-z0-9_$]*)\}/g;

/** The template with its {name}s put in from values (a name with no value stays as it is). */
export function fill(template: string, values: Record<string, string>): string {
  return template.replace(PARAM, (all, name: string) => (name in values ? values[name] : all));
}

/** The values of the {name}s of an English template in a text made from it, or null when the text does not fit. */
export function valuesOf(template: string, text: string): Record<string, string> | null {
  const names: string[] = [];
  let re = "^", last = 0;
  for (const m of template.matchAll(PARAM)) {
    re += template.slice(last, m.index).replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    if (names.includes(m[1])) re += "\\k<" + m[1] + ">";
    else { names.push(m[1]); re += "(?<" + m[1] + ">[\\s\\S]*?)"; }
    last = m.index! + m[0].length;
  }
  re += template.slice(last).replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + "$";
  const got = new RegExp(re, "u").exec(text);
  return got ? { ...(got.groups ?? {}) } : null;
}

/** The text in the language: the translation of the first English template (id, or id.<branch>) that the English
 *  text fits, with the values put back; the English text when there is none. */
function say(id: string, englishText: string, en: Texts, to: Texts): string {
  for (const key of [id, ...Object.keys(en).filter((k) => k.startsWith(id + "."))]) {
    const tpl = en[key];
    if (typeof tpl !== "string") continue;
    const values = valuesOf(tpl, englishText);
    if (!values) continue;
    const t = to[key];
    return typeof t === "string" && t.trim() ? fill(t, values) : englishText;
  }
  return englishText;
}

/** Puts every word table in the language of `to` (en: the English templates of the same round). English when to
 *  is empty. The tables change in place, so a screen that renders again shows the new words. */
export function applyTexts(code: string, en: Texts, to: Texts): void {
  const source = code === SOURCE.code || Object.keys(to).length === 0;
  for (const [id, value] of english) {
    const [name, key] = [id.slice(0, id.indexOf(".")), id.slice(id.indexOf(".") + 1)];
    const t = TABLES[name];
    if (typeof value === "string") t[key] = source ? value : say(id, value, en, to);
    else if (Array.isArray(value)) t[key] = source ? value : value.map((v) => (typeof v === "string" ? say(id, v, en, to) : v));
    else if (typeof value === "function") {
      const f = value as (...a: unknown[]) => unknown;
      t[key] = source ? f : (...a: unknown[]) => { const out = f(...a); return typeof out === "string" ? say(id, out, en, to) : out; };
    }
  }
  now = source ? SOURCE.code : code;
}

/** The language to start in: the saved choice, else the first browser language the app has (the exact code, then
 *  the same language: es-MX -> es-419, pt-PT -> pt-BR, zh-CN -> zh-Hans), else English. */
export function startLanguage(saved: string | null, browser: readonly string[], have: readonly Language[]): string {
  const codes = have.map((l) => l.code);
  if (saved && codes.includes(saved)) return saved;
  for (const b of browser) {
    const exact = codes.find((c) => c.toLowerCase() === b.toLowerCase());
    if (exact) return exact;
    const base = b.toLowerCase().split("-")[0];
    if (base === "zh" && /-(tw|hk|mo|hant)/i.test(b)) continue; // traditional script: not zh-Hans
    const same = codes.find((c) => c.toLowerCase().split("-")[0] === base);
    if (same) return same;
  }
  return SOURCE.code;
}

/** The languages of an index.json (the source first); English only when it cannot be read. */
export function languagesOf(index: unknown): Language[] {
  const list = (index as { languages?: unknown })?.languages;
  if (!Array.isArray(list)) return [SOURCE];
  const ok = list.filter((l): l is Language => !!l && typeof l.code === "string" && typeof l.native === "string")
    .map((l) => ({ code: l.code, name: String(l.name ?? l.code), native: l.native, dir: l.dir === "rtl" ? "rtl" as const : "ltr" as const }));
  return ok.some((l) => l.code === SOURCE.code) ? ok : [SOURCE, ...ok];
}

/** X49x (T9: "español" is cramped): the short name for the language button: the name in its own language up to
 *  the region in brackets ("español (Latinoamérica)" -> "español", "中文（简体）" -> "中文"). The sheet shows the full name. */
export function shortName(native: string): string {
  const cut = native.search(/\s*[(（]/);
  return cut > 0 ? native.slice(0, cut) : native;
}

/** The id -> text map of a published language file ({lang, round, messages}). */
export function textsOf(file: unknown): Texts {
  const m = (file as { messages?: unknown })?.messages;
  if (!m || typeof m !== "object") return {};
  const out: Texts = {};
  for (const [k, v] of Object.entries(m as Record<string, unknown>)) if (typeof v === "string") out[k] = v;
  return out;
}

const onWeb = () => typeof window !== "undefined" && typeof document !== "undefined";

export function savedLanguage(): string | null {
  try { return onWeb() ? window.localStorage.getItem(SAVED_KEY) : null; } catch { return null; }
}
export function saveLanguage(code: string): void {
  try { if (onWeb()) window.localStorage.setItem(SAVED_KEY, code); } catch { /* private window: the choice lasts for this visit */ }
}

/** The page reads in the direction of the language (Arabic: right to left; the layout uses start and end). */
export function setPageLanguage(l: Language): void {
  if (!onWeb()) return;
  document.documentElement.lang = l.code;
  document.documentElement.dir = l.dir;
}

/** X49x review: a translation file that does not come in this time is an error (the app stays as it is). */
export const LANGUAGE_FETCH_MS = 8000;

async function getJson(path: string): Promise<unknown> {
  const stop = new AbortController();
  const late = setTimeout(() => stop.abort(), LANGUAGE_FETCH_MS);
  const res = await fetch(path, { credentials: "same-origin", cache: "no-cache", signal: stop.signal }).finally(() => clearTimeout(late));
  if (!res.ok) throw new Error(path + " " + res.status);
  return res.json();
}

const files = new Map<string, Promise<Texts>>();
function load(code: string): Promise<Texts> {
  let p = files.get(code);
  if (!p) {
    p = getJson(I18N_PATH + encodeURIComponent(code) + ".json").then(textsOf);
    p.catch(() => files.delete(code)); // a failure lets the next choice try again
    files.set(code, p);
  }
  return p;
}

/** The published languages (English only when the box has none or on a phone build). */
export async function loadLanguages(): Promise<Language[]> {
  if (!onWeb()) return [SOURCE];
  try { return languagesOf(await getJson(I18N_PATH + "index.json")); } catch { return [SOURCE]; }
}

let ticket = 0;

/** Loads a language and puts the tables in it; English when its file cannot be read or has no texts. Returns the
 *  language now, or null when a later call started before this one ended (X49x review: the last choice wins; a
 *  slow first load does not undo the user's pick). */
export async function switchLanguage(l: Language): Promise<Language | null> {
  const mine = ++ticket;
  let en: Texts = {}, to: Texts = {};
  if (l.code !== SOURCE.code) {
    try { [en, to] = await Promise.all([load(SOURCE.code), load(l.code)]); } catch { en = {}; to = {}; }
  }
  if (mine !== ticket) return null;
  applyTexts(l.code, en, to);
  const shown = languageNow() === l.code ? l : SOURCE; // X49x review: a file with no texts is English, so is the page
  setPageLanguage(shown);
  return shown;
}
