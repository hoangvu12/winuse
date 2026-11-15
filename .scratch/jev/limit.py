"""Finds the server's input limit: one noul over a state of growing size.

Usage: python -I limit.py ; env TYPESAFE_BASE_URL (default TypeSafe), TYPESAFE_API_KEY.
"""
import json, os, ssl, urllib.error, urllib.request

BASE = os.environ.get("TYPESAFE_BASE_URL", "https://api.typesafe.ai").rstrip("/")
LINE = "123 link Some article title about a topic Description: more words here\n"  # ~70 chars


def tokens(n_lines):
    body = {"model": "jev-latest", "state": LINE * n_lines,
            "questions": {"q": {"type": "noul", "instructions": "Is there a link?"}}}
    r = urllib.request.Request(BASE + "/v1/systemone", json.dumps(body).encode(),
                               {"Authorization": "Bearer " + os.environ["TYPESAFE_API_KEY"],
                                "Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(r, context=ssl.create_default_context(), timeout=60) as resp:
            return json.load(resp)["usage"]["input_tokens"]
    except urllib.error.HTTPError as e:
        return "HTTP %d %s" % (e.code, e.read()[:120].decode(errors="replace"))


lo, hi = 200, 4000
print(lo, tokens(lo), flush=True)
print(hi, tokens(hi), flush=True)
while hi - lo > 50:
    mid = (lo + hi) // 2
    t = tokens(mid)
    print(mid, t, flush=True)
    if isinstance(t, int):
        lo = mid
    else:
        hi = mid
