# L4h sources: each prompt gets the context of its text from the graph

### D1. The context goes into the prompt
The first runs had no context, and "Post {plural}" became a noun. The prompt now gives what the text does, what is near it, and what its tags hold. Localization teams give translators the same kind of information (where and how a string is used). gettext uses a context for each string for the same reason.
Source: https://lingohub.com/blog/the-power-of-context-how-visual-context-for-translation-improves-quality
Source: https://gnu.org/software/gettext/manual/html_node/Contexts.html

### D2. Earlier translations of the same idea keep one word for one idea
For each concept of the text, the prompt lists up to 6 earlier translations of other texts that name it. Research on terminology integration into MT with LLMs gives the term constraints in the prompt in the same way.
Source: https://arxiv.org/pdf/2310.14451

### D3. The dedupe key is the text and its context
The user decided (2026-10-30) that with contexts the key is the text and its context. gettext gives the same string two translations when it has two contexts.
Source: https://gnu.org/software/gettext/manual/html_node/Contexts.html

### D4. Other texts show their values as [value]
The tags of the text stay <x1/>; the other texts in the block show their placeholders as [value], so that the model does not copy a {name} of another text (the restore step rejects a bare {name}, L4a).
Source: https://preview.aclanthology.org/setup/2023.mtsummit-research.13

### D5. A sweep runs a small batch with other settings
`-ids`, `-effort` and `-temperature` run the user's problem texts with other settings, with 3 tries each (`-k 3`), so that a difference is not one random answer. Baseten accepts reasoning_effort and temperature for this model; the defaults stay Meta's recommended sampling and effort low.
Source: https://docs.baseten.co/reference/inference-api/chat-completions
Source: https://unsloth.ai/docs/models/muse-spark-open
