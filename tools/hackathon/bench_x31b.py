#!/usr/bin/env python3
"""X31b bench: compares Muse variants on the same photos (one probe JSON).

  python3 tools/hackathon/bench_x31b.py <probe.json> <x30b_labels.json> [out.json]

Per variant:
  muse_wait  = longest first-round Muse call + every retry call (ms): the
               wait SAM does not hide. SAM is shared between variants
               (SAMCache), so the engine's own total is not comparable.
  cost       = every call at its production price (cached SAM included).
  out tokens per named box, reasoning share.
  names vs labels: each named box is matched to the X30b label of the
               same photo (box IoU >= 0.5 on the 0-1000 grid); its name is
               compared (normalised) with the labelled name:
               same_correct | same_wrong | same_unreadable | new (a new name needs
               a label).
  near-duplicate listings: pairs of groups in one photo whose normalised
               names share >= 50% of their words (Jaccard).
No accuracy share is printed: "new" names have no label yet.
"""
import json, re, statistics, sys

FILLER = {"and", "the", "with", "of", "a"}


def norm(s):
    s = (s or "").lower().replace("&", " and ")
    out = []
    for w in re.sub(r"[^a-z0-9]+", " ", s).split():
        if w in FILLER:
            continue
        if len(w) >= 4 and w.endswith("s") and not w.endswith("ss"):
            w = w[:-1]
        out.append(w)
    return " ".join(out)


def iou(a, b):
    ix = max(0, min(a[2], b[2]) - max(a[0], b[0])); iy = max(0, min(a[3], b[3]) - max(a[1], b[1]))
    i = ix * iy
    u = (a[2] - a[0]) * (a[3] - a[1]) + (b[2] - b[0]) * (b[3] - b[1]) - i
    return i / u if u > 0 else 0


def jacc(a, b):
    x, y = set(a.split()), set(b.split())
    return len(x & y) / len(x | y) if x | y else 0


def pctl(v, q):
    v = sorted(v)
    return v[min(len(v) - 1, int(round(q * (len(v) - 1))))] if v else 0


def main(probe, labels, outp=None):
    d = json.load(open(probe))
    lab = {}
    L0 = json.load(open(labels))
    for r in (L0["rows"] if isinstance(L0, dict) else L0):
        o = r.get("original")
        if o and o.get("norm"):
            lab.setdefault(r["photo"], []).append((o["norm"], o["name"].replace("NF:", ""), o["label"]))
    res = {}
    for run in d["runs"]:
        inv = run.get("inventory") or {}
        if run["variant"] == "gate" or not inv:
            continue
        v = res.setdefault(run["variant"], {"photos": 0, "errors": 0, "wait": [], "cost": [], "out": 0, "reason": 0,
                                            "named": 0, "listings": 0, "same_correct": 0, "same_wrong": 0, "same_unreadable": 0, "new": 0,
                                            "no_label": 0, "near_dup": 0, "rejected": 0})
        v["photos"] += 1
        if run.get("error"):
            v["errors"] += 1
        calls = [c for c in inv.get("calls", []) if c["model"] != "sam-3.1"]
        first = [c["ms"] for c in calls if re.match(r"muse:single(-\d+)?$", c["purpose"])]
        retry = [c["ms"] for c in calls if c["purpose"].startswith("muse:retry")]
        if first:
            v["wait"].append(max(first) + sum(retry))
        v["cost"].append(sum(c.get("cost_usd", 0) for c in inv.get("calls", [])))
        for c in calls:
            u = c.get("usage", {})
            v["out"] += u.get("output_tokens", 0); v["reason"] += u.get("reasoning_tokens", 0)
        v["named"] += inv.get("named", 0); v["listings"] += len(inv.get("items") or [])
        v["rejected"] += len(inv.get("rejected") or [])
        names = [norm(it["name"]) for it in inv.get("items") or []]
        v["near_dup"] += sum(1 for i in range(len(names)) for j in range(i + 1, len(names)) if jacc(names[i], names[j]) >= 0.5)
        L = lab.get(run["photo"], [])
        for it in (inv.get("items") or []) + (inv.get("not_listed") or []):
            for nb in it.get("norm_boxes") or []:
                best = max(L, key=lambda l: iou(nb, l[0]), default=None)
                if not best or iou(nb, best[0]) < 0.5:
                    v["no_label"] += 1
                elif norm(it["name"]) == norm(best[1]):
                    v[{"correct": "same_correct", "wrong": "same_wrong"}.get(best[2], "same_unreadable")] += 1
                else:
                    v["new"] += 1
    out = {}
    for k, v in res.items():
        out[k] = {"photos": v["photos"], "errors": v["errors"],
                  "muse_wait_ms": {"median": statistics.median(v["wait"]) if v["wait"] else 0, "p90": pctl(v["wait"], 0.9), "max": max(v["wait"] or [0])},
                  "cost_usd_per_photo": round(sum(v["cost"]) / max(1, v["photos"]), 4),
                  "out_tokens_per_named_box": round(v["out"] / max(1, v["named"]), 1),
                  "reasoning_share_of_output": round(v["reason"] / max(1, v["out"]), 3),
                  "listings": v["listings"], "near_duplicate_pairs": v["near_dup"], "rejected_boxes": v["rejected"],
                  "names_vs_labels": {"same_as_correct_label": v["same_correct"], "same_as_wrong_label": v["same_wrong"], "same_as_unreadable_label": v["same_unreadable"],
                                      "new_name": v["new"], "no_label_box": v["no_label"]}}
    print(json.dumps(out, indent=1))
    if outp:
        json.dump(out, open(outp, "w"), indent=1)


if __name__ == "__main__":
    main(*sys.argv[1:])
