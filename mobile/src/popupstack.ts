// X65 review: the open popups, newest last. When two popups are open at once (a wide screen: a dialog of the role panel,
// then the language sheet from the top bar), only the newest one takes Esc and keeps the Tab focus; the one under it
// gets them back when the newest closes (WAI-ARIA APG, modal dialog: a dialog opened from a dialog is on top of it).
const open: object[] = [];

/** A new popup is on top; the returned function takes it off (in any order). */
export function pushPopup(token: object): () => void {
  open.push(token);
  return () => {
    const i = open.lastIndexOf(token);
    if (i >= 0) open.splice(i, 1);
  };
}

/** True when token is the newest open popup. */
export const isTopPopup = (token: object): boolean => open.length > 0 && open[open.length - 1] === token;
