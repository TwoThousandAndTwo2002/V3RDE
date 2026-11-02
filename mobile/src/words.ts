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
  close: "Close",
  collected: "Collected",
  expired: "Expired",
  timeLeft: "Time left",
} as const;

/** X49x: the counts and units (whole messages, one text for each case, so each language can say them its way). */
export const UNITS = {
  servings: (n: number) => (n === 1 ? "1 serving" : n + " servings"),
  miles: (d: string) => d + " mi",
  near: "0.1 mi or less",
  seconds: (t: string) => t + " s",
} as const;

/** "1 serving", "6 servings". */
export function servings(n: number): string {
  return UNITS.servings(n);
}

/** X49j: where the food is, for the user who grabbed it: the chosen address, then the instructions. */
export function pickupLine(l: { pickup_text: string; pickup_address?: { text: string } }): string {
  return [l.pickup_address?.text ?? "", l.pickup_text.trim()].filter((x) => x).join(". ");
}

/** The Give screen: the listings of the giver. */
export const GIVE = {
  title: "Your listings",
  loading: "Loading…",
  empty: "Nothing is listed yet. Take a photo of a meal to share it.",
  add: "+ New listing",
  // X48t: delete a listing before a grab
  remove: "Delete",
  removeName: (title: string) => "Delete " + title,
  confirm: (title: string) => "Delete \u201c" + title + "\u201d?",
  confirmBody: "People will no longer see it. This cannot be undone.",
  keep: "Keep",
  removed: "Deleted. People no longer see it.",
  grabbed: "Someone is picking this up. It cannot be deleted.",
  removeFailed: "Could not delete it. Try again.",
  // X60 (T9): change a posted listing nobody has grabbed: the name, the servings, and the timer (30 minutes from now)
  editName: (title: string) => "Change " + title,
  editTitle: "Change the listing",
  againTitle: "Post again",
  editBody: "People see the change at once. The Facebook post changes too.",
  againBody: "It shows again for 30 minutes. The Facebook post changes too.",
  timerReset: "Reset the timer to 30 minutes",
  save: "Save",
  postAgain: "Post again",
  saved: "Saved. People see the change.",
  postedAgain: "Posted again for 30 minutes.",
  saveFailed: "Could not save it. Try again.",
  changeGrabbed: "Someone is picking this up. It cannot be changed.",
  changeEnded: "It ended. Tap the pencil to post it again.",
} as const;

/** R5: the rescue of an expired listing (T9 2026-10-31: take it to one of 3 places; the email only for bulk). */
export const RESCUE = {
  pill: "Rescue", // T9 2026-10-31: one word, a red pill on the expired card
  pillName: (title: string) => "Rescue " + title + ": find a place for it",
  find: "Find a place for it", // the popup title until places come
  title: "Take it to a food program",
  titleBulk: "Ask a food program first",
  looking: "Looking for places near the pickup address…",
  dropoff: "Nobody picked this up in time. These places are near the pickup address. Call before you go.",
  bulk: "This is a large amount. Send a message to a place first, then take the food there.",
  directions: "Directions",
  call: "Call",
  email: "Email",
  linkName: (what: string, place: string) => what + ", " + place,
  none: "No place was found near the pickup address.",
  failed: "Could not find places. Try again.",
  retry: "Try again",
  // X49q: no ZIP code; a listing with no pickup place uses the giver's location
  whereAsk: "This listing has no pickup place. Search near your location now?",
  useLocation: "Use my location",
  locationOff: "Location is off. Turn it on for this site in your settings, then try again.",
  close: "Close",
} as const;

/** X49x: the counts of the review list and of a post (approved mockup words; one whole message for each case). */
export const COUNT = {
  found: (n: number) => (n === 1 ? "1 item found" : n + " items found"),
  post: (n: number) => (n === 1 ? "Post 1 listing" : "Post " + n + " listings"),
  posting: (i: number, n: number) => "Posting " + i + " of " + n + "\u2026",
  notPosted: (n: number) => (n === 1 ? "1 item not posted" : n + " items not posted"),
  postedSome: (posted: number, left: number) => posted === 1
    ? (left === 1 ? "1 listing posted. 1 item is kept here; try again." : "1 listing posted. " + left + " items are kept here; try again.")
    : (left === 1 ? posted + " listings posted. 1 item is kept here; try again." : posted + " listings posted. " + left + " items are kept here; try again."),
  posted: (n: number) => (n > 1 ? n + " listings are posted. They stay up for 30 minutes." : "Listed. It stays up for 30 minutes."),
} as const;

