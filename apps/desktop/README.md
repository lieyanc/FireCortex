# apps/desktop

> **Support scope:** FireCortex Desktop is supported in **remote-server mode only** (connecting to a self-hosted FireCortex server). It does not bundle a local Go/SQLite sidecar and requires neither Go nor CGO to build. Stable `v*.*.*` releases automatically dispatch `Desktop Release` to build and attach desktop packages; manual `workflow_dispatch` is also supported.

Tauri 2 shell around the `@firecortex/web` build. It contains **no business logic**:
windows, tray, deep links, auto-update and OS keychain access only. If a feature
needs code here, the right fix is a missing abstraction in `packages/core` or
`apps/web` — see [docs/ARCHITECTURE.md](../../docs/ARCHITECTURE.md).

## How the pieces fit

```
apps/web (Next.js static export — the same bundle the Go server serves)
        │  frontendDist: ../../web/out
        ▼
apps/desktop/src-tauri (Rust)
        ├── tabs               src/tabs.rs       one window, one webview per server, tab strip webview
        ├── session            src/session.rs    server choice + keychain + refresh (token never exposed to JS)
        ├── proxy              src/proxy.rs      network proxy: OS settings or the app's own, for shell and webviews
        ├── tray               src/tray.rs       show / quit, left click restores the window
        ├── menu               src/menu.rs       macOS menu bar: File ▸ Close Tab ⌘W / Close Window ⇧⌘W
        ├── OAuth loopback     src/oauth_loopback.rs  RFC 8252 receiver on 127.0.0.1:<ephemeral>
        ├── downloads          src/downloads.rs  `<a download>` → Downloads folder, result reported to the tab
        ├── updater            tauri-plugin-updater + apps/web/shared/platform/desktop-updater.ts
        └── CSP                no inline/remote scripts; network open to http(s) (user picks the server)
```

The desktop app loads the exact web bundle; there is no desktop-specific build.
At runtime the page detects the shell through `window.isTauri` and shows the
first-run server setup screen. Token delivery is decided server-side: requests
carrying `X-Client-Platform: desktop` from a non-web Origin get the refresh
token in the response body instead of a `SameSite` cookie, which a cross-origin
webview would never receive.

No request leaves a tab before its server is known: the webview origin
(`tauri.localhost`) answers every unknown path with `index.html`, so a request
addressed to it would never reach an API. Branding is therefore loaded from the
tab's server once it is bound; an unbound tab keeps the built-in branding.

## Tabs: one webview per server

The window is a plain `Window` with child webviews (Tauri `unstable`):

```
┌─ chrome (38px) ── /desktop/tabs ── tab strip, drag region ───────────┐
├─ tab-1 ── /chat ── bound to https://chat.example.com                 │
├─ tab-2 ── /chat ── bound to https://staging.example.com  (hidden)    │
└─ tab-3 ── /chat ── unbound → setup screen                (hidden)    ┘
```

Tabs enter at `/chat`: under the static export the root route is only a
`redirect("/chat")` payload, which a webview cannot render.

The strip is the title bar. macOS overlays the native bar so the traffic lights
sit on the strip; Windows is undecorated and the strip draws the caption buttons
(tao keeps edge resizing, the shadow and the maximized inset); Linux keeps the
system bar, since undecorated GTK varies by window manager. On Windows and macOS
close hides the window and the app stays behind the tray — a second launch
focuses the running copy (`tauri-plugin-single-instance`). On Linux close quits: a tray icon is not
guaranteed there, so a hidden window could be unreachable.

Because one window holds every tab, macOS gets its own menu bar
(`src/menu.rs`): Tauri's default menu binds ⌘W to Close Window, and with a close
turned into a hide a stray ⌘W would make the whole app disappear instead of
closing a tab. The menu replaces that binding the way browsers do — ⌘W closes
the tab that is showing and ⇧⌘W the window — with one deviation: the last tab is
never closed, since closing it would forget its server and delete its token. ⌘W there closes the window instead, and the item says so: with a
single tab File reads `Close Window ⌘W`, with more it reads `Close Tab ⌘W` and
adds `Close Window ⇧⌘W`. The red traffic light and the tray still close/hide the
window.

