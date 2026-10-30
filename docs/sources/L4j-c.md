# L4j-c sources: one retranslation with the judge's reason, spread through the graph

### D1. The second try gets the judge's reasons, not other settings
The sweep of #57 (docs/sources/L4h.md) showed no gain from effort medium/high or temperature 0.7 on the 7 problem texts. Ki and Carpuat (NAACL Findings 2024): an LLM that gets the error annotations of a translation post-edits it better than with no feedback.
Source: https://arxiv.org/abs/2404.07851

### D2. A correction goes to the texts of the same idea
Lexical cohesion: one idea keeps one word in all the texts that name it; a text can be correct alone and wrong next to the others. So a corrected word goes to the pairs, joined parts and texts of the same concept.
Source: https://aclanthology.org/P19-1116/
Localization QA reports "inconsistency in target" for the same reason.
Source: https://okapiframework.org/wiki/index.php/CheckMate

### D3. Once only, then a person
A model that judges and corrects its own output again and again increases its self-bias (Pride and Prejudice, ACL 2024). So a text gets one retranslation (a fix or a spread); a text that the judges flag again goes to human review.
Source: https://aclanthology.org/2024.acl-long.826

### D4. The ledger stays JSON lines
The new tries are added to the same ledger with `fix` and `spread_from`, so every try stays on record and `cmd/score` keeps the marks.
Source: https://jsonlines.org/
