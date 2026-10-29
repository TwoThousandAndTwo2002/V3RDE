# V3RDE — the app UI, built during the Meta Global AI Developer Hackathon

This file is the record of the **app UI** work on branch `ui`. It follows the rules of `NEW_WORK.md` (branch `services` writes that file; this branch writes only this one, so the two branches never change the same file). When the UI is complete, branch `ui` merges into `main` and these entries join `NEW_WORK.md`.

- **Carried:** a file first written during the Hackathon in our earlier repository (Icarus) comes over only when `git blame -M -C -C -C` gives none of its lines to code from before the kickoff. Any such line is written again. Each carried file is checked against the sha256 the audit recorded.
- **Written again:** each UI part that existed before the kickoff (the screens, the shared parts, the theme, the app base) is new code here, made from the Hackathon specifications: the parity list, the new style guide and the mockups approved during the Hackathon.
- **Labelled as pre-existing:** open-source dependencies and brand assets, each named where it is used.

Each entry names the date, the Meta model it uses, its files and its sources (`docs/sources/U<n>.md`).

---

## UI 1. The voice and web-camera parts of the app, carried

**When:** Oct 28 2026 (carried). The files were first written during the Hackathon, Oct 27–28 2026, in entries 12, 20 and 21.
**Meta model:** Muse Voice Transcribe 1.0 (the voice parts make the audio clip in the format it takes).
**Files:** `mobile/src/webcam.ts`, `mobile/src/wav.ts`, `mobile/src/webvoice.ts`, `mobile/public/voice-capture-worklet.js`, `mobile/src/voicerec.ts`, `mobile/src/clipsession.ts`, `mobile/tests/wav.test.mts`, `mobile/tests/clipsession.test.mts`
**Sources:** `docs/sources/U1.md`

The audit gives none of the lines of these files to pre-kickoff code, except 4 import lines in the two tests. Those 4 lines are written again.

| File | What it does | First written |
|---|---|---|
| `mobile/src/webcam.ts` | the web camera at full size | entry 12, Oct 27 |
| `mobile/src/wav.ts` | the WAV clip: resample, 16-bit, size limit | entry 20, Oct 28 |
| `mobile/src/webvoice.ts` | records the clip in the browser | entry 20, Oct 28 |
| `mobile/public/voice-capture-worklet.js` | the browser audio worklet | entry 20, Oct 28 |
| `mobile/src/voicerec.ts` | records the clip on the phone and on the web | entry 20, Oct 28 (changed in 21) |
| `mobile/src/clipsession.ts` | one recording at a time; stop and cancel | entry 21, Oct 28 |
| `mobile/tests/wav.test.mts` | tests of `wav.ts` | entry 20, Oct 28 |
| `mobile/tests/clipsession.test.mts` | tests of `clipsession.ts` | entry 21, Oct 28 |

- Not yet type-checked: `voicerec.ts` and `webcam.ts` use `api.ts` and `logic.ts`, which come with the rebuild.
- Tests: mobile 11 (node --test).

---

## UI 2. The app base: Expo SDK 58, the V3RDE identity, the brand icons

**When:** Oct 28 2026.
**Meta model:** none.
**Files:** `mobile/package.json`, `mobile/package-lock.json`, `mobile/tsconfig.json`, `mobile/app.json`, `mobile/eas.json`, `mobile/.env`, `mobile/.gitignore`, `mobile/index.ts`, `mobile/App.tsx`, `mobile/third_party/EXPO_TEMPLATE_LICENSE.txt`, `mobile/assets/*.png` (6), `mobile/public/apple-touch-icon.png`
**Sources:** `docs/sources/U2.md`

The app base is written new. It replaces the base of the earlier app; it is not a copy.

