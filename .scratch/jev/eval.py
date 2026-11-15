"""Element-picking eval for Jev-compatible System One servers.

Reads Window States from states/*.txt, asks each model to pick the Element
for a description, and reports accuracy and latency per model and layout.

  TYPESAFE_BASE_URL=https://api.codiv.ai TYPESAFE_API_KEY=... python -I eval.py [models...]

Layouts:
  state  - the Element lines go in the state; the choice options are bare
           indexes (use-browser's find).
  crit   - each option's description is its Element line; the state is
           only the Window line (options described by what they contain).
"""
import json, os, re, ssl, sys, time, urllib.request, statistics, pathlib

HERE = pathlib.Path(__file__).parent
BASE = os.environ.get("TYPESAFE_BASE_URL", "https://api.typesafe.ai").rstrip("/")
KEY = os.environ["TYPESAFE_API_KEY"]
LINE = re.compile(r"^\t*(\d+) (.*)$")

CASES = {
    "calc": [("the add button", {36}), ("equals", {37}), ("the 7 key", {46}),
             ("clear everything", {26}), ("square root", {31}),
             ("the result display", {14}), ("close the window", {6}),
             ("open the history", {15}), ("keep the window on top", {52}),
             ("decimal point", {49}), ("the volume slider", None)],
    "fixture": [("the text field", {5}), ("the Save As button", {7}),
                ("the notify-me checkbox", {13}), ("the Charlie item", {17}),
                ("the details section", {23}), ("the status text", {25}),
                ("the File menu", {2}), ("the disabled button", {8}),
                ("the print button", None)],
    "wiki": [("the search box", {7}), ("the search button", {8}),
             ("the log in link", {16}), ("dark theme", {54}),
             ("the page's revision history", {30}), ("create an account", {14}),
             ("the main menu", {3}), ("the talk page", {22}),
             ("the shopping cart", None)],
}


def elements(path):
    lines = path.read_text(encoding="utf-8-sig").splitlines()
    head = lines[0]
    els = []
    for l in lines[1:]:
        m = LINE.match(l)
        if m and m.group(1) != "0":
            els.append((m.group(1), m.group(2)))
    return head, els, "\n".join(l for l in lines if LINE.match(l) or l == head)


def post(body):
    req = urllib.request.Request(BASE + "/v1/systemone", data=json.dumps(body).encode(),
                                 headers={"Authorization": "Bearer " + KEY,
                                          "Content-Type": "application/json"})
    t = time.perf_counter()
    with urllib.request.urlopen(req, timeout=90) as r:
        out = json.load(r)
    return out, (time.perf_counter() - t) * 1000


def ask(model, layout, head, els, tree, desc, chunk):
    qs = {}
    for ci in range(0, len(els), chunk):
        part = els[ci:ci + chunk]
        if layout == "state":
            crit = {i: None for i, _ in part}
        else:
            crit = {i: t[:200] for i, t in part}
        crit["none"] = "no listed element matches the description"
        qs[f"where{ci // chunk}"] = {
            "type": "choice",
            "instructions": f'Which element of the window matches this description: "{desc}"? Answer with its number.' + os.environ.get("INSTR_SUFFIX", ""),
            "criteria": crit}
    qs["exists"] = {"type": "noul",
                    "instructions": f'Some element of the window matches this description: "{desc}"'}
    state = tree if layout == "state" else head
    body = {"model": model, "state": state, "questions": qs}
    out, ms = post(body)
    best, bp = "none", 0.0
    for k, a in out["answers"].items():
        if k.startswith("where") and a["choice"] != "none":
            p = a["probabilities"][a["choice"]]
            if p > bp:
                best, bp = a["choice"], p
    return best, bp, out["answers"]["exists"]["noul"], ms, out.get("model")


def main():
    models = sys.argv[1:] or ["openjev-0.1", "clef-flash-1.0", "jevk5-0.3", "clm-v0.1"]
    rows = []
    for model in models:
        for layout in os.environ.get("LAYOUTS", "state,crit").split(","):
            ok = n = 0
            lat = []
            fails = []
            for name, cases in CASES.items():
                head, els, tree = elements(HERE / "states" / f"{name}.txt")
                for desc, gold in cases:
                    try:
                        pick, p, ex, ms, served = ask(model, layout, head, els, tree, desc, 200)
                    except Exception as e:  # noqa: BLE001
                        fails.append(f"{name}/{desc}: ERROR {e}")
                        n += 1
                        continue
                    lat.append(ms)
                    # A pick counts when it is right and confident, or when
                    # nothing matches and the pick is rejected (p<0.5 or exists<0.5).
                    # GATE=choice drops the exists check: the choice's own
                    # "none" option and p carry the decision.
                    acted = pick != "none" and p >= 0.5 and (ex >= 0.5 or os.environ.get("GATE") == "choice")
                    good = (gold is None and not acted) or (gold is not None and acted and int(pick) in gold)
                    ok += good
                    n += 1
                    if not good:
                        fails.append(f"{name}/{desc}: picked {pick} p={p:.2f} exists={ex:.2f}")
            med = statistics.median(lat) if lat else 0
            rows.append((model, layout, ok, n, med, max(lat) if lat else 0))
            print(f"{model:16} {layout:5} {ok}/{n}  median {med:.0f} ms  max {max(lat) if lat else 0:.0f} ms", flush=True)
            for f in fails:
                print("    " + f)
    json.dump(rows, open(HERE / "eval-results.json", "w"), indent=1)


if __name__ == "__main__":
    main()
