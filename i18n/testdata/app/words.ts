export const ROLE = { give: "Give", receive: "Receive", logout: "Log out" } as const;
export const RECEIVE = { title: "Available now", collected: "Collected", expired: "Expired" } as const;
export function roleCard(role: string, chosen = false): { title: string; spoken: string } {
  const title = role === "give" ? ROLE.give : ROLE.receive;
  return { title, spoken: title + (chosen ? ". Selected" : "") };
}
export function timeText(l: { status: string }, words: { collected: string; expired: string }): string {
  return l.status === "collected" ? words.collected : words.expired;
}
export function postTitle(n: number): string { return "Post " + plural(n, "listing", "listings"); }
export function plural(n: number, one: string, many: string): string { return n === 1 ? `1 ${one}` : `${n} ${many}`; }
