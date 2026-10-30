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

/** The Give screen: the listings of the giver. */
export const GIVE = {
  title: "Your listings",
  loading: "Loading…",
  empty: "Nothing is listed yet. Take a photo of a meal to share it.",
  add: "+ New listing",
} as const;

/** The toast after a post: one listing or several. */
export function postedText(n: number): string {
  return n > 1 ? n + " listings are posted. They stay up for 30 minutes." : "Listed. It stays up for 30 minutes.";
}

/** The camera and the sheets after the shutter (the approved X25 and X32 words, made simpler). */
export const CAPTURE = {
  hint: "Point at a barcode, or take a photo of the food",
  dropHint: "or drop an image here",
  dropNow: "Drop to use this image",
  close: "Close the camera",
  upload: "Upload a photo",
  shoot: "Take photo",
  cancel: "Cancel",
  allow: "Allow camera access to take a photo.",
  allowLink: "Allow camera",
  blocked: "Camera access is off. Upload a photo.",
  shotFailed: "The photo failed. Try again.",
  use: "Use this photo?",
  again: "Try again?",
  retake: "Retake",
  usePhoto: "Use photo",
  tryAgain: "Try again",
  barcode: (code: string) => "Barcode " + code,
  analyzing: "Analyzing…",
  notFood: "Try a food item",
  notFoodLine: "This does not look like food. Take a photo of the food item.",
  noNameOne: "The scanner cannot name this item",
  noNameOneLine: "Take a closer photo of the food item.",
  noNameMany: "The scanner cannot name these items",
  noNameManyLine: "Take a closer photo of fewer items.",
  listed: "Already listed",
  listedLine: "This item is already up in Your listings.",
  seeMine: "See my listings",
  paused: "Scanning is paused",
  pausedLine: "The scanner reached the limit for today. A barcode still works.",
  closeFlow: "Close",
} as const;

/** The review of the found items (the approved X32 words, made simpler). */
export const REVIEW = {
  one: "Your listing",
  check: "Check each item. Remove what you do not want to share.",
  title: "Title",
  servings: "Servings",
  fewer: "Fewer servings",
  more: "More servings",
  pickup: "Pickup details",
  pickupAll: "Pickup details (for all items)",
  pickupHint: "Where to collect, for example Lobby of 12 Main St",
  startOver: "Start over",
  post: "Post listing",
  posting: "Posting…",
  notPosted: "This item did not post. It stays here.",
  nonePosted: "No item posted. Try again.",
} as const;

/** The photo touch-up on the review (the original stays the default). */
export const TOUCH = {
  start: "Touch up photo", // X48r: one touch-up (the auto one)
  working: "Touching up the photo…",
  original: "Original",
  edit: "Touched up",
  choose: "Choose the photo to post:",
  kept: "The photo stays as it is: the check saw a change to the food.",
  failed: "The touch-up did not work. The photo stays as it is.",
  busy: "The touch-up is busy. Try again in a minute.",
  paused: "Touch-up is paused for today.",
  wait: "Wait for the touch-up to end, then post.",
  ofItem: (n: number) => "of item " + n,
} as const;

/** The chat bar on the review (X48s: talk or type; tap to talk, tap to stop). */
export const VOICE = {
  start: "Start talking",
  stop: "Stop talking",
  micOff: "Allow the microphone to talk.",
  failed: "Could not read what you said. Try again.",
  noSpeech: "No speech was heard. Tap the microphone and try again, or type the change.",
  busy: "Voice is busy. Try again in a minute.",
  paused: "Voice is paused for today.",
  rename: (n: number, from: string, to: string) => "Item " + n + ": \u201c" + from + "\u201d to \u201c" + to + "\u201d",
  servings: (n: number, from: string, to: number) => "Item " + n + ": " + from + " to " + to + " servings",
  remove: (n: number, title: string) => "Remove item " + n + ": " + title,
  pickup: (text: string) => "Pickup: " + text,
  question: (text: string) => "Question: " + text,
  notAdded: (title: string, n: number) => "Not added (a listing needs a photo): " + title + ", " + n + " servings",
  removedBefore: "Not used: a change for an item that you removed.",
  dropped: (text: string) => "Not used: " + text,
  unnamed: (n: number) => "Item " + n,
  first: "Stop the microphone or wait for the change, then post.",
  notAll: "remove every item (a listing needs at least one item; use Start over)",
  // X48s: the chat bar
  hint: "Talk or type a change",
  typeName: "Type a change to the list",
  send: "Send",
  examples: "Try: \u201c6 servings\u201d, \u201cremove item 2\u201d.",
  said: "You said",
  typed: "You typed",
  made: (n: number) => (n === 1 ? "1 change made" : n + " changes made"),
  undo: "Undo",
  undone: "Undone. The list is back as it was.",
  listeningFor: (s: number) => "Listening\u2026 " + Math.floor(s / 60) + ":" + String(s % 60).padStart(2, "0"),
  tapStop: "Tap to stop. It stops by itself after 1 minute.",
  reading: "Reading the change\u2026",
  markTitle: "title",
  markServings: (from: string, to: string) => from + " to " + to + " servings",
  mark: (parts: string[]) => "Changed in the chat: " + parts.join(", "),
  markPickup: "Changed in the chat",
  none: "No change found in what you said.",
} as const;
