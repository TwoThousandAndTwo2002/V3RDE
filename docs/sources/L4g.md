# L4g sources: the text graph, a context for every text

### D1. A context for every text, in ASD-STE100
The context tells the translator the kind of text, where it shows, and what it does. Without it, "Post {plural}" was translated as a noun (Publicación, Postagem) where the app needs a verb. gettext solves the same problem with a context on each string. The contexts are written in ASD-STE100, a controlled English with defined writing rules that non-native readers understand and that is valuable for translation.
Source: https://gnu.org/software/gettext/manual/html_node/Contexts.html
Source: https://asd-ste100.org/about_STE.html

### D2. A graph of the dependencies between texts
A change to one text can change the meaning of others: a joined line, the texts on the same screen, a pair such as Log out / sign in, and the texts that name the same idea. Change impact analysis uses the dependencies between parts (dependency analysis) and the recorded links between them (traceability analysis) to find how far a change reaches. The graph has both: edges found in the code (on_screen, part_of, uses) and edges made by hand (opens, pair).
Source: https://en.wikipedia.org/wiki/Change_impact_analysis

### D3. Concepts keep the words of one idea together
The texts that name one idea (listing, serving, pickup ...) are linked to one concept node. The next change gives the translator the approved translations of the other texts of the same concept, so one idea gets one word in each language. Research on terminology integration with LLMs gives the term constraints in the prompt in the same way.
Source: https://arxiv.org/pdf/2310.14451

### D4. Stale contexts stop the change
Each context stores the hash of the English text it was written for. A test fails when a text has no context, its English changed, or its text is gone, and `cmd/graph` lists what the change reaches, most connected first, for review.
Source: https://en.wikipedia.org/wiki/Change_impact_analysis

### D5. Root cause of the two lost texts
The extractor started a joined line only at a literal or a name, so a line that starts with "(" (`(a ? b : c) + " — your photo is kept."`) lost its literals: they were in neither en.json nor skipped.json. It now starts a line at "(" too (not at a call such as `f(`), and it counts every literal and fails if one is not in a message, a skip or an object key.
Source: https://learn.microsoft.com/it-it/globalization/internationalization/concatenation
