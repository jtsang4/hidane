## Install

| System | Download | Notes |
|---|---|---|
| macOS 12+ (Apple Silicon and Intel) | `hidane-<version>-macos-universal.dmg` (or `.zip`) | Drag **Hidane** to Applications. Builds without an Apple Developer ID are ad-hoc signed: on first launch, right-click → Open, or run `xattr -dr com.apple.quarantine /Applications/Hidane.app`. |
| Windows 10/11 (x64, ARM64) | `hidane-<version>-windows-<arch>.zip` | Unzip anywhere and run `hidane.exe`. Needs the Microsoft Edge WebView2 runtime (built into Windows 11 and current Windows 10). `hidane-cli.exe` is the same program for terminals (`hidane-cli chat …`). |
| Linux (x64, ARM64) | `hidane-<version>-linux-<arch>.deb` or `.tar.gz` | Needs GTK 4 and WebKitGTK 6.0 (Ubuntu 24.04+, Debian 13+, Fedora 40+). `sudo apt install ./hidane-<version>-linux-<arch>.deb` pulls them in; for the tarball install `libgtk-4-1` and `libwebkitgtk-6.0-4` (or your distribution's equivalents). |

hidane drives the agent CLIs installed on your machine — install and log in to at least one of [Claude Code](https://docs.anthropic.com/en/docs/claude-code), [Codex](https://github.com/openai/codex) or [pi](https://github.com/badlogic/pi-mono), then pick it in Settings → Roles.

`SHA256SUMS` lists the checksum of every file below (`shasum -a 256 -c SHA256SUMS`).
