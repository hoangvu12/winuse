# 10: One tab group per Agent

Status: done

**Request (user, via the multi-agent research chat).** `createTab` in
`internal/bridge/extension/background.js` put every agent tab into one blue
group titled "winuse". Make the group per Agent (ADR 0014): its title the
Agent's name, its colour the chrome.tabGroups colour closest to the Agent's
Agent Cursor colour. Reuse an Agent's group in that window; grouping stays
cosmetic.

**Done.**

- `win.TabGroupColor` (`internal/win/cursor_colors.go`): the nearest of Chrome's nine fixed colours by hue, grey for unsaturated colours. The palette maps to orange, green, purple, pink, cyan, yellow, red; the default Agent's accent to blue, which no palette colour takes.
- `Services.agentTabGroup` / `cursorSet.colorOf` (`internal/helper/cursors.go`): the title is the cursor's name (`WINUSE_AGENT`, else the Harness's program name; "winuse" for the default Agent), the colour the Agent's live cursor colour, else its `WINUSE_CURSOR_COLOR`, else the default's.
- `page.NewTab` takes a `page.TabGroup` and sends it as `winuseGroup` on `Target.createTarget`, through the extension only.
- `background.js` `createTab` validates it (`tabGroupFor`: title cut at 40 characters, colour from the fixed list, else "winuse"/blue) and reuses the window's group with that title and colour; failures stay ignored. A newer winuse reloads the extension itself (build stamp), so no user step.
- `bridgetest`: the fake groups tabs the same way and exposes `Extension.Group`.
- Tests: `TestTabGroupColor`, `TestAgentTabGroup`, `TestNewTabOnTheExtensionNamesTheWindow`, `TestTabNewOnTheExtension`, `TestExtensionFilesAreWinuses` strings.
- Docs: SKILL.md (Tabs), CONTEXT.md (Tab), README.

**Not verified live:** the group in a real browser (title, colour, reuse).
Two sessions of one Harness share a title but differ in colour, so their
groups stay apart.

## Comments
