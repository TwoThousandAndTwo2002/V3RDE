# L4i sources: translate many, score once

### D1. Root cause of the slow sweep: one container start for each run
The sweep had 25 runs, and each run started the MetricX container and loaded the fp32 Large model (several GB) again; the scoring itself took about 20 s for 18 pairs. Loading a model once and scoring all inputs in one process is the way MetricX's own predict.py works (one input file, one output file).
Source: https://github.com/google-research/metricx

### D2. Scores go into the ledger as JSON lines
The ledger keeps one JSON value on each line. cmd/score adds one score line for each try, so a run can be scored later and the ledger shows when each score came.
Source: https://jsonlines.org/

### D3. The choice is the same as before
For each text the try with the lowest MetricX QE score is kept (lower is better), as in #51; with no scores yet, the first good try is kept.
Source: https://github.com/google-research/metricx
