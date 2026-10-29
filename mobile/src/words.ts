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
