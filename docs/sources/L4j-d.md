# L4j-d sources: the review file

### D1. Each finding names its source and its reason
GEMBA-MQM gives each error a span and a category, so a person can check the finding. The review file keeps the check name or the judge model, the group and the reason for each finding.
Source: https://arxiv.org/abs/2310.13988

### D2. A person decides the texts that fail twice
A model that judges its own corrections again is biased toward them (Pride and Prejudice, ACL 2024), so the status `review` sends a text to a person, not to a third model try.
Source: https://aclanthology.org/2024.acl-long.826

### D3. JSON for one file that both pages read
Source: https://www.json.org/
