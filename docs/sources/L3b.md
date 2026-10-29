# L3b sources: Arabic is `ar`

### D1. `ar` instead of `ar-001`, for the largest number of users
The user asked for the Arabic code that works for the largest population. Lookup matching removes subtags from the end of the requested tag until a tag matches (`ar-EG`, then `ar`). Every regional Arabic tag therefore reaches `ar`; none reaches `ar-001`. The written text stays Modern Standard Arabic, the one written form for all Arabic regions.
Source: https://datatracker.ietf.org/doc/draft-ietf-ltru-matching/15/
Source: https://www.localeplanet.com/icu/ar_001/
