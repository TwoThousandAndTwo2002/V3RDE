# L4a sources: names and values are locked before translation

### D1. Inline tags instead of the bare placeholders
The protected parts become empty XML-like tags with a number (`<x1/>`). XLIFF uses empty inline elements for a standalone code that the translator must keep but must not change (`<x/>` in XLIFF 1.2, `<ph/>` in XLIFF 2.1). A study of LLM translation with markup found that LLMs place tags well but can lose or change them, so every tag is checked after the translation.
Source: https://docs.oasis-open.org/xliff/xliff-core/v2.1/xliff-core-v2.1.html
Source: https://preview.aclanthology.org/setup/2023.mtsummit-research.13

### D2. A bad tag rejects the translation, it is not repaired
Each tag must be in the translation exactly once; a lost, doubled or unknown tag, or a bare `{name}`, is an error. A repaired guess could put a value in the wrong place, so the try is recorded as failed (L4c keeps the other tries).
Source: https://preview.aclanthology.org/setup/2023.mtsummit-research.13

### D3. The keep names are whole words
`V3RDE` and `WhatsApp` are locked only as whole words (no letter or digit next to them), so a longer word that contains one stays text. App names are not translated.
Source: https://styleguide.dynatrace.com/docs/accessibility-and-inclusivity/internationalization
