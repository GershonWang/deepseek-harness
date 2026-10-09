# DeepSeek Harness official desktop client as a Linglong bundle

English | [中文](README.zh.md)

`apps/desktop-linglong` packages the upstream Electron Desktop client ([`apps/desktop`](../desktop)) as a Linglong bundle for Deepin 25. It is the official-client counterpart of [`apps/desktop-launcher`](../desktop-launcher), which packages the Go + Wails thin shell embedding `dsh web`. The two bundles are independent and install side by side under different application ids.

## What this module produces

One `.uab` whose command entry is `/opt/apps/com.deepseek.dsh-desktop-official/files/bin/dsh-desktop-official`, a wrapper that starts the Electron application with `--no-sandbox`.

Electron Desktop is self-contained: it ships its own Electron runtime and its own `resources/app.asar/dsh` tree, so the bundle adds no second Node runtime and no second harness tree. The unpacked Linux x64 application measures about 1.1 GB (Electron 227 MB, primary runtime 496 MB, dsh tree 331 MB), the same order as the thin-shell bundle: bundling an Electron application does not shrink the payload.

## Why the upstream packaging entry is not reused

[`package-target.ts`](../desktop/scripts/package-target.ts) binds its target type to release targets (`DesktopPackageTargetName` is mac-arm64, mac-x64, win-x64): code signing, notarization, `electron-updater`, and COS upload. Linux is not a release target, because the Linglong bundle owns distribution. Adding `linux-x64` to that type pulled the whole upload and update type chain along, measured as 12 type errors, so `prepare-offline.sh` calls the underlying packaging steps directly in the order that `package-target.ts` uses.

## Building

Run the host-side preparation first, then assemble in the Linglong container.

```sh
sh apps/desktop-linglong/prepare-offline.sh
ll-builder build -f apps/desktop-linglong/linglong.yaml
ll-builder export --ref com.deepseek.dsh-desktop-official
```

`prepare-offline.sh` builds the repository, packs the dsh and vendor package sets, prepares the Electron runtime and the primary runtime, prepares the dsh tree, and runs `electron-builder --linux --x64 --dir` into `apps/desktop-linglong/stage/`. The container build only copies that tree, installs the icons and desktop entry, and writes the launcher wrapper.

## Upstream changes this module depends on

`apps/desktop` gained a Linux platform path in the same change.

| File | Change |
|---|---|
| [`desktop-build-paths.mjs`](../desktop/scripts/desktop-build-paths.mjs) | `linux-x64` joins `SUPPORTED_TARGETS`; `desktopTargetPlatform` returns `linux` |
| [`desktop-build-paths.d.mts`](../desktop/scripts/desktop-build-paths.d.mts) | `DesktopBuildTarget` is the release targets plus `linux-x64` |
| [`prepare-runtime.ts`](../desktop/scripts/prepare-runtime.ts) | platform derivation and the Electron executable path know `linux` |
| [`prepare-dsh.ts`](../desktop/scripts/prepare-dsh.ts) | the Electron executable resolves from the packaging target, not the build host |
| [`prepare-cli.ts`](../desktop/scripts/prepare-cli.ts) | the POSIX `dsh` launcher is made executable on Linux |
| [`electron-builder-config.mjs`](../desktop/scripts/electron-builder-config.mjs) | the Linux target generates no updater feed |

## Known limits

- The bundle has no code signing, notarization, or in-app updater, because the Linglong container forbids privilege escalation. The Electron sandbox is therefore disabled with `--no-sandbox`.
- Office conversion selects the WASM engine on Linux, because the pinned `libreoffice-kit` declares native packages for macOS and Windows only.
- The runtime library set that Electron needs inside the container is not yet verified on a real device: the `depends` list in [`linglong.yaml`](linglong.yaml) only guarantees build-time resolution, and the base runtime supplies the runtime libraries.
