# 08: Skill, install and retiring use-browser

**What to build:**

- **The winuse skill teaches browser Windows.** Load writing-for-agents first. Remove the skill test's ban on mentioning use-browser, and replace it with a migration line.
- **The README and the usage text** cover the new commands.
- **`install.ps1`** notes an installed use-browser and how to remove it.
- **Draft the use-browser README deprecation notice** in this folder, for the user to publish.
- **License note** for the ported code.

See spec: "Retiring use-browser".

**Blocked by:** 05, 06, 07

**Status:** done

- [x] Skill tests green; SKILL.md stays plain ASCII and follows the unslop rules
- [x] Nothing is pushed to the use-browser repo by an agent

## Comments

### 2026-11-02: landed

**Scope change from the user (mid-ticket).** use-browser is mentioned nowhere users or agents read. So, against the ticket text above:
- no migration line in SKILL.md, and the skill test's ban on mentioning use-browser stays (`TestSkillNeverMentionsUseBrowser`, unchanged);
- no use-browser note in `install.ps1` (the file is unchanged);
- no deprecation notice drafted (`use-browser-deprecation.md` was never written);
- no use-browser mention in the README;
- `third_party/use-browser/NOTICE` stays as is: it is the license attribution for the ported code. It already covers the extension, the native host and extension.go, and states the same-author relicensing under winuse's MIT license.

**Open for the user:** whether to add a LICENSE file to the use-browser repo. That is the user's call; nothing in winuse depends on it.

**What changed.**
- `skills/winuse/SKILL.md`:
  - The description now also triggers on browsing, opening, reading or automating a website in a Chromium browser.
  - 06's "Browser pages" stub is replaced by "Browser Windows": one Window State (frame plus Page), Page Elements by Element Index, what the fallback note means; "Get a Browser Connection" (`browser status` first, the pin, the extension route with the steps to relay word for word and `ext status` after the user replies, `launch`, `clone` with `--close-source` asked first, `connect` only with the user at the keyboard, `stop`, the user turning winuse off); "Act on a Page" (navigate instead of the address bar, waits, set-value for lists and check boxes, actionability refusals, staleness, dialogs, file choosers and upload, downloads on launched or cloned browsers only, eval and PowerShell quoting); "Tabs" (which are Headless and which Headed, act in your own Tab, the last Tab refusal).
  - Small fixes elsewhere: the Headless list, the keyboard routes (Page focus is Headless), Posted Keys no longer names browsers, Batch steps for Page and tab commands, the request budgets, and the command reference.
  - Everything stays in SKILL.md. A reference file next to it would ship through `npx skills add`, but `TestSkillShipsAlone` pins the folder to SKILL.md and the tasksuite copies only SKILL.md, so splitting would need both changed. The file is about 27 KB, under the 64 KB test limit.
- `README.md`: the tagline mentions websites, a short "Browsers" section (what works in the background, the four ways to connect, the UI Automation fallback), and five rows in the commands table. Structure and GIF unchanged. At the user's request the whole README, and then the browser parts of SKILL.md, went through pstack's unslop skill (backnotprop/pstack @ 25abb38): no dashes or parenthetical asides in prose, colons only before lists and examples, no label-plus-colon bullets, active voice, plain words.
- `internal/cli/cli.go` usage: set-value names a Page list's option; tab switch says the Page commands act on the active Tab. The browser, tab and page commands were already documented by 01 to 07.

**Checks.** `go build ./...`, `go vet ./...`, `go vet -tags e2e ./test/e2e` and `go test ./...` are green. SKILL.md and README.md are plain ASCII with LF endings; no em or en dashes.

**Risk carried from 02.** A browser running the winuse extension keeps `winuse.exe` open as its native host. `install.ps1` already stops every winuse process before replacing the binary, which also ends the host; the extension reconnects when the browser next starts the host. Not measured live.