- **Open-source template (pre-existing, labelled):** `package.json`, `tsconfig.json`, `index.ts`, `App.tsx` and `.gitignore` start from Expo's `blank-typescript` template for SDK 58 (`create-expo-app`, MIT licence, `mobile/third_party/EXPO_TEMPLATE_LICENSE.txt`). `App.tsx` is the template screen until the screens are written again.
- **Dependencies:** added with `npx expo install`, so each version is the one SDK 58 names: camera, image picker, image manipulator, audio, fonts (Poppins, SIL Open Font Licence) and the web parts.
- **The app is V3RDE:** name and web title `V3RDE`, slug and URL scheme `v3rde`, Android package and iOS bundle ID `com.kkombuchaas.v3rde`. A phone installs it as a new app. The permission texts name V3RDE.
- **Brand icons (pre-existing, labelled):** the 7 icon files are Hungree LLC brand assets from before the Hackathon. They are byte-equal to the earlier files (sha256 checked against the audit).
- **EAS:** the `preview` profile builds an APK. The EAS project for slug `v3rde` is made with `eas init` after this change.
- Not yet type-checked or exported for the web: `voicerec.ts` and `webcam.ts` need `api.ts` and `logic.ts`, which come next.
- Tests: mobile 11 (`npm test`).

---

## UI 3. The package ID is `com.kkombuchaas.V3RDE`

**When:** Oct 28 2026.
**Meta model:** none.
**Files:** `mobile/app.json`
**Sources:** `docs/sources/U3.md`

- T9: the app name is upper case in the package ID. The Android package and the iOS bundle ID are now `com.kkombuchaas.V3RDE`.
- The URL scheme stays `v3rde` (Android matches schemes case-sensitively and asks for lower case). The slug stays `v3rde`.
- No APK or iOS build was made with the earlier ID, so no installed app changes.

---

## UI 4. The API client and the app logic, and the EAS project

**When:** Oct 29 2026.
**Meta model:** none (the client calls the routes that use Muse Spark, SAM 3.1 and Muse Voice Transcribe on the services side).
**Files:** `mobile/src/api.ts`, `mobile/src/logic.ts`, `mobile/tests/logic.test.mts`, `mobile/package.json`, `mobile/app.json`
**Sources:** `docs/sources/U4.md`

- **`api.ts` is new code.** It calls the routes of branch `services`: auth, listings, the one analyze route (photo, barcode, capture data), the label report and voice posting. A phone fetches photos with the session cookie and shows them as data URIs.
- **`logic.ts`** holds two kinds of code:
  - **Carried:** the answer and label-report functions and the words of the review list (entries 10 and 15), and the upload size, the web-camera size and the turn of a still photo (entry 12). Each block is checked against the blame of the earlier repository: no line from before the kickoff.
  - **New:** `itemPhotoPath`, `targetSize`, `joinUrl` and `errorMessage`, written in place of the earlier helpers. A script compared them with the earlier helpers on 899 inputs; none differed.
- **Rewrite guard:** `api.ts`, `logic.ts` and the test have no line of the earlier files they replace (the script checks it).
- **EAS:** the project `@kkombuchaas-team/v3rde` is linked (`extra.eas.projectId`).
- From this change on, `tsc` checks the app code. `voicerec.ts` and `webcam.ts` (UI 1) now type-check.
- Tests: mobile 25 (11 + 14 new).

---

## UI 5. The design tokens, the shared UI parts and the style guide

**When:** Oct 29 2026.
**Meta model:** none (the parts show the results of the Muse Spark and SAM 3.1 routes; this change has no model call).
**Files:** `mobile/src/theme.ts`, `mobile/src/ui.tsx`, `mobile/tests/style.test.mts`, `mobile/docs/style_guide.md`, `mobile/tools/ui/measure_x32.py`, `mobile/tools/ui/x32_measure.json`, `mobile/tools/ui/parity_compare.py`, `mobile/tools/ui/.gitignore`, `mobile/App.tsx`, `mobile/package.json`, `mobile/package-lock.json`
**Sources:** `docs/sources/U5.md`

