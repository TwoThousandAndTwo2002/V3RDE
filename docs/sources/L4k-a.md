# L4k-a sources: translate one screen in one call

### D1. Root cause: one call for each text gives one idea different words
In the run of #60-#63, each reached text got its own call, and zh "Your listings" became 我的发布 while texts of the same idea kept 你的 or got 您的. Lexical cohesion needs the texts of one idea to be translated with knowledge of each other.
Source: https://aclanthology.org/P19-1116/

### D2. An LLM that translates a whole group keeps the words consistent
Document-level translation with LLMs (EMNLP 2023): a model that gets the whole document in its context translates with better consistency and discourse than one segment at a time.
Source: https://aclanthology.org/2023.emnlp-main.1036

### D3. Fewer calls under a fixed limit
The Baseten Model API limit is a number of requests per minute (100 for this key, and it will not be raised), so one call for a group of texts is the way to translate faster.
Source: https://docs.baseten.co/inference/model-apis/rate-limits-and-budgets

### D4. One JSON object for the answer, and one call for each text that fails
The answer is one JSON object (id -> translation), read by the same parser for every group; a text that is not in the object, or that loses a tag, gets the single-text call of #51, so no text is lost.
Source: https://www.json.org/
