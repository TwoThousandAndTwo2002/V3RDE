# L4j-a sources: checks with no model

### D1. A score of one text does not see errors between texts
Context-aware evaluation shows that a sentence can be a good translation alone and wrong in its context (lexical cohesion: one idea, one word).
Source: https://aclanthology.org/P19-1116/

### D2. The checks are the standard localization QA checks
Okapi CheckMate has "target same as source", "inconsistency in target" (one source, different translations) and "inconsistency in source" (different sources, one translation). Our checks use the same idea on the text graph.
Source: https://okapiframework.org/wiki/index.php/CheckMate
Source: https://okapiframework.org/wiki/index.php/Quality_Check_Step

### D3. Report files sit next to the results
`<lang>.check.json` and `<lang>.judge.json` are JSON. Only `<lang>.json` is a result (one dot in the name), so a glob such as `runs-ctx/*/*.json` is safe for all commands.
Source: https://www.json.org/

### D4. A test must not depend on the order of goroutines
Go gives no order between goroutines that do not synchronize, so a count shared by the workers gives each message different numbers in each run. The fake now counts per message, under a mutex.
Source: https://go.dev/ref/mem
