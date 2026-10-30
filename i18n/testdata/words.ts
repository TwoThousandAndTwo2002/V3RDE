export const WORDS = {
  title: "Your listing",
  hint: "you@email.com",
  heard: (t: string) => "You said: “" + t + "”",
  rename: (n: number, from: string, to: string) => "Item " + n + ": " + from + " to " + to,
  nested: { deep: "Deep text" },
} as const;
const FIXED: Record<number, string> = {
  401: "Please log in again",
};
export function servings(n: number): string {
  return n === 1 ? "1 serving" : n + " servings";
}
export function grab(l: { title: string }, email: string): string {
  return `Hi! I grabbed "${l.title}" for ${servings(2)}. I am ${email}.`;
}
export function title(n: number) { return plural(n, "item found", "items found"); }
export function code(s: string) {
  if (s === "busy") return "The scanner is busy";
  return fetch("/api/listings", { method: "POST", headers: { "Content-Type": "application/json" } });
}
export function Pill({ label = "Take photo" }: { label?: string }) {
  return <View testID="pill" style={{ alignItems: "center", color: "#FFFFFF" }} aria-label={label} />;
}
const clock = (h: number, m: number) => `${h}:${two(m)}:${two(h)}`;
