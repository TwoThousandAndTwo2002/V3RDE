// X49w (T9 2026-10-31, tester: "the box that user is attempting to answer in is then obscured by the keyboard"):
// on a phone browser the on-screen keyboard covers the bottom of the page, but the page size does not change, so a
// bottom sheet and the sign-in button stay under it. The visual viewport is the part of the page the user sees
// (https://developer.mozilla.org/en-US/docs/Web/API/VisualViewport): the keyboard height is the layout viewport height
// less the visual viewport height and its top offset, while a field is in use. The sheets move up by it (react-native-web already gives the visible
// height as the window height, so the sheet's height limit needs no change), and a field that gets the focus is
// scrolled into the part that shows. When the keyboard closes, the height is 0 and the screen is as before.
import { useEffect, useState } from "react";

/** The web build: a page with a document (a phone build has none). No react-native import: the tests load this file. */
const onWeb = () => typeof window !== "undefined" && typeof document !== "undefined";

/** Under this many px the difference is the browser's own bars, not a keyboard. */
export const KEYBOARD_MIN_PX = 80;
/** The keyboard opens in about this time; then the focused field is scrolled into view. */
export const KEYBOARD_SETTLE_MS = 350;

/** The height the keyboard covers (px): the layout viewport height less the bottom of the visual viewport; 0 = no
 *  keyboard. X49w review: the keyboard is open only while the user types in a field, so a pinch zoom with no field in
 *  use is not a keyboard. While a field is in use the zoom is kept in the count: iOS Safari zooms in by itself on a
 *  field with text under 16 px (https://weblog.west-wind.com/posts/2023/Apr/17/Preventing-iOS-Textbox-Auto-Zooming-and-ViewPort-Sizing),
 *  and then the sheet must still sit on the bottom of the part that shows. */
export function keyboardInset(layoutHeight: number, viewportHeight: number, viewportTop: number, typing = true): number {
  if (!typing || !(layoutHeight > 0) || !(viewportHeight > 0)) return 0;
  const h = Math.round(layoutHeight - viewportHeight - Math.max(0, viewportTop));
  return h >= KEYBOARD_MIN_PX ? h : 0;
}

/** The element takes typed text (the phone keyboard opens for it). */
export function takesText(el: unknown): boolean {
  const e = el as { tagName?: string; type?: string; isContentEditable?: boolean } | null;
  if (!e) return false;
  if (e.isContentEditable) return true;
  const tag = (e.tagName ?? "").toUpperCase();
  if (tag === "TEXTAREA") return true;
  if (tag !== "INPUT") return false;
  return !["button", "checkbox", "radio", "range", "color", "file", "image", "reset", "submit", "hidden"].includes((e.type ?? "text").toLowerCase());
}

/** The layout viewport height (the root element's clientHeight: https://drafts.csswg.org/cssom-view/#dom-element-clientheight);
 *  a zoom and the keyboard do not change it. */
const layoutHeight = () => document.documentElement?.clientHeight || window.innerHeight;

/** The keyboard height now, from the page. */
function readInset(vv: VV): number {
  return keyboardInset(layoutHeight(), vv.height, vv.offsetTop, takesText(document.activeElement));
}

type VV = { height: number; offsetTop: number; addEventListener: (t: string, f: () => void) => void; removeEventListener: (t: string, f: () => void) => void };

/** The keyboard height on the web (0 on a phone build: KeyboardAvoidingView does that work there). */
export function useKeyboardInset(): number {
  const [inset, setInset] = useState(0);
  useEffect(() => {
    if (!onWeb()) return;
    const vv = (window as unknown as { visualViewport?: VV }).visualViewport;
    if (!vv) return;
    const read = () => setInset(readInset(vv));
    read();
    vv.addEventListener("resize", read);
    vv.addEventListener("scroll", read);
    document.addEventListener("focusin", read);
    document.addEventListener("focusout", read);
    return () => {
      vv.removeEventListener("resize", read); vv.removeEventListener("scroll", read);
      document.removeEventListener("focusin", read); document.removeEventListener("focusout", read);
    };
  }, []);
  return inset;
}

/** The field must move: a keyboard is open and the field is not all inside the part that shows (X49w review: no
 *  scroll on a desktop, and none that undoes another scroll, such as the address block that moves to the top). */
export function mustReveal(inset: number, top: number, bottom: number, viewportHeight: number): boolean {
  return inset > 0 && (top < 0 || bottom > viewportHeight);
}

/** Web: after the keyboard has opened, the field that has the focus comes into view (its scroll box moves). */
export function revealOnFocus(target: unknown): void {
  if (!onWeb()) return;
  const el = target as { scrollIntoView?: (o: ScrollIntoViewOptions) => void; isConnected?: boolean;
    getBoundingClientRect?: () => { top: number; bottom: number } } | null;
  if (!el?.scrollIntoView || !el.getBoundingClientRect) return;
  window.setTimeout(() => {
    if (el.isConnected === false || (document.activeElement as unknown) !== el) return;
    const vv = (window as unknown as { visualViewport?: VV }).visualViewport;
    if (!vv) return;
    const r = el.getBoundingClientRect!();
    const inset = readInset(vv);
    if (mustReveal(inset, r.top - vv.offsetTop, r.bottom - vv.offsetTop, vv.height)) el.scrollIntoView!({ block: "center", behavior: "smooth" });
  }, KEYBOARD_SETTLE_MS);
}
