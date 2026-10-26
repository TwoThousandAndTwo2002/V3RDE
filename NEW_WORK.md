# Icarus — work built during the Meta Global AI Developer Hackathon

This file is the record of what is **new** in the Hackathon. Judges can see every new line with one compare:

    https://github.com/TwoThousandAndTwo2002/Icarus/compare/hackathon-start...hackathon/meta-2026

- **`main` at tag `hackathon-start`** is the base: the food-rescue app and its services as they were **before** the Hackathon. It is a scrubbed snapshot of our private prior product (commit `79c7d69`, built Oct 21–26 2026). It is not new work.
- **Branch `hackathon/meta-2026`** holds only work started at or after the kickoff (Oct 26 2026, 12:00 PM ET). Each entry below names the commit, the date and time (ET), and the Meta model it uses.

Times: the kickoff was Oct 26, 12:00 PM ET; the building period is Oct 27 – Nov 4.

---

## 1. A Meta model for the food gate (PE-Core), measured against the old gate

**When:** Oct 26 2026, 2:01 PM ET (bench run). After the kickoff, before the building period started. Committed Oct 26.
**Meta model:** Perception Encoder, `PE-Core-S16-384` (Apache-2.0).
**Files:** `tools/hackathon/pe_gate_bench.py`, `tools/hackathon/results/bench_20261026T180111Z.{txt,json}`

**Why.** The base gate (food / not food, before any cloud call) used MobileCLIP2-S0. Its weights carry Apple's research-only licence (`apple-amlr`: "does not include … use in any commercial product or service"), so it cannot ship in a product. We looked for a Meta model of about the same size.

**How.** The same protocol as the base gate, on the same 1,432 gold-labelled photos: a new logistic head for each encoder, 5-fold out-of-fold on DEV (573), threshold = the highest with false retake ≤ 3%, applied once to HELDOUT (859). Preprocessing comes from timm's own data config.

**Result (HELDOUT):**

| Model | Size | AUROC | Catch (not-food refused) | False retake (food refused) | Oracle catch | Pass |
|---|---|---|---|---|---|---|
| MobileCLIP2-S0 fp32 @192 (base) | 46.2 MB | 0.970 | 0.817 | 2.9% | 0.817 | bar |
| **PE-Core-S16-384 fp16** | **47.2 MB** | **0.980** | **0.839** | **2.3%** | **0.863** | **yes** |
| PE-Core-T16-384 fp32 | 24.2 MB | 0.973 | 0.758 | 1.2% | 0.798 | no (catch) |

PE-Core-S16 refuses more non-food and less real food than the old gate, at the same file size. Next: export to LiteRT, verdict parity (0 flips), then shadow mode on the server.
