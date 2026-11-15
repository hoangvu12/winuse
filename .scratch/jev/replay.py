"""Resends a dumped run request N times; prints the operation and achieved odds.

Usage: python -I replay.py <dump.json> [n] [drop-question ...]
"""
import json, os, sys, urllib.request, ssl

path, n, drop = sys.argv[1], int(sys.argv[2]) if len(sys.argv) > 2 else 3, sys.argv[3:]
req = json.load(open(path, encoding="utf-8"))["request"]
for q in drop:
    req["questions"].pop(q, None)
body = json.dumps(req).encode()
for _ in range(n):
    r = urllib.request.Request(os.environ["TYPESAFE_BASE_URL"].rstrip("/") + "/v1/systemone", body,
                               {"Authorization": "Bearer " + os.environ["TYPESAFE_API_KEY"],
                                "Content-Type": "application/json"})
    with urllib.request.urlopen(r, context=ssl.create_default_context(), timeout=60) as resp:
        a = json.load(resp)["answers"]
    op = a["operation"]["probabilities"]
    print(f"DONE={op.get('DONE', 0):.2f} CLICK={op.get('CLICK', 0):.2f} achieved={a['achieved']['noul']:.2f}", flush=True)
