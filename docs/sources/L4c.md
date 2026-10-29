# L4c sources: rung 1, translate and score every message

### D1. Separate calls, then rerank by a reference-free score
Baseten gives one answer per request, so k candidates are k requests. Choosing among candidates with a quality estimation metric (rerank) is a standard way to raise translation quality without references; with k = 1 the score only flags weak messages.
Source: https://aclanthology.org/2022.naacl-main.100
Source: https://docs.baseten.co/reference/inference-api/chat-completions

### D2. MetricX-24 in QE mode, lower is better
The scorer is the MetricX-24 image of L1/L2 (fp32 Large, 10 GB, no swap). In QE mode it scores a source and a translation with no reference, from 0 (best) to 25 (worst). All pairs go in one container run, so the model loads once.
Source: https://github.com/google-research/metricx

### D3. A ledger line for every try
Every try (also the failed ones) is one JSON line with its input, raw answer, result and settings, so a run can be checked and compared later. The output stays outside the repo until a person reviews it.
Source: https://jsonlines.org/

### D4. The scorer must return the same pairs in the same order
The score file must have one row for each pair, with the same source and translation in the same order and a prediction in 0..25; any other output stops the run.
Source: https://github.com/google-research/metricx
