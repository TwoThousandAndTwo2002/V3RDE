// Rules of the photo touch-up on the review, written new for V3RDE (NEW_WORK_UI.md, UI 16). The touch-up service
// (branch services) edits an item photo with Muse Image and checks with Muse Spark that the food did not change;
// the original is always kept and is the default. Pure functions, so the tests run them directly.

/** The app asks for the state of the job this often. */
export const POLL_MS = 2000;
/** It stops asking after this time (the job is then shown as failed). */
export const GIVE_UP_MS = 120_000;

export type TouchState =
  | { kind: "idle" } | { kind: "working"; id: string; since: number }
  | { kind: "offer"; id: string; photoId: string; chosen: "original" | "edit" }
  | { kind: "kept" } | { kind: "failed" } | { kind: "error"; text: string };

/** The next state after a GET of the job (or after the time is over). */
export function afterPoll(job: { status: string; photo_id?: string }, id: string, since: number, now: number): TouchState {
  if (job.status === "done" && job.photo_id) return { kind: "offer", id, photoId: job.photo_id, chosen: "original" };
  if (job.status === "kept_original") return { kind: "kept" };
  if (job.status === "running" && now - since < GIVE_UP_MS) return { kind: "working", id, since };
  return { kind: "failed" };
}

/** The photo_id to post: the edit only when the user chose it. */
export function photoToPost(original: string, s: TouchState): string {
  return s.kind === "offer" && s.chosen === "edit" ? s.photoId : original;
}

/** The words of an error answer of the touch-up service. */
export function touchError(status: number, code: string, words: { busy: string; paused: string; failed: string }): string {
  if (code === "daily_cap_reached") return words.paused;
  if (status === 429 || code === "busy" || code === "rate_limited") return words.busy;
  return words.failed;
}
