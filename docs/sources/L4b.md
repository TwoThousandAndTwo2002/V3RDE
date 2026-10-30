# L4b sources: a Go client for Muse Spark Open on Baseten

### D1. The OpenAI-compatible chat completions endpoint
`POST https://inference.baseten.co/v1/chat/completions` with `Authorization: Bearer <key>`. Only `n = 1` is supported. The answer is `choices[0].message.content`, and `usage.completion_tokens_details.reasoning_tokens` counts the reasoning. 429 is the rate limit and 500 a server error.
Source: https://docs.baseten.co/reference/inference-api/chat-completions

### D2. The sampling settings
Meta's recommended settings for Muse Spark Open are temperature 1.0, top_p 1.0 and top_k off. Baseten's `top_k` default is 50 and `-1` turns it off, so the client sends `top_k: -1`.
Source: https://unsloth.ai/docs/models/muse-spark-open
Source: https://docs.baseten.co/reference/inference-api/chat-completions

### D3. reasoning_effort low, and no answer is an error
Our endpoint tests on 2026-10-29 (before #44): with no effort set, the reasoning used all 2000 tokens and `content` was empty (`finish_reason: length`); with `low` the answers came back. `low` is the setting until MetricX scores show that more effort helps. The client sends Baseten's default `max_tokens` (4096) and treats empty content or `finish_reason: length` as no answer.
Source: https://docs.baseten.co/reference/inference-api/chat-completions

### D4. No logprobs, no seed, no n
On this endpoint `logprobs` is accepted but ignored, `prompt_logprobs` and `n > 1` return 400, and `seed` does not make the answers the same. The client does not send them; diversity comes from separate calls.
Source: https://docs.baseten.co/reference/inference-api/chat-completions

### D5. The key stays out of the logs
The key is read from `BASETEN_API_KEY` (the name in Baseten's hackathon setup) and is replaced by `***` in every error text.
Source: https://docs.baseten.co/inference/model-apis/overview
