# L4k-b sources: compare the ways of translating

### D1. With no labels, compare measures, not accuracy
MetricX-24 QE gives a score to a translation with no reference; it ranks translations and is not an accuracy measure. The comparison reports the scores as they are.
Source: https://github.com/google-research/metricx

### D2. Two judges from different models, and their agreement
A panel of different models judges with less bias than one model (PoLL). The comparison reports the texts flagged by both judges and by one judge only.
Source: https://arxiv.org/abs/2404.18796

### D3. A model judge gives the errors, not a grade
GEMBA-MQM: error spans with a reason. The judges of #61 give the text and the problem, and the comparison counts these texts.
Source: https://arxiv.org/abs/2310.13988