- **Written again:** the theme, the shared parts and the style test are new code. The rewrite guard finds no line of the pre-kickoff `theme.ts`, `ui.tsx`, `style.test.mts` or `App.tsx` of the earlier repository.
- **`theme.ts`:** every colour, size, radius, spacing, glyph path, opacity and motion value of the app. Each value has a source in `mobile/docs/style_guide.md`: a measurement of the mockups approved during the Hackathon (X32, Oct 27; X25 for the camera and the small sheets), a brand asset (Hungree red, Poppins; pre-existing, labelled), a platform rule, or a choice that T9 accepted.
- **`ui.tsx`:** the parts the screens use: text, glyphs, icon buttons, pills, rows, fields, the review card, the camera controls, the progress bar, the sheet and dialog (photo, shade, frost, spring), the sheet body and its pills, the camera dialog, the photo frame.
- **Style gate:** `tests/style.test.mts` fails a part or a screen with a colour, a font, a text size, a radius that is not a token, or a fractional number; it fails when the guide and `theme.ts` do not list the same tokens with the same values; it computes the text contrast in both modes.
- **Mockup:** the parts rendered 25 approved states again (Expo web export, Playwright, phone 390 × 844 at 3×, desktop 1440 × 900 at 2×, the same photos). Two independent checks measured them against the approved shots. After the fixes, the layout is within 0.33 pt in 24 states and within 1.0 pt in one; frost, shade and colours are within 2 levels. T9 approved the result on Oct 29.
- **New dependencies (pre-existing, labelled):** `expo-blur`, `expo-linear-gradient`, `react-native-svg` (the versions of Expo SDK 58).
- The app shell shows the name in the theme until the screens come.
- Tests: mobile 33 (25 + 8 new).

---

## UI 6. The field parts: focus border, Show/Hide, text link, busy and hover pill

**When:** Oct 29 2026.
**Meta model:** none (parts of the sign-in form; this change has no model call).
**Files:** `mobile/src/ui.tsx`, `mobile/src/theme.ts`, `mobile/tests/style.test.mts`, `mobile/docs/style_guide.md`
**Sources:** `docs/sources/U6.md`

- **`Field`:** a 2 pt border in `accent` while the field has the focus, and no browser focus ring on the web; a text button can sit inside the field (`right`); the label is also the name of the input for a screen reader; a `ref` reaches the input.
- **`FieldButton`** (new): the Show/Hide button inside a field, in the `link` colour, with a 44 pt touch area and its own name ("Show password").
- **`TextLink`** (new): an action in a line of text ("Create one"), in the `link` colour, underlined, read as a button.
- **`Pill`:** while busy, a tap does nothing and the pill keeps the keyboard focus (before, the focus went to the page); with the mouse on it (web), opacity 0.92.
- **Colours (T9, Oct 29):** new token `link` (light `#BC202C`, dark `#FF727D`); dark `danger` `#FF727D` and dark `placeholder` `#6D6D78` from the earlier sign-in screen; light `danger` stays `#C43140`. Every text colour reaches 4.5:1 on `card` and `bg` (the style test checks `link` too).
- **Reference:** the earlier sign-in screen (Icarus `457a62c`, from before the kickoff), rendered once by a harness as a reference to measure. No code of it is carried; the rewrite guard checks `ui.tsx`, `theme.ts` and the style test.
- **Mockup:** a mock form put the parts in the layout of the sign-in form (9 states, phone and desktop, 3 dark). An independent check found no difference other than T9's decisions. T9 approved the review on Oct 29.
- The parts are shared, so the U5 review list and the one-item form also get the focus border, the hover and the busy behaviour.
- Tests: mobile 33.

---

## UI 7. The food-doodle wallpaper, placed and animated by rule 30

**When:** Oct 29 2026.
**Meta model:** none (a decorative wallpaper; this change has no model call).
**Files:** `mobile/src/automata.ts`, `mobile/src/doodles.tsx`, `mobile/tests/automata.test.mts`, `mobile/src/theme.ts`, `mobile/docs/style_guide.md`, `mobile/tests/style.test.mts`, `mobile/package.json`
**Sources:** `docs/sources/U7.md`

- **New art:** 20 new line drawings of food. The earlier doodle art (from before the kickoff) is not carried; two independent checks found no copy or near copy of it, and the rewrite guard finds no line of the earlier doodle file.
- **Rule 30:** a drawing sits in every cell of a grid. The cellular automaton rule 30 (from a random first row, a fixed seed) makes each cell live or dead; a drawing in a dead cell has 55 % of the strength. Every 1000 ms a new generation comes in at the bottom and the pattern moves up one row; the drawings stay in their cells. With "reduce motion" on, the pattern stands still.
- **T9's values (Oct 29), chosen from an animated sample:** step 1000 ms, faint 55 %, wallpaper 9 % (dark 10 %), rows move up.
- **Look:** stroke 2.2 pt and opacity as on the earlier wallpaper; the ink per area is near it. A hash picks the drawing, its turn and its shift; no drawing is the same as the one to its left or above it.
- **Not used yet:** the sign-in screen (UI 8) is the first screen with the wallpaper.
- Tests: mobile 42 (33 + 9 new: fixed hash and rule-30 values, the step, fairness, neighbours, edge cases).

