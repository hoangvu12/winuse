#!/usr/bin/env python3
"""Round 2: agent-desktop vs winuse variants on bigger windows.

Variants run in blocks (the winuse helper's environment is fixed when it
starts, so each winuse variant gets a fresh helper), in two passes of
opposite order to cancel drift. Reports min and median per case: this
machine stalls process starts at random, and the min is the tool's own cost.

Clicks go only to Calculator's Seven and File Explorer's Refresh; Discord is
read-only.
"""
import argparse
import json
import os
import statistics
import subprocess
import sys
import tempfile
import time

HERE = os.path.dirname(os.path.abspath(__file__))
STOPPED = "stopped by the user"


def run(argv, env=None, timeout=120):
    start = time.perf_counter()
    proc = subprocess.run(argv, capture_output=True, text=True, encoding="utf-8",
                          errors="replace", timeout=timeout, env=env)
    elapsed = (time.perf_counter() - start) * 1000
    if STOPPED in proc.stdout + proc.stderr:
        raise SystemExit(f"winuse stopped: {proc.stdout} {proc.stderr}")
    return elapsed, proc


def ad_env(proc):
    try:
        return json.loads(proc.stdout)
    except ValueError:
        return {}


def ad_nodes(env):
    tree = env.get("data", {}).get("tree")
    if not tree:
        return None
    count, stack = 0, [tree]
    while stack:
        node = stack.pop()
        count += 1
        stack.extend(node.get("children", []))
    return count


def ad_ref(env, role, match):
    stack = [env.get("data", {}).get("tree", {})]
    while stack:
        node = stack.pop()
        if node.get("role") == role and match(node.get("name") or "") and node.get("ref_id"):
            return node["ref_id"]
        stack.extend(node.get("children", []))
    return None


class Sample:
    def __init__(self, ms, ok, nbytes, nodes=None):
        self.ms, self.ok, self.bytes, self.nodes = ms, ok, nbytes, nodes


class AD:
    def __init__(self, exe):
        self.exe = exe
        self.env = dict(os.environ)
        # A path agent-desktop creates itself (it refuses a state root whose
        # DACL it did not write), outside the repo: refmaps hold desktop text.
        self.env["AGENT_DESKTOP_HOME"] = os.path.join(tempfile.gettempdir(), f"ad-bench-state-{os.getpid()}")
        self.env.pop("AGENT_DESKTOP_SESSION", None)

    def call(self, *args):
        ms, proc = run([self.exe, *args], env=self.env)
        env = ad_env(proc)
        return Sample(ms, bool(env.get("ok")), len(proc.stdout.encode()), ad_nodes(env)), env

    def cases(self, t):
        w = f"w-{t['hwnd']}"
        out = [
            ("read (full)", lambda: self.call("snapshot", "--window-id", w, "--max-depth", "30")[0]),
            ("read (ad -i --compact)", lambda: self.call("snapshot", "--window-id", w, "-i", "--compact")[0]),
            ("skeleton", lambda: self.call("snapshot", "--window-id", w, "--skeleton")[0]),
            ("find button --first", lambda: self.call("find", "--window-id", w, "--role", "button", "--first")[0]),
        ]
        if t.get("click"):
            role, match = t["click"]["ad_role"], t["click"]["ad_match"]
            ref = None
            for _ in range(5):
                _, env = self.call("snapshot", "--window-id", w, "-i")
                ref = ad_ref(env, role, match)
                if ref:
                    break
                time.sleep(1)
            if not ref:
                raise SystemExit(f"agent-desktop: no click target in {t['name']}")

            def read_click():
                s1, env = self.call("snapshot", "--window-id", w, "-i")
                fresh = ad_ref(env, role, match)
                s2 = self.call("click", fresh)[0] if fresh else Sample(0, False, 0)
                return Sample(s1.ms + s2.ms, s1.ok and s2.ok, s1.bytes + s2.bytes)

            out += [("click", lambda: self.call("click", ref)[0]), ("read then click", read_click)]
        return out