⇧⌘W needs a nudge: AppKit matches it against the shifted key equivalent `W`,
while a Tauri accelerator string only ever produces the unshifted `w`, which
would leave the item printed but inert. `menu::promote_shifted_shortcut` fixes
the installed item through AppKit. (Tauri's own `Edit ▸ Redo` ⇧⌘Z has the same
defect and is not touched here.)

Every tab is a full instance of the web app with its own DOM, caches, SSE
connections and in-memory session, so the app never has to model "several
servers at once". A tab is bound to at most one server; two tabs never share a
server (opening one that is already open activates its tab). Closing a tab
forgets that server and deletes its refresh token. Bound tabs are restored on
launch from `tabs.json` (any legacy `local` entries from older profiles are
dropped and their stored credentials removed).

Session commands resolve their server from the *calling webview's label*, so a
tab can only ever touch its own credential. `build.rs` declares the app ACL
manifest, so every app command must be granted per webview in
`capabilities/`: content tabs get the session commands, the strip gets
`tabs_*`, and neither can call the other's (Tauri only enforces the ACL on app
commands once a manifest exists). Webviews may only navigate within the app;
external links and `window.open` are routed to the system browser. The `BroadcastChannel` the browser
build uses to sync tokens between same-origin documents is disabled on desktop
for the same reason — tabs share an origin but not a server.

The strip is a page of the same web build (`/desktop/tabs`) under its own root
layout (`app/(shell)`), which mounts only the theme and the strip's strings —
no branding fetch, no app i18n bundles, no guards.

Tabs are lazy: on launch only the active tab gets a webview, the others are
created on first click. A tab hidden for 30 minutes has its webview discarded
(browser "memory saver"); it reloads on the next activation and signs back in
from the keychain. Tabs can be reordered by dragging; order is persisted.

`pnpm dev` runs with `tauri.dev.conf.json`, which swaps the identifier to
`com.firecortex.desktop.dev`: the dev build gets its own config dir and keychain
service, so it never disturbs an installed copy.

Storage (localStorage) is shared across tabs on purpose: it holds UI
preferences such as theme and fonts, which should follow the user, not the
server.

Downloads (`<a download>`: images, exports, file previews) go straight to the
user's Downloads folder; wry appends ` (1)`, ` (2)` … rather than overwrite.
Without a download handler WKWebView would cancel them silently. No webview
shows download UI, so the shell emits `downloads:finished` to the tab that
started one and the page shows a toast (`desktop-download-notifier.tsx`). The
webview gets no filesystem permission for this: Rust picks the path.

## Remote-server connection model

FireCortex Desktop connects to one or more self-hosted FireCortex servers (one
tab per server). It does not ship a bundled local server or SQLite sidecar:

- **Server:** any reachable FireCortex server URL (`http://` or `https://`),
  entered on the first-run setup screen (or prefilled/locked by Windows
  enterprise policy).
- **Accounts & data:** managed entirely by the connected server; capabilities
  and feature flags are declared by `GET /api/v1/capabilities`.
- **Keychain key:** `refresh-token:<origin>`, stored per server in the OS
  credential store.

## Network proxy

`src/proxy.rs` decides one proxy per launch and hands it to everything that
leaves the machine. Settings › Network (desktop only) offers three modes:
follow the system (default), a manual `http://` or `socks5://` proxy with an
optional sign-in, or no proxy.

| Consumer | How it gets the proxy |
| --- | --- |
| Shell requests (session refresh, update checks) | `proxy::apply` on each reqwest client. Loopback origins are always direct. |
| Webviews | Windows: WebView2 `--proxy-server` / `--proxy-bypass-list` / `--no-proxy-server`. macOS and Linux follow the OS: a per-webview proxy needs macOS 14, and Tauri gives WebKitGTK no bypass list. |

Where the proxy comes from, highest first: enterprise policy (`ProxyMode` and
friends, below), the app setting, the process's `HTTP(S)_PROXY` / `ALL_PROXY`,
then the OS — Windows through `WinHttpGetIEProxyConfigForCurrentUser`, macOS
through the dynamic store (`SCDynamicStoreCopyProxies`); Linux has no OS source
besides the environment.

The OS bypass lists are translated, because reqwest reads `NO_PROXY` syntax
only: `127.*` / `192.168.*` become CIDR blocks, `*.corp` becomes `.corp`,
macOS's `169.254/16` is padded. `<local>` (dotless host names) has no equivalent
and is dropped. Loopback is always added.