/** The toast after a post: one listing or several. */
export function postedText(n: number): string {
  return COUNT.posted(n);
}

/** X49x: the retry line under "Try again?" (logic.ts retryText), each one a whole sentence; the photo is kept. */
export const SCAN = {
  busy: "The scanner is busy. Wait a few seconds \u2014 your photo is kept.",
  tooMany: "Too many photos this minute. Wait a minute \u2014 your photo is kept.",
  gate: "The food check is not available right now \u2014 your photo is kept.",
  tooLong: "This photo took too long. Try a closer photo of fewer items \u2014 your photo is kept.",
  unreadable: "The scanner could not read this photo. Try again, or retake it closer \u2014 your photo is kept.",
  offline: "No connection. Check your internet, then try again \u2014 your photo is kept.",
  noAnswer: "No answer in time. Check your internet, then try again \u2014 your photo is kept.",
  stopped: "The scan stopped. Try again \u2014 your photo is kept.",
  ourSide: "Something went wrong on our side. Try again \u2014 your photo is kept.",
  said: (said: string) => said + " \u2014 your photo is kept.",
  failed: (status: number) => "Request failed (" + status + ") \u2014 your photo is kept.",
  noItems: "The scanner found no items in this photo. Retake it closer to the food.",
} as const;

/** X49x: the words of a request that did not get an answer (api.ts, logic.ts errorMessage). */
export const NET = {
  timedOut: "Timed out, try again",
  offline: "Network error, check the connection",
  stopped: "Stopped",
  tryAgain: "Try again",
  scannerBusy: "The food scanner is busy, try again",
  serviceBusy: "The service is busy, try again",
  failed: (status: number) => "Request failed (" + status + ")",
  photo: "The photo did not load",
} as const;

/** X49x: the names of the parts of a review card (ui.tsx ReviewCard), for screen readers. */
export const CARD = {
  title: (n: number) => "Title of item " + n,
  remove: (n: number) => "Remove item " + n,
  fewer: (n: number) => "Fewer servings of item " + n,
  servings: (n: number) => "Servings of item " + n,
  more: (n: number) => "More servings of item " + n,
} as const;

/** X49x: the microphone and recording errors (voicerec.ts, webvoice.ts, clipsession.ts). */
export const MIC = {
  allow: "Allow the microphone to talk to the app",
  failed: "The microphone did not start",
  tapAgain: "The microphone did not start. Tap the microphone again.",
  https: "This browser cannot record here (https is needed)",
  busy: "A recording is already running",
  cancelled: "The recording was cancelled",
  notRecording: "Not recording",
} as const;

/** X49x: the line under the title of the one-item form: where the title came from (capture.ts sourceNote). */
export const SOURCE_NOTE = {
  barcode: (code: string) => "From the barcode " + code,
  scanner: (description: string) => "The scanner says: " + description,
} as const;

/** X49x: the language choice (sign-in row, role sheet). The names of the languages come from /i18n/index.json. */
export const LANG = {
  title: "Language",
  name: (language: string) => "Language: " + language,
  close: "Close",
} as const;

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
  pickup: "Pickup address and instructions", // R5 (T9 2026-10-31)
  pickupAll: "Pickup address and instructions (for all items)",
  pickupHint: "5 Broad St, Lagos. Front desk", // X49q: an example with no ZIP code
  startOver: "Start over",
  post: "Post listing",
  posting: "Posting…",
  notPosted: "This item did not post. It stays here.",
  nonePosted: "No item posted. Try again.",
} as const;