---

## UI 8. The sign-in screen

**When:** Oct 29 2026.
**Meta model:** none (sign-in; the auth service of branch `services` checks the email and the password).
**Files:** `mobile/src/screens/Login.tsx`, `mobile/src/signin.ts`, `mobile/tests/signin.test.mts`, `mobile/App.tsx`, `mobile/src/ui.tsx`, `mobile/src/theme.ts`, `mobile/docs/style_guide.md`, `mobile/package.json`
**Sources:** `docs/sources/U8.md`

- **Written again:** the sign-in screen is new code on the U6 field parts and the U7 doodle wallpaper. The rewrite guard finds no line of the earlier `Login.tsx`, `App.tsx`, `ui.tsx` or `theme.ts`.
- **Words (T9, Oct 29: the earlier words, made simpler):** "Share or find a meal", "Sign in or sign up", "8 characters or more", "Continue"; a 401 shows "No account found." with "Create one". The words and the rules are in `signin.ts`, with tests.
- **Behaviour:** one request at a time; Enter in the email field goes to the password field, Enter there signs in and keeps the focus; the email has no spaces at the ends and must have the form name@domain.end (the same check as the auth service); the password field has no auto-capital, auto-correct or spell check, and asks for a new password after "No account found"; a screen reader reads the busy pill as "Signing in…".
- **Wide screen:** a centred card on the doodles; after sign-in it fades and rises (300 ms, 40 pt), then the next screen comes.
- **App:** at start the app asks the services for a session; without one it shows the sign-in screen. After sign-in a short line (the email) stands in for the next screens.
- **Check:** one independent check (T9: at most one, at most 15 minutes) found no layout difference over 1.0 pt and 6 faults; all 6 are fixed and checked in the real app.
- Tests: mobile 46 (42 + 4 new).

---

## UI 9. Reset password by email link

**When:** Oct 29 2026.
**Meta model:** none (reset password; the auth service of branch `services` makes the link and sends the email).
**Files:** `mobile/src/screens/Reset.tsx`, `mobile/src/screens/AuthFrame.tsx`, `mobile/tests/reset.test.mts`, `mobile/src/screens/Login.tsx`, `mobile/src/signin.ts`, `mobile/src/api.ts`, `mobile/App.tsx`, `mobile/docs/style_guide.md`, `mobile/package.json`
**Sources:** `docs/sources/U9.md`

- **New:** "Forgot password?" on the sign-in screen opens "Reset your password" (email, "Send link"), then "Check your email". The same words for every email, so the screen tells nothing about which accounts exist.
- **The link:** web `/reset#token=…`, app `v3rde://reset#token=…`. The token is after "#", so the browser does not send it to the web server or in a Referer header; on the web it leaves the address bar at once. The link opens "Set a new password"; on success the user is signed in, with the same handoff as sign-in. A link that is unknown, used or too old shows "This link has expired or was used." with "Send a new link".
- **Routes (contract for branch services):** `POST /api/auth/reset {email}` → 202; `POST /api/auth/reset/confirm {token, password}` → 200 user and session, or 400 "reset link expired". The token is 32 random bytes, unpadded base64url (43 characters).
- **Frame:** the phone and wide-screen frame of sign-in moved to `AuthFrame.tsx`, shared by the reset screens; sign-in looks the same as in UI 8.
- **Behaviour:** one request at a time; the links do nothing while a request waits; the cursor starts in the new-password field; "Check your email" is read out by a screen reader; a start error does not leave a blank screen, and a link that comes during start wins.
- **Check:** one independent check (T9: at most one, at most 15 minutes) found 5 issues; all are fixed and checked in the real app.
- Tests: mobile 49 (46 + 3 new).

---

## UI 10. The role sheet

