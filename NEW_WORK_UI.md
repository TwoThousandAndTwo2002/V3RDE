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
