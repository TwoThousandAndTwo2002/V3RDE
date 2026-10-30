# L4f sources: each English text once, Docker checked first, the ledger written as it goes

### D1. One English text, one translation
The user decided (2026-10-30): repeated English text is translated once, always. A translation memory reuses an exact match so that the same text reads the same everywhere and is paid for once. gettext keeps one translation per string unless a context is added to the string.
Source: https://gnu.org/software/gettext/manual/html_node/Contexts.html

### D2. Collapse the earlier results with no new requests
The results made before this change follow the rule after `-collapse`: for each shared text the translation with the lowest MetricX score is kept (MetricX QE: lower is better). The score ranks the candidates; it does not prove that the kept one is correct (zh-Hans "no blob": the kept 无斑点 means "no spots"), so a person still checks the text.
Source: https://github.com/google-research/metricx

### D3. Check the scorer before the requests
Root cause of the lost run (2026-10-29): `docker.sock` was missing because Colima was not running, so `qe/score.sh` failed after every request was sent and paid for. `docker info` and `docker image inspect` now run first.
Source: https://docs.docker.com/reference/cli/docker/system/info/

### D4. Write the ledger as the answers come in
JSON Lines has one value on each line, so the ledger can grow one line at a time. A try line is written when its answer comes in and a score line after the scoring; a failure later in the run keeps the answers that were paid for.
Source: https://jsonlines.org/
