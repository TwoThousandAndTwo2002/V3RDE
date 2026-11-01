# LR2 — sources for each decision

Change: LR2 (services, branch `learn`), the learn report: what the donors kept and changed, for each prompt version (`inference/learn.go` (new), `inference/learn_test.go` (new), `inference/cmd/learn/main.go` (new), `inference/cmd/learn/main_test.go` (new), `inference/Dockerfile`; NEW_WORK.md 94).

T9 (2026-11-01): "i approve of I1"; on prompt changes: "full tracibility in the logs so that we can attribute a change in performance to a changed prompt" (P1 gives the prompt version of each call; LR1 the answer items).

Code review (services bbfed58): `inference/calibrate.go` (X44g) already reads the analyze events and counts, by box size and by day, how many names and servings the donors kept; it does not group by prompt and does not read the edits of X48u. `cmd/calibrate` prints it.

### D408. The groups of the report
- https://developers.google.com/machine-learning/guides/rules-of-ml/ — Rule #2: "First, design and implement metrics." The report counts, for each group: scans, cost, p50 and p95 time, labeled items, kept, name kept (`normName`, as calibrate), servings kept, removed.
- By prompt: the "id@version" of each answered Muse call of the scan (P1), sorted and joined; "before P1" for a line with no prompt fields; "no model call" for a barcode hit. So a new prompt version is a new row next to the old one.
- By how the item was made: `via` of the answer item (LR1); "<source> (before LR1)" for an older line; "no scan line" for labels whose scan is not in the days read.
- The fields: each edit field (title, servings, condition, diet, portions, weight, remove, photo, pickup) with how many edits, by how (hand, voice, text, undo) and the 5 most common from -> to pairs (not for pickup: the app sends no pickup text, X48u; not for undo; not for an edit with no values, such as remove).
- The servings of kept items: the same, the model higher, the model lower, and the mean of model - final.
- The calibration of X44g is part of the report, unchanged.
- Only the last labels line of a request counts (the app can send more than one report for one answer), by the order of the lines, not by the `ts` text: RFC 3339 times with fractions of a second do not sort as text (`10:00:00Z` after `10:00:00.5Z`).
- The calibration part counts every labels line, as X44g does; so its counts of label sets can be higher than the report's.

### D409. p50 and p95 by the nearest rank
- https://en.wikipedia.org/wiki/Percentile#The_nearest-rank_method — the value at rank ceil(p x n) of the sorted list; no interpolation, so each value is a real request time.

### D410. A command that changes nothing
- https://pubs.opengroup.org/onlinepubs/9699919799/functions/rename.html — rename replaces the target in one step: the route never reads a half-written report.
- The report never changes a setting or a prompt: a person reads it, and a change is a SUP change (a prompt change with a new version, P1). `cmd/learn` reads the -last whole UTC days before today (7; `-today` ends with today), and with `-out` writes `report-<last day>.json` and `.md` through a temporary file and a rename, mode 0644, and keeps the newest 90 (at least 1). The image has the binary at `/learn` (as `/calibrate`).

Own evidence: `TestLearnReport` (4 prompt groups, an error line, an unknown prompt, two labels lines for one request in an order the `ts` text does not show, a retake, labels with no scan, the edits and the servings), `TestLearnEmpty`, `TestLastDays`, `TestWriteAndPrune`; a run of the command on a small events folder wrote the two files. Independent review (1 defect, fixed: the last labels line was chosen by the `ts` text); its notes fixed: no empty pairs, `-keep` at least 1. Limit: the job reads 7 days of lines into memory (256 MB limit in LR3); at today's volume this is small. Tests: inference 110 -> 114.
