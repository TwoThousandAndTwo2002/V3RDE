// The words of the app screens after sign-in, written new for V3RDE (NEW_WORK_UI.md, UI 10 and after).
// T9 (Oct 29): the earlier words, made simpler (ASD-STE100). Kept out of the screens so the tests can check them.

/** The role sheet. */
export const ROLE = {
  title: "What do you want to do?",
  give: "Give",
  giveLine: "Share your food",
  receive: "Receive",
  receiveLine: "Find food near you",
  logout: "Log out",
  close: "Close",
  selected: "Selected",
  change: "Change role",
} as const;

export type RoleName = "give" | "receive";

/** The name and the line of a role card, and what a screen reader reads for it. */
export function roleCard(role: RoleName, chosen = false): { title: string; line: string; spoken: string } {
  const title = role === "give" ? ROLE.give : ROLE.receive;
  const line = role === "give" ? ROLE.giveLine : ROLE.receiveLine;
  return { title, line, spoken: title + ". " + line + (chosen ? ". " + ROLE.selected : "") };
}

/** The frame of the signed-in screens: the ☰ header (phone, tablet) and the top bar (wide screen). */
export const FRAME = {
  brand: "V3RDE",
  menu: "Menu",
  tabs: "Roles",
  expired: "Your session has ended. Sign in again.",
} as const;

/** The Receive screen: the feed of listings that are up now, and the grab sheet. */
export const RECEIVE = {
  title: "Available now",
  loading: "Loading…",
  empty: "Nothing is offered now. Look again in a few minutes.",
  grab: "Grab",
  grabbing: "Grabbing…",
  gone: "This food is gone",
  grabbed: "You grabbed it",
  maps: "Maps",
  chat: "WhatsApp",
  close: "Close",
  yours: "Your listing",
  collected: "Collected",
  expired: "Expired",
  timeLeft: "Time left",
} as const;

/** "1 serving", "6 servings". */
export function servings(n: number): string {
  return n === 1 ? "1 serving" : n + " servings";
}

/** The WhatsApp message to the giver after a grab. */
export function grabMessage(l: { title: string; number_of_serving: number; pickup_text: string }, email: string): string {
  return `Hi! I grabbed "${l.title}" on V3RDE (${servings(l.number_of_serving)}). Pickup: ${l.pickup_text}. I am ${email}. When can I come?`;
}
