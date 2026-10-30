# L4j-b sources: two judges on concepts and screens

### D1. A model can judge translations and give the errors
GEMBA-MQM asks a model to mark error spans with a category and gives results close to human MQM labels. Our judges give the text, the problem and a suggestion.
Source: https://arxiv.org/abs/2310.13988

### D2. A model prefers its own output; use two judges
Self-bias: a model rates its own translations higher. The translator is Muse Spark Open, so Muse Spark 1.3 is a second judge, and a flag from either one counts until labels show which one to trust.
Source: https://aclanthology.org/2024.acl-long.826

### D3. Group by concept and by screen
Errors of cohesion and of pairs are between texts, so the judge must see the texts together (see L4j-a D1).
Source: https://aclanthology.org/P19-1116/

### D4. Meta Model API
Responses endpoint, `reasoning.effort` set for each request (low, as in the Icarus decision D12).
Source: https://dev.meta.ai/docs/protocols/responses
Source: https://dev.meta.ai/docs/reasoning

### D5. Baseten limits
429 answers are retried with backoff, as in #53.
Source: https://docs.baseten.co/inference/model-apis/rate-limits-and-budgets
