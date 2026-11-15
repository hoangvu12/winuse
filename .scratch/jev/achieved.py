"""Replays dumped run states with variants of the achieved question.

Env: TYPESAFE_BASE_URL, TYPESAFE_API_KEY. Prints p(achieved) per state and variant.
"""
import json, os, sys, urllib.request, ssl, pathlib

HERE = pathlib.Path(__file__).parent
CASES = [  # (dump, expected)
    ("dump-ddg3/step-03.json", False),  # DuckDuckGo results not yet loaded
    ("dump-ddg3/step-02.json", False),  # query typed, suggestions open
    ("dump-wiki2/step-02.json", False),  # query typed in Wikipedia search
    ("dump-gh/step-01.json", True),  # the GitHub repository page
    ("dump-wiki2/step-03.json", True),  # the Kurt Godel article
]

VARIANTS = {
    "current": ("Goal: {goal}\nDoes the current window show visible evidence that the entire goal is achieved?",
                {"true": "Every requirement of the goal is satisfied by what the window shows",
                 "false": "Some requirement of the goal is not yet satisfied"}),
    "final": ("Goal: {goal}\nIs the window now showing the goal's end result, so that nothing is left to do? "
              "A typed query, open suggestions, a loading page or a list of search results is not the end result "
              "unless the goal asks only for that.",
              {"true": "The window shows the goal's end result",
               "false": "The window shows an earlier step on the way, or something else"}),
    "final-short": ("Goal: {goal}\nIs every part of the goal already done, as the window shows it now?",
                    {"true": "Every part of the goal is done; the window shows the end result",
                     "false": "A part of the goal is still to do: the window shows an intermediate step"}),
}


def ask(state, q):
    body = json.dumps({"model": "jev-latest", "state": state, "questions": {"achieved": q}}).encode()
    req = urllib.request.Request(os.environ["TYPESAFE_BASE_URL"].rstrip("/") + "/v1/systemone", body,
                                 {"Authorization": "Bearer " + os.environ["TYPESAFE_API_KEY"],
                                  "Content-Type": "application/json"})
    with urllib.request.urlopen(req, context=ssl.create_default_context(), timeout=60) as r:
        return json.load(r)["answers"]["achieved"]["noul"]


for path, want in CASES:
    d = json.loads((HERE / path).read_text(encoding="utf-8"))
    state = d["request"]["state"]
    row = [f"{path:28} want={str(want):5}"]
    for name, (instr, crit) in VARIANTS.items():
        p = ask(state, {"type": "noul", "instructions": instr.format(goal=state["goal"]), "criteria": crit})
        row.append(f"{name}={p:.2f}")
    print("  ".join(row), flush=True)
