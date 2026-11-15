"""Asks a dumped step's danger noul inside its request and alone, n times each.

Usage: python -I danger.py <dump.json> [n]
"""
import json, os, sys, urllib.request, ssl

path, n = sys.argv[1], int(sys.argv[2]) if len(sys.argv) > 2 else 2
req = json.load(open(path, encoding="utf-8"))["request"]


def send(r):
    hr = urllib.request.Request(os.environ["TYPESAFE_BASE_URL"].rstrip("/") + "/v1/systemone", json.dumps(r).encode(),
                                {"Authorization": "Bearer " + os.environ["TYPESAFE_API_KEY"],
                                 "Content-Type": "application/json"})
    with urllib.request.urlopen(hr, context=ssl.create_default_context(), timeout=60) as resp:
        return json.load(resp)["answers"]["danger"]["noul"]


alone = dict(req, questions={"danger": req["questions"]["danger"]})
for _ in range(n):
    print(f"in request={send(req):.2f}  alone={send(alone):.2f}", flush=True)
