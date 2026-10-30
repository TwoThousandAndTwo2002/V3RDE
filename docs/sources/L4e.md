# L4e sources: requests at a steady rate, and a resume run

### D1. Root cause of the 25 missing messages: the rate limit
The ledger of run `20261029T215819Z` has 25 failed tries, all `HTTP 429: Rate limit exceeded`, and 179 good ones, all in about 80 seconds. Baseten's Model API limits are per minute and refill all the time. The default limits are 15 or 120 requests per minute for a basic account, and a model can set its own limit. Baseten asks for a steady rate of requests and exponential backoff after a 429. The client tried 4 times in 7 seconds, which is shorter than the limit's window.
Source: https://docs.baseten.co/inference/model-apis/rate-limits-and-budgets

### D2. A pacer from the limit header
Each answer has `x-ratelimit-limit-requests`. The pacer spaces the requests evenly at that limit (one request every 60/limit seconds), so the workers do not send in bursts. `-rpm` sets a fixed limit when needed.
Source: https://docs.baseten.co/inference/model-apis/rate-limits-and-budgets

### D3. Exponential backoff with jitter, longer than one minute
After a 429 with no `Retry-After`: 1, 2, 4, 8, 16, 32 seconds (63 seconds in all, more than the one-minute window), each with a random part of a second so that the workers do not try again at the same time.
Source: https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/

### D4. Resume instead of a full new run
The 179 good answers are kept. `-resume` sends only the missing ids, so the cost of the first run is not paid again, and the merge refuses an earlier result of another language, `en.json` commit, model or k.
Source: https://docs.baseten.co/inference/model-apis/rate-limits-and-budgets