Deliberate limits:

- **PAC and WPAD are not applied** to the shell's own HTTP requests: there is
  no proxy address to hand over. The settings page says so; the webviews still
  use the script, through the OS.
- **An unreachable proxy is not bypassed.** Requests fail as they do in the
  system browser; silently connecting directly would route around a proxy the
  user or the administrator chose.
- **The webviews never get proxy credentials.** Neither engine takes them up
  front and Tauri forwards no sign-in prompt, so a manual proxy that needs a
  password leaves the window on the OS settings; the shell's own requests use
  the password.
- **Changes apply at the next launch.** The webviews take their proxy when they
  are created; the page shows "restart required" and a button.

The setting is stored in `network.json` next to `tabs.json`; the password in the
keychain (see [Keychain entries](#keychain-entries)).

## Credential model

The browser build keeps the refresh token in an HttpOnly cookie, so page script
cannot read it. The desktop shell reproduces that property in Rust:

| | Browser | Desktop |
| --- | --- | --- |
| Access token | JS memory | JS memory |
| Refresh token at rest | HttpOnly cookie | OS keychain, account `refresh-token:<origin>` |
| Who sends it | browser, to the cookie's origin | `session.rs`, to the pinned origin only |
| Readable by page script | no | no — `store_session` is write-once, there is no read command |
| Server address | page origin | bound per tab, persisted in `tabs.json`; closing the tab drops that session |

The shell never routes a request to a loopback server through a proxy, so
refresh tokens targeting a local development server stay on the machine.

The webview holds the refresh token exactly once, in the login response, and
hands it to `store_session` immediately. After that the only session commands
are `refresh_session` (returns an access token), `clear_session` and
`leave_server`. A script injected into the page — from a malicious message,
a shared conversation, another user's profile — therefore gets the same
short-lived access it would get in a browser, not the long-lived credential,
and cannot redirect the credential to a server it controls.

## Prerequisites

- Rust toolchain (`rustup`) with a recent stable
- Platform dependencies for Tauri 2 (WebKitGTK on Linux, WebView2 on Windows,
  Xcode command line tools on macOS)
- `pnpm install` from the repository root

## Development

```bash
# from the repository root
pnpm --filter @firecortex/desktop dev
```

`beforeDevCommand` (`scripts/dev-web.mjs`) starts `next dev` on port 3000, or reuses a
web dev server that is already running there (e.g. from `pnpm dev:web`), and the shell
loads it. The Go
server must already be reachable at the address you enter on the setup screen
(default `http://127.0.0.1:8080`).

## Build

```bash
pnpm --filter @firecortex/desktop build
```

Artifacts land in `apps/desktop/src-tauri/target/release/bundle/`. The Windows
portable zip is assembled afterwards by `pnpm --filter @firecortex/desktop package:portable`
(see [Windows distribution](#windows-distribution)).

A local Windows build of a prerelease version (`x.y.z-beta.n`) fails at the MSI
step, because MSI versions must be numeric. Build the setup only
(`pnpm tauri build --bundles nsis`) or pass the config CI uses:
`node scripts/release-config.mjs --channel beta` prints the `--config` argument.

The macOS app requires macOS 12 (`bundle.macOS.minimumSystemVersion`, which
becomes `LSMinimumSystemVersion`). Tauri builds the Rust binary for that
minimum deployment target, and `verify-macos-bundle.sh` checks the shipped
executable against it.

Building is not enough to ship: macOS packages must be **notarized** and Windows
packages **code-signed**, otherwise users cannot install them. Signing keys and
the updater key live only in CI secrets — never in this repository.

## Third-party sign-in (OAuth / OIDC)

The webview cannot receive a provider redirect, so the desktop app follows the
RFC 8252 native-app flow using the server's provider-auth bridge:

1. The shell binds an ephemeral port on `127.0.0.1` and hands the web app
   `http://127.0.0.1:<port>/oauth/callback` as the redirect URI.
2. The web app starts the bridge with client id `com.firecortex.desktop`; the
   server validates that the redirect is a loopback address with a port and
   nothing else, then returns the provider authorization URL.
3. The authorization URL opens in the **system browser** (the user's existing
   provider session is reused; the webview never sees provider credentials).
4. The provider redirects to the server callback, which issues a one-time FireCortex
   grant and redirects the browser to the loopback URI.
5. The shell answers that request with a "you can close this tab" page and
   emits the callback URL to the webview, which navigates to `/auth/callback`
   and exchanges the grant with its PKCE verifier — the same code path the
   browser build uses.

Requirements on the server side: `PUBLIC_API_BASE_URL` set (this enables the
bridge) and the instance callback registered with each provider — see the
"OAuth callbacks for Web, App, and Desktop" section of the root README. No
custom URL scheme is registered and nothing is added to the provider allowlist
for the desktop app.

## Auto-update

The updater is configured in `tauri.conf.json` (`plugins.updater`). How a build
updates depends on how it was distributed (`src-tauri/src/distribution.rs`):

| Build | Update mode | Behavior |
| --- | --- | --- |
| NSIS setup (macOS/Linux bundles too) | `auto` | Checks on launch and every four hours (`desktop-update-notifier.tsx`) and offers the update in a toast; nothing downloads until the user accepts. |
| Portable zip | `notify` | Same checks, but only announces the new version and links to the download page; it never replaces its own files. |
| MSI | `disabled` | No checks. IT deploys new versions. |
| Policy `DisableAutoUpdate=1`, dev builds | `disabled` | No checks. |

The JS updater permission is not in the static capability: it is granted at
runtime only in `auto` mode, so a portable or MSI build cannot install updates
even if the web layer asked.

Release assets are staged by `scripts/collect-release-assets.mjs` as
`FireCortex-<version>-<os>-<arch>[-setup|-updater].<ext>`, e.g.
`FireCortex-0.4.4-beta.1-macos-arm64.dmg`, `-linux-x64.AppImage`,
`-windows-x64-setup.exe`, `-macos-arm64-updater.tar.gz`. Every target must have
exactly one of each required package and its updater signature before staging.
After all targets pass, `scripts/publish-desktop-release.mjs` assembles
`latest.json` and `SHA256SUMS` from those final files and uploads them to one
draft. It preserves the updater platform keys used by existing installs,
resumes a single existing draft and refuses to replace a published release.
Only the artifact bytes are signed, so staging under a new name does not change
their signatures. `pnpm test` covers staging, updater compatibility and hashes.

Release flow: merging a `VERSION` bump into `main` creates the tag
`v<VERSION>` (`release-tag.yml`), which builds every target and opens a
**draft** GitHub Release with the installers and `latest.json` containing their signatures.
Publishing the draft is the step that ships the update; until then existing
installs see nothing. Pushing the tag by hand does the same.

Before tagging, update `VERSION`, run `make version` and commit the synchronized
version files, including the desktop root package entry in `Cargo.lock` and the
generated Swagger/TypeScript API contracts. This uses the same generation
pipeline as `pnpm api:generate`; Go and workspace JS dependencies are required.
`make version-check` runs the full API contract drift check as well as version
checks, so stale manifests, lock metadata or generated API versions fail before
release. Dependency versions stay pinned.

Windows assets: `-windows-x64-setup.exe`, `-windows-x64.msi`,
`-windows-x64-portable.zip` (and `-portable-offline.zip` when built). Every
release also carries `SHA256SUMS` and GitHub build provenance attestations.

### Release workflows

[`desktop-release.yml`](../../.github/workflows/desktop-release.yml) coordinates
the following reusable workflows. Each native job receives the same immutable
source commit and channel; platform-specific installation, signing and
verification live in that platform's file.

| Workflow | Responsibility |
| --- | --- |
| [`desktop-prepare.yml`](../../.github/workflows/desktop-prepare.yml) | Verify the run's source commit and tag, check version consistency, and build the frontend once. |
| [`desktop-macos.yml`](../../.github/workflows/desktop-macos.yml) | Build arm64 natively and cross-compile x64 on ARM `macos-26` runners; sign the app, create the final DMG and updater archive, then verify both. |
| [`desktop-linux.yml`](../../.github/workflows/desktop-linux.yml) | Build the x64 desktop executable, bundle deb/AppImage and verify the Wayland library exclusion. |
| [`desktop-windows.yml`](../../.github/workflows/desktop-windows.yml) | Build the x64 desktop executable, sign and verify NSIS/MSI/portable packages, and attest their provenance. |

The four native jobs run in parallel after preparation. Shared toolchain/cache
setup lives in [the desktop-setup action](../../.github/actions/desktop-setup/action.yml).
The frontend is downloaded as a workflow artifact; native jobs disable Tauri's
frontend build hook, so they never rebuild the shared frontend, and no Go or
CGO toolchain is needed.
Compilation, bundling, notarization and verification have separate steps. Rust
`--timings` reports are retained as `desktop-timings-*` artifacts for seven days.
The shipped Rust optimization profile, including fat LTO, is unchanged.

Release-tag runs (`release.yml`) dispatch `desktop-release.yml` on the **release tag**. Every
job checks out the run's immutable `github.sha`; source inputs are verified
against that commit and cannot select code from a different ref. Tag code writes
only to its own cache scope, keeping the default branch and other tags isolated.
Compatible caches from default-branch CI can still be restored, and reruns of
the same tag reuse its caches. Successive tags cannot restore caches written by
each other, so their first native builds may need recompilation. Publication
verifies the tag has not moved, and Windows provenance records the matching
workflow and desktop source commits. Rust is pinned in the setup action, and
Rust cache keys include the target triple so the two macOS targets sharing ARM
runners cannot overwrite one another's cache. Dependency changes or cache
eviction can also cause rebuilds.

To build an existing tag containing these workflows and scripts:

```bash
gh workflow run desktop-release.yml --ref refs/tags/v1.2.3
```

The tag must contain the reusable workflows, build scripts and setup action;
rerun the original workflow for historical tags. A manual branch run with an
empty `release_tag` only uploads workflow artifacts; a manual tag run creates a
draft. Setting `release_tag` on a branch dispatches the workflow onto that tag
without building it in the branch's context. On a tag run, a nonempty input must
match the current tag. A release run creates the draft only after all four
targets succeed; publishing the draft remains a separate human step. Failed
jobs can be retried while the shared artifacts are retained (seven days).

### Channels

| Tag | Channel | What the app polls |
| --- | --- | --- |
| `v1.2.3` | stable | `releases/latest/download/latest.json` — GitHub resolves this to the newest published non-prerelease |
| `v1.2.3-beta.1` | beta | `releases/download/desktop-beta/latest.json` — a rolling tag refreshed by `desktop-channel.yml` whenever a prerelease is published |

A beta build is the same code with a different updater endpoint, injected at
build time via `tauri build --config`. Both channels are signed with the same
updater key: a channel is a distribution lane, not a trust boundary. Beta
installs keep receiving betas; to move a user back to stable, have them install
a stable build. Stable installs never see prereleases.

## Windows distribution

One signed binary set ships in three forms:

| Form | For | Install scope | Updates |
| --- | --- | --- | --- |
| `…-windows-x64-setup.exe` (NSIS) | Most users | Per user, no admin prompt (`installMode: currentUser`); English/Chinese by system language | `auto` |
| `…-windows-x64.msi` (WiX) | IT deployment (Intune, SCCM, Group Policy) | Per machine | `disabled`, IT manages versions |
| `…-windows-x64-portable.zip` | Restricted machines, USB drives, trying the app | None: unzip and run | `notify` |

Silent installs: `FireCortex-<v>-windows-x64-setup.exe /S` (per user) or
`msiexec /i FireCortex-<v>-windows-x64.msi /qn` (per machine). The MSI
`upgradeCode` in `tauri.conf.json` identifies the product family for upgrades and
deployment assignments and must never change. MSI versions are numeric, so
`x.y.z-beta.n` is built as `x.y.z.n` (`scripts/release-config.mjs`).

### Portable edition

```text
FireCortex/
  FireCortex.exe            signed app
  portable                  marker: its presence switches the app to portable mode
  README.txt
  data/                     created on first run
    instance-id             identity of this copy
    webview/  ...           WebView2 profile, tabs, network proxy settings
```

- **Detection**: the `portable` marker next to the exe decides; the same binary
  runs as an installed app without it. Kind precedence is marker → debug build
  → Tauri bundle type.
- **Data stays in the folder.** All app-owned paths go through
  `src-tauri/src/paths.rs`, including the WebView2 profile. If `data/` is not
  writable the app shows an error and exits; it never falls back to `%APPDATA%`.
  Temp files WebView2 itself writes to the system `%TEMP%` are the one
  exception.
- **Credentials stay in Windows Credential Manager**, never in `data/`, so a lost
  USB drive does not leak sign-ins; moving to another machine means signing in
  again. Each copy runs under `com.firecortex.desktop.portable-<instance-id>`,
  which keeps its credentials and single-instance lock separate from an installed
  copy and from other portable copies. The id survives moving the folder, a new
  drive letter, and updating by extracting over the old folder. Sign out before
  deleting the folder to remove the stored credential.
- **WebView2**: the online zip uses the system runtime (built into Windows 10
  21H1+ and 11, missing on Windows Server by default); without one the app shows
  a download link instead of crashing. The `-portable-offline.zip` adds a Fixed
  Version runtime in `webview2/`, used automatically. CI builds it only when the
  repository variable `WEBVIEW2_FIXED_RUNTIME_URL` is set (optionally pinned with
  `WEBVIEW2_FIXED_RUNTIME_SHA256`); the runtime's Microsoft signature is verified
  first.
- **Why a zip, not a single exe**: a self-extracting exe unpacks to `%TEMP%` on
  every launch, which AppLocker/WDAC rules commonly block and antivirus often
  flags; a zip runs the signed binaries in place.

### Enterprise policy

The app reads policy once at startup from
`HKLM\SOFTWARE\Policies\FireCortex\Chat` and `HKCU\SOFTWARE\Policies\FireCortex\Chat`; a
value set under HKLM wins. Rust enforces it (`src-tauri/src/policy.rs`); the UI
only reflects it.

| Value | Type | Effect |
| --- | --- | --- |
| `DefaultServerUrl` | REG_SZ | Prefilled server address on the setup screen. |
| `LockServerUrl` | REG_DWORD `1` | Only `DefaultServerUrl` may be used (requires a valid `DefaultServerUrl`). Saved tabs for other servers are dropped with their credentials. |
| `DisableLocalMode` | REG_DWORD `1` | No-op (retained in ADMX for backwards compatibility; FireCortex Desktop is always remote-only). |
| `DisableAutoUpdate` | REG_DWORD `1` | No update checks in any form. |
| `ProxyMode` | REG_SZ `system` / `direct` / `fixed_servers` | Replaces the user's proxy choice ([Network proxy](#network-proxy)). The hive that sets it decides `ProxyServer` and `ProxyBypassList` too. |
| `ProxyServer` | REG_SZ | `host:port`, `http://host:port` or `socks5://host:port`, for `fixed_servers`. No credentials; an invalid value makes the policy ignored. |
| `ProxyBypassList` | REG_SZ | Hosts, `*.example.com`, addresses and ranges that skip the proxy, `;` or `,` separated. |

Group Policy templates and an example `.reg` live in
[`deploy/windows/`](../../deploy/windows/README.md).

## Build times and sizes

What a local `pnpm build` costs once caches are warm, and where it goes:

| Step | Unchanged | Changed | Notes |
| --- | --- | --- | --- |
| Web (`next build`) | **0.3 s** | ~35 s | runs through `turbo`, so an untouched frontend is a cache hit |
| Rust app crate | ~1 s | ~60 s | recompiles whenever `out/` changed (assets are embedded); fat LTO, the shipped profile |
| `.app` | ~1 s | | |
| `.dmg` | ~25 s | | `hdiutil` + Finder layout; `scripts/package-dmg.sh` then re-encodes with lzfse and (when signing is configured) re-signs and notarizes |

`pnpm build:signed` uses the local Tauri DMG/repackaging path above. CI uses
`scripts/package-macos.sh`: it creates the `.app` first, completes its signing
and notarization, then creates an ULFO DMG directly from that app. It avoids
building a compressed DMG only to decompress, replace the app and recompress it.
The updater tarball is created from the same final app and then signed with the
updater key. Both packages go through `verify-macos-bundle.sh` before staging.
Use `pnpm build:app` while iterating: it relaxes LTO (~17 s link) and stops at the `.app`.

## Icons

`src-tauri/icons/mark.png` is the bare monochrome FireCortex flame mark (black on transparent). From it:
`source.png` is the 1024² app icon on the macOS grid (824px rounded square in
the cool-light `#f7f9fb` theme surface with the `#018eee` blue-flame mark, transparent margin) —
regenerate every platform size with
`pnpm tauri icon src-tauri/icons/source.png --output src-tauri/icons` and
delete the generated `android/` and `ios/` folders. `tray.png` is the 44px
monochrome menu-bar glyph (macOS template image); Windows/Linux trays show the
app icon.

## Signing

| Platform | What is needed | CI secrets |
| --- | --- | --- |
| macOS | Developer ID Application certificate (.p12) + Apple ID app-specific password for notarization; identity and team id are derived from the certificate (`scripts/apple-signing-env.sh`) | `APPLE_CERTIFICATE`, `APPLE_CERTIFICATE_PASSWORD`, `APPLE_ID`, `APPLE_PASSWORD` |
| macOS, external service | Alternative app/DMG signing and notarization provider | `SIGN_AND_NOTARIZE_SCRIPT_URL`, `SIGN_AND_NOTARIZE_BASE_URL`, `SIGN_AND_NOTARIZE_API_TOKEN`, `SIGN_AND_NOTARIZE_HELPER_URL` |
| Windows | Code-signing certificate (.pfx) | `WINDOWS_CERTIFICATE`, `WINDOWS_CERTIFICATE_PASSWORD` |
| All | Updater keypair (`tauri signer generate`); the private key is kept git-ignored in `deploy/secrets/` (see its README) | `TAURI_SIGNING_PRIVATE_KEY`, `TAURI_SIGNING_PRIVATE_KEY_PASSWORD` |

`tauri.conf.json` already enables the hardened runtime on macOS and SHA-256 +
RFC 3161 timestamping on Windows. CI imports the Windows certificate into the
runner's store and passes its thumbprint as `bundle.windows.certificateThumbprint`
through `--config` (tauri-cli has no environment variable for it). The bundler
signs the exe inside each installer; the portable exe is signed separately with
signtool. A CI step then requires a valid Authenticode signature on the setup,
MSI, and portable exe whenever a certificate is configured.

CI chooses the external macOS provider when all four `SIGN_AND_NOTARIZE_*`
secrets are set, even if Apple credentials also exist. Tauri only ad-hoc signs
in that mode; the external service signs/notarizes the app once and then the
final DMG. The updater archive is created afterwards, from the signed app.
A partial external configuration fails immediately. Without the external
provider, the existing `APPLE_*` credentials enable Tauri app signing and
notarization; DMG signing imports its own temporary keychain because Tauri
removes its keychain when app bundling finishes.

On macOS, `scripts/verify-macos-bundle.sh` checks the app as it ships, inside the
final DMG and inside the updater archive, before CI uploads the DMG: the app
binary must be built for the leg's architecture and must not require a newer
macOS than the app declares, and the bundle must pass
`codesign --verify --deep --strict`. With a Developer ID it also requires the
certificate's team id on the app and the DMG, a stapled notarization ticket on
both, and a Gatekeeper assessment of `Notarized Developer ID`.
The DMG signer is checked against Apple's Developer ID Application certificate
chain and its certificate's Team ID (`subject.OU`), because a signed disk image
can report `TeamIdentifier=not set` in `codesign -dv` output.
`pnpm build:signed` runs the same check.

Only the updater key is mandatory. Platform signing switches on by itself once
its secrets exist; CI fails when only some of the macOS secrets are set instead
of quietly building an unnotarized app. Without a Developer ID the macOS app is
signed **ad-hoc** (`APPLE_SIGNING_IDENTITY=-`); Tauri would otherwise skip
`codesign` and leave only the linker's signature, which macOS reports as
*damaged* on Apple silicon. Ad-hoc builds still need the user to allow the app
under System Settings → Privacy & Security and are for internal testing, not
public releases. Signing keys never enter the repository.

To obtain the .p12 on macOS: Keychain Access → My Certificates → right-click the
"Developer ID Application" cert → Export → base64 it (`base64 -i cert.p12 | pbcopy`).

## Keychain entries

| Field | Value |
| --- | --- |
| Service | `com.firecortex.desktop` |
| Account | `refresh-token:<origin>` (e.g. `refresh-token:https://chat.example.com`) |
| Account | `proxy-password`: the manual proxy's password, only while a username is set |

One refresh token per origin; switching servers deletes the previous entry
rather than leaving dormant credentials behind. The proxy password is removed
the same way when the username is cleared. A Windows portable copy uses the service
`com.firecortex.desktop.portable-<instance-id>` instead (see
[Portable edition](#portable-edition)).
