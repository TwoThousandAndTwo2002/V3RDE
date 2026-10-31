// X49 (T9, 2026-10-30): the details of an item on the review (what the scan found), written new for V3RDE
// (mobile/docs/style_guide.md "Review"). Pure functions (no React, no network), so the tests run them directly.
// The words come from src/words.ts (DETAILS), as a parameter. The cost (meal_cost) is never shown.
import type { ItemDetails } from "./logic";

/** The words this module needs (DETAILS of src/words.ts). */
export type DetailWords = {
  ready: string; cooking: string; sealedLine: string; veg: string; meat: string; bulk: string; single: string;
  about: (kg: string) => string;
};

/** A weight in kg for a person: at most 2 decimals, no zeros at the end ("1.2", "0.35", "3"). */
export function kgText(w: number): string {
  return String(Math.round(w * 100) / 100);
}

/**
 * The lines of the details: one short sentence (the condition first, then vegetarian or meat or fish, then bulk or
 * single portions, then "about N kg"; joined with ", ", with a capital first letter and a full stop), then the
 * description. A part that is missing is left out; no details give no lines.
 */
export function detailLines(d: ItemDetails | undefined, w: DetailWords): string[] {
  if (!d) return [];
  const cond = { ready_to_eat: w.ready, cooking_required: w.cooking, sealed: w.sealedLine } as const;
  const parts = [
    d.meal_condition ? cond[d.meal_condition] : "",
    d.is_non_veg === 1 ? w.meat : d.is_non_veg === 0 ? w.veg : "",
    d.serving_type === "bulk" ? w.bulk : d.serving_type === "individual" ? w.single : "",
    typeof d.weight === "number" && d.weight > 0 ? w.about(kgText(d.weight)) : "",
  ].filter(Boolean);
  const out: string[] = [];
  if (parts.length > 0) {
    const s = parts.join(", ") + ".";
    out.push(s.charAt(0).toUpperCase() + s.slice(1));
  }
  const desc = (d.description ?? "").trim();
  if (desc) out.push(desc);
  return out;
}

/** The time (ms) before the next letter shows, when `shown` letters of `text` show: a line end ("\n") waits
 *  `lineMs`, a letter `letterMs`; -1 when all of it shows. */
export function typeDelay(text: string, shown: number, letterMs: number, lineMs: number): number {
  if (shown >= text.length) return -1;
  return shown > 0 && text[shown - 1] === "\n" ? lineMs : letterMs;
}

/** The details while the user changes them (Change): the choices, and the weight as typed. */
export type DetailDraft = {
  condition?: NonNullable<ItemDetails["meal_condition"]>; diet?: 0 | 1; portions?: NonNullable<ItemDetails["serving_type"]>; weight: string;
};

export function draftOf(d: ItemDetails | undefined): DetailDraft {
  return { condition: d?.meal_condition, diet: d?.is_non_veg, portions: d?.serving_type,
    weight: typeof d?.weight === "number" ? kgText(d.weight) : "" };
}

/** The details after Done: the choices and the weight of the draft (an empty weight, or a choice cleared, is
 *  left out); the description and the other fields stay. Undefined when nothing is left. The weight must be
 *  checked first (capture.ts weightProblem). */
export function applyDraft(d: ItemDetails | undefined, x: DetailDraft): ItemDetails | undefined {
  const out: ItemDetails = { ...(d ?? {}) };
  delete out.meal_condition; delete out.is_non_veg; delete out.serving_type; delete out.weight;
  if (x.condition) out.meal_condition = x.condition;
  if (x.diet === 0 || x.diet === 1) out.is_non_veg = x.diet;
  if (x.portions) out.serving_type = x.portions;
  const kg = x.weight.trim() === "" ? NaN : Number(x.weight);
  // X49 review: the field shows kgText (2 decimals); the same text keeps the original number (no false edit)
  if (typeof d?.weight === "number" && x.weight.trim() === kgText(d.weight)) out.weight = d.weight;
  else if (Number.isFinite(kg)) out.weight = kg;
  return Object.keys(out).length > 0 ? out : undefined;
}

/** X49 review: the Details pill shows when the item has details now, or when the scanner gave it details (the user
 *  cleared them all and can add them back). */
export function detailsPillShows(now: ItemDetails | undefined, scanned: ItemDetails | undefined, w: DetailWords): boolean {
  return detailLines(now, w).length > 0 || detailLines(scanned, w).length > 0;
}
