# 01: Register the native host where Brave looks for it

Status: done

**Evidence.** After the extension was loaded in Brave:

```
winuse browser ext status brave
the winuse extension is loaded in Brave but has not connected to winuse
hint: run winuse browser ext install brave, then tell the user: in Brave, open brave://extensions and click the reload icon on "winuse"; ...
```

Re-running `ext install brave` and reloading changed nothing. The agent then
checked the registry:

```
HKCU:\Software\Google\Chrome\NativeMessagingHosts\com.hoangvu12.winuse ->
HKCU:\Software\BraveSoftware\Brave-Browser\NativeMessagingHosts\com.hoangvu12.winuse -> C:\Users\HP MEDIA\AppData\Local\winuse\browser\native-host\com.hoangvu12.winuse.json
```

ran `winuse browser ext install chrome` (which writes Chrome's key), reloaded,
and got `ok brave connected through the winuse extension` within 4 s. Running
the host by hand (a hello piped into `winuse.exe chrome-extension://...`) had
already shown the host itself works.

**Root cause.** `internal/browser/extension.go` `hostKeys` maps `brave` to
`Software\BraveSoftware\Brave-Browser\NativeMessagingHosts\` only. Brave on
Windows reads Chrome's `Software\Google\Chrome\NativeMessagingHosts\` (as
KeePassXC's docs also say). `hostProblem` checks the same single key, so
`ext status` could not see the problem either.

**Fix.** Each browser has a list of keys: its own first, then Chrome's for the
Chromium forks that may read it (Brave, Vivaldi, Opera, Chromium). Install
writes all of them; `hostProblem` wants all of them. Registering Chrome's key
for our host is harmless: the manifest only allows the winuse extension's id.

## Comments

Done on branch browser-ux: `internal/browser/extension.go` (`hostKeys` per browser, `HostKeys`, install writes all, `hostProblem` wants all). Test: `TestExtInstallRegistersBraveUnderChromesKeyToo`. Live check pending (see the final report).
