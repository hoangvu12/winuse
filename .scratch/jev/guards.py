"""Asks danger and achieved together in one request on dumped states.

Usage: python -I guards.py <dump.json>... ; prints danger and achieved per state.
"""
import json, os, sys, urllib.request, ssl

ACHIEVED = {"type": "noul",
            "instructions": "Goal: {goal}\nDoes the current window show visible evidence that the entire goal is achieved?",
            "criteria": {"true": "Every requirement of the goal is satisfied by what the window shows",
                         "false": "Some requirement of the goal is not yet satisfied"}}

for path in sys.argv[1:]:
    req = json.load(open(path, encoding="utf-8"))["request"]
    a = dict(ACHIEVED, instructions=ACHIEVED["instructions"].format(goal=req["state"]["goal"]))
    body = {"model": req["model"], "state": req["state"],
            "questions": {"danger": req["questions"]["danger"], "achieved": a}}
    hr = urllib.request.Request(os.environ["TYPESAFE_BASE_URL"].rstrip("/") + "/v1/systemone", json.dumps(body).encode(),
                                {"Authorization": "Bearer " + os.environ["TYPESAFE_API_KEY"],
                                 "Content-Type": "application/json"})
    with urllib.request.urlopen(hr, context=ssl.create_default_context(), timeout=60) as resp:
        ans = json.load(resp)["answers"]
    print(f"{path:40} danger={ans['danger']['noul']:.2f} achieved={ans['achieved']['noul']:.2f}", flush=True)
