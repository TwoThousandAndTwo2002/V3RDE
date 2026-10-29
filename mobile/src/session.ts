// The signed-in session that the screens share, written new for V3RDE (NEW_WORK_UI.md, UI 11).
// The app shell gives it; a screen reads it with useSession().
import { createContext, useContext } from "react"; // React context: the session for every screen
import type { User as Person } from "./api";
import type { RoleName } from "./words";

export type Session = {
  who: Person;
  /** Shows another role's screen. */
  goTo: (role: RoleName) => void;
  /** Shows a short message at the bottom of the screen for `ms` (2600 ms when not given). */
  say: (text: string, ms?: number) => void;
  /** The services answered 401: the session has ended; the app shows sign-in. */
  ended: () => void;
};

const NOBODY: Session = { who: { id: "", email: "" }, goTo: () => undefined, say: () => undefined, ended: () => undefined };
export const SessionContext = createContext<Session>(NOBODY);
export const useSession = (): Session => useContext(SessionContext);
