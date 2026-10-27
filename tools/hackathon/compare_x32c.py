#!/usr/bin/env python3
"""X32c bench: the live engine vs the early whole-photo call, photo by photo.

  python3 compare_x32c.py <probe_live.json> <probe_early.json> [out.json]

Each probe JSON is one ie-probe run (-parallel 1, its own SAM calls), so
the times are each run's own wait. Per photo: what SAM kept (0, 1, 2+), the
path (agent / whole / multi), the wait (ms), the cost, the listings and the
names. Per group: median and max wait, total cost. The group is what SAM's
FIRST find kept, read from the early run (v2: in the live run an agent
replaces "kept" with its own count, so v1 grouped two agent photos wrong).
No accuracy share is printed: the names are shown side by side.
"""
import json, statistics, sys


def load(p):
    runs = {}
    for r in json.load(open(p))["runs"]:
        inv = r.get("inventory") or {}
        calls = [c["purpose"] for c in inv.get("calls") or []]
        path = "agent" if any(c.startswith("muse:agent") for c in calls) else \
               "whole" if any(c.startswith("muse:whole") for c in calls) and not any(c.startswith("muse:single") for c in calls) else "multi"
        runs[r["photo"]] = {
            "kept": inv.get("kept", 0), "path": path, "ms": inv.get("ms", 0), "cost": inv.get("cost_usd", 0.0),
            "listings": len(inv.get("items") or []), "error": r.get("error", ""),
            "names": [it.get("name", "") for it in inv.get("items") or []],
            "calls": ["%s %d ms" % (c["purpose"], c["ms"]) for c in inv.get("calls") or []],
        }
    return runs


live, early = load(sys.argv[1]), load(sys.argv[2])
rows, groups = [], {}
print("%-24s %-5s | %-6s %7s %8s %4s | %-6s %7s %8s %4s | names (live -> early)" % (
    "photo", "kept", "live", "ms", "$", "list", "early", "ms", "$", "list"))
for p in sorted(live):
    a, b = live[p], early.get(p)
    if b is None:
        print("%-24s missing in the early run" % p)
        continue
    g = "0" if b["kept"] == 0 else "1" if b["kept"] == 1 else "2+"
    groups.setdefault(g, []).append((a, b))
    same = a["names"][:1] == b["names"][:1]
    print("%-24s %-5s | %-6s %7d %8.4f %4d | %-6s %7d %8.4f %4d | %s" % (
        p[:24], b["kept"], a["path"], a["ms"], a["cost"], a["listings"], b["path"], b["ms"], b["cost"], b["listings"],
        ("same: " if same else "") + "%s -> %s" % ("; ".join(a["names"][:2])[:50], "; ".join(b["names"][:2])[:50])))
    for x, n in ((a, "live"), (b, "early")):
        if x["error"]:
            print("%30s %s error: %s" % ("", n, x["error"][:100]))
    rows.append({"photo": p, "group": g, "live": a, "early": b})

print("\n%-9s %6s | %-24s | %-24s | %-19s" % ("SAM kept", "photos", "live wait med / max (s)", "early wait med / max (s)", "cost live -> early"))
summary = {}
for g in ("0", "1", "2+"):
    if g not in groups:
        continue
    la = [a["ms"] for a, _ in groups[g]]; ea = [b["ms"] for _, b in groups[g]]
    lc = sum(a["cost"] for a, _ in groups[g]); ec = sum(b["cost"] for _, b in groups[g])
    summary[g] = {"photos": len(la), "live_ms_median": statistics.median(la), "live_ms_max": max(la),
                  "early_ms_median": statistics.median(ea), "early_ms_max": max(ea), "live_cost": lc, "early_cost": ec}
    print("%-9s %6d | %10.1f / %-11.1f | %10.1f / %-11.1f | $%.4f -> $%.4f" % (
        g, len(la), statistics.median(la) / 1000, max(la) / 1000, statistics.median(ea) / 1000, max(ea) / 1000, lc, ec))
if len(sys.argv) > 3:
    json.dump({"summary": summary, "rows": rows}, open(sys.argv[3], "w"), indent=1)
    print("\nwritten:", sys.argv[3])
