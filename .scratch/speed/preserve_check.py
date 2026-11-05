"""Sentence-level check that every sentence and code line of the old SKILL.md
(at a given commit) appears verbatim, whitespace aside, in the new core or one
of its topics. Prints the ones that do not, for a human to review."""
import glob
import os
import re
import subprocess
import sys

repo, rev = sys.argv[1], sys.argv[2]
old = subprocess.run(["git", "-C", repo, "show", f"{rev}:skills/winuse/SKILL.md"], capture_output=True).stdout.decode("utf-8")
old = old.split("\n---\n", 1)[1]

files = [os.path.join(repo, "skills", "winuse", "SKILL.md")] + sorted(glob.glob(os.path.join(repo, "skills", "winuse", "references", "*.md")))
new = "\n".join(open(f, encoding="utf-8").read() for f in files)


def norm(s):
    return re.sub(r"\s+", " ", s).strip()


new_n = norm(new)
units = []
in_code = False
para = []
for line in old.split("\n"):
    if line.strip().startswith("```"):
        if para:
            units.append(" ".join(para))
            para = []
        in_code = not in_code
        continue
    if in_code:
        if line.strip():
            units.append(line)
        continue
    if not line.strip() or line.startswith("#") or line.lstrip().startswith("- ") or re.match(r"^\s*\d+\. ", line):
        if para:
            units.append(" ".join(para))
            para = []
        if line.startswith("#"):
            units.append(line.lstrip("#").strip())
            continue
        if not line.strip():
            continue
    para.append(line.strip())
if para:
    units.append(" ".join(para))

missing = []
total = 0
for u in units:
    for sent in re.split(r"(?<=[.!?])\s+(?=[A-Z`(\"])", norm(u)):
        sent = sent.lstrip("- ").strip()
        sent = re.sub(r"^\d+\.\s+", "", sent)
        if not sent:
            continue
        total += 1
        if sent not in new_n:
            missing.append(sent)
print(f"{total} sentences and code lines checked, {len(missing)} not found verbatim:")
for m in missing:
    print("  -", m)
