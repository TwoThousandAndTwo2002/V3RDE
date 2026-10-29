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