/** X49e: the place and the distance (Receive and the review). T9 2026-10-30, the approved "distance" mockup v2. */
export const PLACE = {
  askTitle: "Find food near you",
  askLine: "Your place shows only the food within the distance you pick. Nobody else sees your place.",
  useLocation: "Use my location",
  // X49q: no ZIP code; location off = all food, with no distance
  notNow: "Not now",
  offTitle: "Location is off",
  offLine: "Turn on location for this site in your settings, then try again. Or see all food, with no distance.",
  tryAgain: "Try again",
  noDistance: "Location is off. No distance.",
  offPost: "Location is off. Turn it on for this site in your settings, then post again.",
  locating: "Finding your location…",
  nearLocation: "Near your location",
  change: "Change",
  changeName: "Change your place",
  close: "Close",
  yourPickup: "Your pickup",
  moreNear: "More near you",
  release: "Release it",
  keep: "Keep it",
  releaseYes: "Release",
  releaseLine: "Other people near it can grab it again, while its timer runs.",
  released: "Released. Other people can grab it now.",
  releaseEnded: "The timer ended. The grab is final.",
  cancel: "Cancel",
  // the review (the giver)
  reach: (d: number) => d + " mi",
  reachName: (d: number) => "Within " + d + " miles",
  farther: (d: number) => "Farther than " + d + " mi",
  emptyNear: (d: number) => "Nothing within " + d + " mi now. Look again in a few minutes.",
  releaseName: (title: string) => "Release " + title,
  releaseTitle: (title: string) => "Release " + title + "?",
  farTitle: (miles: string) => "This food is " + miles + " away",
  farLine: (reach: number, minutes: number) =>
    "It is farther than your " + reach + " mi. The timer ends in " + minutes + " min. Grab it only if you can get there in time.",
} as const;

/** X49i: the More button of a review item (Touch up and the details), and the chat column. */
export const MORE = {
  title: "More",
  close: "Close",
  chat: "Talk or type a change",
  name: (n: number) => "More for item " + n,
} as const;

/** X49j: the pickup address on the review (Places autocomplete, then the map). */
export const PICKUP = {
  address: "Pickup address",
  hint: "Type the street address",
  choose: "Tap the address to choose it. Then the map shows the pickup place.",
  changeName: (address: string) => "Change the address, " + address, // X49q: the address box is the button
  editHint: "Type a new address, or move the map. Tap outside to close.",
  naming: "Finding the address…", // X49q review: Post waits for it
  none: "No address found. Check the street and the town.",
  failed: "The address search did not answer. Try again.",
  need: "Choose the pickup address from the list.",
  here: "Use my current location", // X49q
  finding: "Finding your location…",
  hereOff: "Location is off. Turn it on for this site in your settings, or type the address.",
  moveHint: "Move the map to put the pin on the pickup spot. The address follows the pin.",
  safety: "Receivers see only how far it is. They see the address and the instructions after they grab.",
  notes: "Instructions at the door",
  notesHint: "For example: front desk, ask for Sam",
  mapName: (address: string) => "Map of " + address,
} as const;

/** The photo touch-up on the review (the original stays the default). */
export const TOUCH = {
  start: "Touch up photo", // X48r: one touch-up (the auto one)
  short: "Touch up", // X49: the word on the Touch up pill (the name stays `start`)
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

/** X49: the details of an item on the review (what the scan found; the cost is never shown). */
export const DETAILS = {
  pill: "Details",
  none: "No details now. Tap Change to add them.", // X49 review: the user cleared them all
  change: "Change",
  changeName: "Change the details",
  cancel: "Cancel",
  done: "Done",
  ready: "Ready to eat",
  cooking: "Needs cooking",
  sealedLine: "A sealed package",
  veg: "vegetarian",
  meat: "has meat or fish",
  bulk: "in bulk",
  single: "in single portions",
  about: (kg: string) => "about " + kg + " kg",
  condition: "Condition",
  food: "Food",
  portions: "Portions",
  sealed: "Sealed",
  vegChoice: "Vegetarian",
  meatChoice: "Meat or fish",
  bulkChoice: "Bulk",
  singleChoice: "Single",
  weight: "Weight (kg)",
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
  // X49q: the hints that take turns in the empty chat box (short: the desktop box is narrow)
  hints: ["Talk or type a change", "Try \u201c6 servings\u201d", "Try \u201cremove item 2\u201d"],
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

/** X49x: every word table of this file, by name (src/lang.ts puts them in the chosen language). */
export const WORD_TABLES = { ROLE, FRAME, RECEIVE, GIVE, RESCUE, CAPTURE, REVIEW, PLACE, MORE, PICKUP, TOUCH, DETAILS, VOICE,
  UNITS, COUNT, SCAN, NET, CARD, MIC, SOURCE_NOTE, LANG } as unknown as Record<string, Record<string, unknown>>;