**When:** Oct 29 2026.
**Meta model:** none.
**Files:** `mobile/src/screens/Role.tsx`, `mobile/src/words.ts`, `mobile/tests/words.test.mts`, `mobile/App.tsx`, `mobile/src/ui.tsx`, `mobile/src/theme.ts`, `mobile/docs/style_guide.md`, `mobile/package.json`, `mobile/package-lock.json`
**Sources:** `docs/sources/U10.md`

- **Written again:** the role sheet is new code on the U5 parts. The rewrite guard finds no line of the earlier app's mobile code.
- **Words (T9, Oct 29: the earlier words, made simpler):** "What do you want to do?", Give "Share your food", Receive "Find food near you", "Log out". They are in `words.ts`, with tests.
- **Parts:** `Popup`, a sheet over the screen on view (a dim layer that fades in, a tap on it or Esc closes it when the sheet can close); the glyphs give (a camera), receive (a bowl) and menu (☰), drawn new.
- **When it shows:** right after sign-in on a phone (over the sign-in screen); right after sign-in on a wide screen (the sign-in card fades out, then the role dialog rises over the doodles, no dim); at start with a session (over the doodles). Until the Give and Receive screens come (UI 12, UI 13), a role shows its name and a pill that opens the sheet again.
- **react-native 0.88.0 → 0.88.1:** Expo SDK 58 (expo 58.0.21, Oct 28) now expects react-native 0.88.1, and `expo install --check` fails on 0.88.0 (the first U10 run on Oct 29 stopped there). The lock file changes only the react-native packages.

---

## UI 11. The app frame: the ☰ header, the top bar, the safe area

**When:** Oct 29 2026.
**Meta model:** none.
**Files:** `mobile/src/screens/Frame.tsx`, `mobile/src/session.ts`, `mobile/App.tsx`, `mobile/src/ui.tsx`, `mobile/src/theme.ts`, `mobile/src/words.ts`, `mobile/tests/style.test.mts`, `mobile/docs/style_guide.md`, `mobile/package.json`, `mobile/package-lock.json`
**Sources:** `docs/sources/U11.md`

- **Written again:** the frame is new code on the U5 parts; the rewrite guard finds no line of the earlier app's mobile code.
- **Phone and tablet:** a header with ☰ (it opens the role sheet; the role on view is marked) and "V3RDE".
- **Wide screen:** a top bar with "V3RDE", the role tabs Give and Receive, and "Log out".
- **Layout:** `layoutFor` also gives the columns (1, 2, 3), the top bar (expanded only) and the content width (560, 900, 1120 pt).
- **Safe area:** new package `react-native-safe-area-context` ~5.10.1, the version Expo SDK 58 pins; the screens keep clear of the status bar and the home bar.
- **Parts:** `Toast` (a short message at the bottom); a session context for the screens. When the services answer 401, the app goes back to sign-in with "Your session has ended. Sign in again."
- Contrast tests for the new colour pairs (the chosen tab, the toast).

---

## UI 12. The Receive screen

**When:** Oct 29 2026.
**Meta model:** none.
**Files:** `mobile/src/screens/Receive.tsx`, `mobile/src/listing.ts`, `mobile/tests/listing.test.mts`, `mobile/App.tsx`, `mobile/src/ui.tsx`, `mobile/src/theme.ts`, `mobile/src/words.ts`, `mobile/tests/words.test.mts`, `mobile/docs/style_guide.md`, `mobile/package.json`
**Sources:** `docs/sources/U12.md`

- **Written again:** Receive is new code on the U5 parts; the rewrite guard finds no line of the earlier app's mobile code.
- **Feed:** "Available now", photo-first cards in 1, 2 or 3 columns with a live time pill (m:ss, "Collected", "Expired"); the next page loads near the end of the list; on a phone, pull down to load again.
- **Grab:** one request at a time; then a small sheet over the listing photo, "You grabbed it", with "Maps" (a map search) and "WhatsApp" (a message to the giver). A listing that someone took first (409) shows as collected, with a toast. The user's own listings show "Your listing" and no Grab.
- **Parts:** `ListingCard`, `TimePill`, `Grid`, `usePhoto`, `useNow`; the `Sheet` part gets a ✕ (`onClose`) and keeps clear of the home bar.
- **Rules:** `listing.ts` (time left, gone, pages without doubles, links, camera first), with tests.
