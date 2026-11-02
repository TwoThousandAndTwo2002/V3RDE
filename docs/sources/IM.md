# IM — sources for each decision

Change: IM (branch `services`), the merge of branch `items` (62924d0, its entries 92 I1, 93 I3, 94 I4) into `services` (0109689, #111) (`NEW_WORK.md`, `inference/events.go`, `inference/service.go`, `mobile/src/screens/Capture.tsx`, `mobile/src/screens/Voice.tsx`, `mobile/tests/voice.test.mts`, `post/voice_test.go`, `docs/sources/I3.md` (services' file kept), `docs/sources/items_I3.md` (new), `post/prompts.go`, `post/prompts_pins.txt`, `mobile/tests/app_texts.json`, `mobile/web-dist/`; NEW_WORK.md, the number read at run time).

T9 (2026-11-02): "merge branch `items` into `services`", "a high priority change … that comes before everything"; the plan `docs/v3rde_items_merge_2026-11-02.md` (project doc). Answers: items' `I3.md` goes to `docs/sources/items_I3.md` ("Yes, items_I3.md"); "Items merge first" (before the Rosette translation screens); I5 and I2: "translation is the priority what every is needed for translation screens do it if its not needed then no" (I5 and I2 are not needed for them: they come later, on `services`); the prompt fixes: "if they pertain to translation then yes if not then no".

Own evidence (the refs, fetched again on the Mac, 2026-11-02): origin/services 0109689, origin/items 62924d0, merge base bbfed58; learn, logo, about, rescue, reach, LIF, ui and main are in services; no `Rosette` branch. The test merge: 12 conflicts, the same as the plan (§3).

### D368. A real merge; every entry of both sides in number order
- https://git-scm.com/docs/git-merge — "git merge --no-ff --no-commit" makes the merge and stops before the commit, so the script solves the conflicts with pinned content and checks the merged tree (`MERGE_TREE`) before its own changes. The items commits keep their history.
- https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/addressing-merge-conflicts/about-merge-conflicts — a conflict comes from changes to the same lines on two branches. In `NEW_WORK.md` both sides added entries 92–94 after entry 91. The solution keeps every entry of both sides byte for byte, in number order; on the same number the services entry comes first, then the learn entry (both already in that order on services), then the items entry (the plan, §3). The merge entry is 1 + the highest number of all branches (read at run time). Items' entry 93 names its sources file `docs/sources/items_I3.md` (D371).

### D369. The code conflicts: everything of both sides
- https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/addressing-merge-conflicts/resolving-a-merge-conflict-using-the-command-line — "decide if you want to keep only your branch's changes, keep only the other branch's changes, or make a brand new change, which may incorporate changes from both branches". Each conflict here is two additions next to each other, so the solution keeps both:
  - `inference/events.go`: the event fields of both (services: `Answer` LR1, `Settings` P1; items: `Barcodes`, `Of`, `Find`).
  - `inference/service.go`: the three routes (`/api/analyze/learn`, `/api/analyze/learn/{day}`, `POST /api/analyze/find`).
  - `mobile/src/screens/Capture.tsx`: the translated `noItemsText` (services #92) and items' barcode names; items' `NO_ITEMS_TEXT` is not imported (the body uses `noItemsText()`).
  - `mobile/src/screens/Voice.tsx`: the earlier turn (X59, #105) and the find turns (I4): after an answer both `nextEarlier` and the turn number run; Start over clears both (`earlier.current = null`, `setFinds({})`, `turnCount.current = 0`).
  - `mobile/tests/voice.test.mts`, `post/voice_test.go`: both import lists and every test.
- The generated web build (`mobile/web-dist/`: `index.html` and 3 bundle files) is dropped and made again from the merged code (as X58 and X61).

### D370. The chat prompt: `voice.map` version 3 with a new pin row
- https://cheatsheetseries.owasp.org/cheatsheets/LLM_Prompt_Injection_Prevention_Cheat_Sheet.html — the prompt states the expected output and its rules. The merged chat prompt has the X59 rules (the earlier turn, "which item") and the items I4 rule (`find`: at most 3 short names, only a food the donor says is missing). The text is new, so the pin test of `post` failed on the merged code (measured: "voice.map is version 3 sha b2452a58d63f1f72 now; its newest pin row is version 2"). T9's rule (Oct 31): a prompt changes only with a new version and a new pin row. `post/prompts.go` has `voice.map` version 3; `post/prompts_pins.txt` gets one new row at the end, `voice.map 3 b2452a58d63f1f72` (the sha from the test output); the older rows do not change.
- https://csrc.nist.gov/pubs/sp/800/107/r1/final — the pin is the first 64 bits of the SHA-256 of the golden text (as P1).
- The find call's prompt (`muse:find`, the plan §4.2): no test fails on it (measured: inference and post pass on the merged code), and it is not a translation part, so it is not in this merge (T9). Its calls log the prompt id `unknown` until a later change registers it.

### D371. `docs/sources/I3.md`: services' file; items' file is `items_I3.md`
- https://git-scm.com/docs/git-merge#_how_conflicts_are_presented — an add/add conflict: both branches made a file with the same name. On services, `I3.md` is the sources file of the learn report page (entry 98); on items, the find service (entry 93). T9: keep services' file, put items' file at `docs/sources/items_I3.md` (its text is not changed), and say so in the merge entry and in entry 93.

### D372. The new app texts are in the translation list
- https://webtranslateit.com/learn/locales/icu-messageformat-guide — "Put the whole sentence in one message." The items texts (barcodes found, reading, looking for, found, not found, the fourth chat hint, …) are whole messages in the word tables; on the merged code the test "every text of the app shows its translation" failed only because `mobile/tests/app_texts.json` did not list them. It is made again by the i18n tool (`V3RDE_UPDATE=1 go test -run TestAppTextsFile`): 10 new ids, none removed. They are English until the translation round at the deploy translates them; that round's work is what the Translations tab will show.

### D373. The review fixes: the two sides together
- https://git-scm.com/docs/git-merge — a merge with no text conflict can still be wrong when two changes meet; the review read the code of both sides together.
- The chat (X59) keeps the list as its answer left it and sends the earlier question only while the list is the same (`earlierToSend`); a find (I4) adds items to the list. Now `earlierWithFound` (`mobile/src/earlierfound.ts`, new) adds the found items to the remembered list as well, as `turnsWithFound` does for the turns. Test: "IM: found items join the earlier turn's list …" (it shows the question was lost without the fix).
- https://developers.google.com/machine-learning/guides/rules-of-ml/ — Rule #29: "save the set of features used at serving time". The scan's answer line (LR1) must say how each item was made. A catalog item of a photo with many barcodes (I1) now has `via` = barcode (`inference/service.go`); an item that the find service added (a `found` edit, I4) is "found" in the learn report (`inference/learn.go`), not "<source> (before LR1)". Tests: `TestIMManyBarcodesInTheAnswerLine`, `TestIMLearnFoundItems` (both fail without the fix: checked).

Review (independent, 15 minutes at most): no lost change of either side; NEW_WORK.md, `I3.md`, `items_I3.md`, the pins and the app texts as intended. Four findings, all fixed: (1) a find after the chat's question removed the question (D373); (2) the catalog items of a photo with many barcodes had no `via` in the answer line (D373); (3) found items were "(before LR1)" in the learn report (D373); (4) the Files line of entry 112 did not name `docs/sources/IM.md`.

Tests (measured in the cloud on the merged code; the Mac run gives the final counts): inference 115 -> 135, post 120 -> 121, mobile 203 -> 210; the other modules as before.