class WU:
    def __init__(self, exe, extra_env):
        self.exe = exe
        self.env = dict(os.environ)
        self.env.update(extra_env)

    def call(self, *args, nodes=False):
        ms, proc = run([self.exe, *args], env=self.env)
        n = sum(1 for line in proc.stdout.splitlines()
                if line.lstrip("\t")[:1].isdigit()) if nodes else None
        return Sample(ms, proc.returncode == 0, len(proc.stdout.encode()), n)

    def restart_helper(self):
        name = os.path.splitext(os.path.basename(self.exe))[0]
        subprocess.run(["powershell", "-NoProfile", "-Command",
                        f"Get-Process '{name}' -ErrorAction SilentlyContinue | Stop-Process -Force"],
                       capture_output=True)
        time.sleep(1.0)
        run([self.exe, "list-windows"], env=self.env)  # starts the helper with this env

    def cases(self, t):
        h = str(t["hwnd"])
        out = [("read (full)", lambda: self.call("get-window-state", h, nodes=True))]
        tail = [
            ("skeleton", lambda: self.call("get-window-state", h, "--skeleton", nodes=True)),
            ("find button --first", lambda: self.call("find", h, "--role", "button", "--first")),
        ]
        if not t.get("click"):
            return out + tail
        if t.get("click"):
            ref = t["click"]["wu_ref"]
            self.call("get-window-state", h)

            def read_click():
                s1 = self.call("get-window-state", h)
                s2 = self.call("click", h, ref)
                return Sample(s1.ms + s2.ms, s1.ok and s2.ok, s1.bytes + s2.bytes)

            out += [("click", lambda: self.call("click", h, ref)), ("read then click", read_click)]
        return out + tail


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--ad", required=True)
    p.add_argument("--wu", required=True)
    p.add_argument("--calc", required=True)
    p.add_argument("--explorer", required=True)
    p.add_argument("--discord", required=True)
    p.add_argument("--rounds", type=int, default=8)
    p.add_argument("--out", required=True)
    p.add_argument("--only", default="", help="comma-separated variant labels to run")
    a = p.parse_args()

    targets = [
        {"name": "Calculator", "hwnd": a.calc,
         "click": {"wu_ref": "name:Seven", "ad_role": "button", "ad_match": lambda n: n == "Seven"}},
        {"name": "Explorer System32", "hwnd": a.explorer,
         "click": {"wu_ref": "id:refreshButton", "ad_role": "button", "ad_match": lambda n: n.startswith("Refresh")}},
        {"name": "Discord", "hwnd": a.discord},
    ]
    variants = [
        ("agent-desktop", lambda: AD(a.ad)),
        ("winuse", lambda: WU(a.wu, {})),
        ("winuse no-settle", lambda: WU(a.wu, {"WINUSE_BENCH_NOSETTLE": "1"})),
        ("winuse no-cursor", lambda: WU(a.wu, {"WINUSE_BENCH_NOCURSOR": "1"})),
        ("winuse no-waits", lambda: WU(a.wu, {"WINUSE_BENCH_NOSETTLE": "1", "WINUSE_BENCH_NOCURSOR": "1"})),
    ]
    if a.only:
        keep = set(a.only.split(","))
        variants = [v for v in variants if v[0] in keep]
    results = {}
    for pass_index, order in enumerate([variants, variants[::-1]]):
        for label, make in order:
            tool = make()
            if isinstance(tool, WU):
                tool.restart_helper()
            for t in targets:
                cases = tool.cases(t)
                for r in range(a.rounds):
                    for name, fn in cases:
                        s = fn()
                        results.setdefault((t["name"], name, label), []).append(s)
            print(f"pass {pass_index + 1}: {label} done", file=sys.stderr, flush=True)

    rows = []
    for (window, case, label), samples in results.items():
        times = sorted(s.ms for s in samples if s.ok)
        nodes = [s.nodes for s in samples if s.nodes is not None]
        rows.append({
            "window": window, "case": case, "tool": label,
            "min_ms": round(times[0]) if times else None,
            "p50_ms": round(statistics.median(times)) if times else None,
            "ok": f"{sum(s.ok for s in samples)}/{len(samples)}",
            "kb": round(statistics.median(s.bytes for s in samples) / 1024, 1),
            "nodes": int(statistics.median(nodes)) if nodes else None,
        })
    json.dump(rows, open(a.out, "w", encoding="utf-8"), indent=1)

    labels = [v[0] for v in variants]
    by = {}
    for r in rows:
        by.setdefault((r["window"], r["case"]), {})[r["tool"]] = r
    print("| Window | Case | " + " | ".join(labels) + " |")
    print("|---|---|" + "---|" * len(labels))
    for (window, case), tools in by.items():
        cells = []
        for label in labels:
            r = tools.get(label)
            cells.append(f"{r['min_ms']} / {r['p50_ms']} ({r['ok']}, {r['kb']} KB)" if r else "-")
        print(f"| {window} | {case} | " + " | ".join(cells) + " |")


if __name__ == "__main__":
    main()
