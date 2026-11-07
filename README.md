# photos0x

<div align="center">



**Cross-platform desktop and command-line client for Google Photos with batch uploads, album organization, Apple Live Photo pairing, and unlimited storage options.**

[![Latest Release](https://img.shields.io/github/v/release/shishir0x/photos0x?color=blue&label=release)](https://github.com/shishir0x/photos0x/releases)
[![Build Status](https://img.shields.io/github/actions/workflow/status/shishir0x/photos0x/build.yml?branch=main&label=build)](https://github.com/shishir0x/photos0x/actions/workflows/build.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](go.mod)
[![Platform Support](https://img.shields.io/badge/Platforms-Windows%20%7C%20macOS%20%7C%20Linux-brightgreen)](#installation-and-downloads)

[Downloads](https://github.com/shishir0x/photos0x/releases) • [Quick Start](#quick-start) • [CLI Documentation](#command-line-documentation) • [Configuration](#configuration-and-credentials) • [Building from Source](#development-and-building-from-source) • [Report Issue](https://github.com/shishir0x/photos0x/issues)

</div>

---

> **Disclaimer**: **photos0x** (formerly gotohp) is an independent, unofficial open-source project and is **not affiliated with, maintained, authorized, or endorsed by Google LLC**. Google Photos is a registered trademark of Google LLC. Use of this application is at your own discretion and must comply with Google's Terms of Service.

---

## Table of Contents

- [Overview](#overview)
- [Key Features](#key-features)
- [Installation and Downloads](#installation-and-downloads)
  - [Prebuilt Binaries](#prebuilt-binaries)
  - [macOS Installation](#macos)
  - [Windows Installation](#windows)
  - [Linux Installation](#linux)
  - [Standalone Headless CLI](#standalone-headless-cli)
- [Quick Start](#quick-start)
  - [1. Connect Account](#1-connect-account)
  - [2. Upload Photos & Videos](#2-upload-photos--videos)
  - [3. Review Results](#3-review-results)
- [Command-Line Documentation](#command-line-documentation)
  - [Binary Overview](#binary-overview)
  - [Global Flags](#global-flags)
  - [Command: `upload`](#command-upload)
  - [Command: `creds` / `credentials`](#command-creds--credentials)
  - [Command: `version` and `help`](#command-version-and-help)
  - [Scripting and Headless Examples](#scripting-and-headless-examples)
- [Configuration and Credentials](#configuration-and-credentials)
  - [Config File Locations](#config-file-locations)
  - [Portable Mode](#portable-mode)
  - [Configuration File Structure](#configuration-file-structure)
  - [Authentication Methods](#authentication-methods)
- [Troubleshooting and FAQ](#troubleshooting-and-faq)
- [Development and Building from Source](#development-and-building-from-source)
  - [Prerequisites](#prerequisites)
  - [Repository Setup](#repository-setup)
  - [Building with Task](#building-with-task)
  - [Manual Build Commands](#manual-build-commands)
  - [Running in Development Mode](#running-in-development-mode)
  - [Testing, Linting, and Formatting](#testing-linting-and-formatting)
- [Architecture and Project Structure](#architecture-and-project-structure)
- [Security, Privacy, and Limitations](#security-privacy-and-limitations)
- [Contributing and Support](#contributing-and-support)
- [License and Acknowledgements](#license-and-acknowledgements)

---

## Overview

**gotohp** is a fast, lightweight, and modern Google Photos uploader available as both an intuitive desktop GUI and a standalone, scriptable command-line utility.

### The Problem It Solves
Official methods for uploading large personal photo libraries to Google Photos often suffer from browser tab crashes, restrictive rate-limiting, clumsy duplicate management, and lack of support for specialized formats like paired Apple Live Photos. Standard web uploaders also cannot run headless on home servers, NAS devices, or scheduled background backup jobs.

### Who Benefits
- **Photographers and Archivists**: Effortlessly transfer gigabytes of RAW camera files, HEIC captures, and high-resolution videos in parallel threads.
- **Apple Ecosystem Users**: Automatically pair static HEIC/JPEG photos with their companion `.MOV` video clips into native Google Photos Live Photos.
- **Power Users and Sysadmins**: Automate photo sync from Linux servers, NAS boxes, and storage arrays using a single static binary without graphical dependencies.

### How It Works
gotohp communicates directly with Google's native mobile client endpoints. It hashes files locally using SHA-1, performs pre-upload deduplication checks against your library, establishes chunked upload sessions via Google Scotty servers, and commits items with exact metadata.

---

## Key Features

- **Duplicate Prevention**: Pre-upload SHA-1 library checks verify whether each photo or video already exists in your Google Photos library, skipping duplicates and saving bandwidth.
- **Triple Drop Zones (GUI)**: Drag files or folders into one of three dedicated drop zones:
  - **Upload Only**: Direct upload into your photo library.
  - **Upload to Album**: Assign batch to an existing album key or create a new named album.
  - **Auto Album**: Automatically parse directory hierarchies and create albums matching local folder names.
- **Apple Live Photo Pairing**: Automatically discovers paired `.HEIC`/`.JPG` images and `.MOV` clips via QuickTime content identifier metadata or matching filenames, uploading them as unified Live Photos.
- **Unlimited Storage Mode**: Supports uploading via device profile parameters that do not deduct from your Google Drive quota (`use-quota: false`), or standard quota consumption mode (`use-quota: true`).
- **Storage Quality Options**: Toggle between **Original Quality** and Google's **Storage Saver** compression.
- **Filename Timestamp Extraction**: Automatically extracts capture dates from standard camera filename patterns (e.g., `20240709_182027.jpg`, `PXL_...`, or Unix timestamps in milliseconds).
- **Multi-Threaded Concurrency**: Configurable upload concurrency (default: 3 worker threads) with per-worker status tracking.
- **Extensive Media Format Support**: Validates against Google Photos supported media formats (AVIF, BMP, GIF, HEIC, JPEG, PNG, TIFF, WebP, Camera RAWs like CR2/CR3/NEF/ARW/DNG, and major video containers: MP4, MOV, MKV, WebM, etc.). Filters can be disabled (`--disable-filter`) when needed.
- **Multi-Account Switching**: Save multiple Google accounts and switch between active upload profiles with one click or command.
- **Dual Operating Modes**:
  - **Desktop GUI**: Native desktop window built with Wails v3, Vue 3, TypeScript, and Tailwind CSS.
  - **Headless CLI**: Standalone, lightweight executable compiled without GUI or webview libraries, featuring a rich terminal UI (TUI) powered by Bubbletea.

---

## Installation and Downloads

Precompiled binaries for all supported platforms are published on the [GitHub Releases page](https://github.com/shishir0x/photos0x/releases).

### Prebuilt Binaries

| Operating System | Architecture | GUI Package | Standalone CLI Binary |
| :--- | :--- | :--- | :--- |
| **Windows** | x86_64 (64-bit) | `photos0x-x64.exe` | `photos0x-cli-x64.exe` |
| **macOS** | Universal (Intel & Apple Silicon) | `photos0x-macos-universal.dmg`<br>`photos0x-macos-universal.zip` | `photos0x-cli-macos-universal` |
| **Linux** | x86_64 (64-bit) | `photos0x_amd64.AppImage`<br>`photos0x_amd64.deb` | `photos0x-cli_amd64`<br>`photos0x-cli_amd64.deb` |

Every release includes a `SHA256SUMS.txt` file to verify package integrity.

---

### macOS

1. Download `photos0x-macos-universal.dmg` from [Releases](https://github.com/shishir0x/photos0x/releases).
2. Open the `.dmg` file and drag **photos0x** to your `Applications` folder.
3. *First launch notice*: Because photos0x is open-source and not signed with an Apple Developer ID certificate, macOS Gatekeeper may present a security prompt. Right-click (or Control-click) `photos0x.app`, select **Open**, and confirm when prompted.

### Windows

1. Download `photos0x-x64.exe` from [Releases](https://github.com/shishir0x/photos0x/releases).
2. Place the executable in your preferred directory (e.g., `C:\Program Files\photos0x` or `D:\Tools\photos0x`).
3. Double-click `photos0x-x64.exe` to launch. No installer is required.

### Linux

#### Option A: AppImage (Universal)
```bash
wget https://github.com/shishir0x/photos0x/releases/latest/download/photos0x_amd64.AppImage
chmod +x photos0x_amd64.AppImage
./photos0x_amd64.AppImage
```

#### Option B: Debian / Ubuntu Package
```bash
wget https://github.com/shishir0x/photos0x/releases/latest/download/photos0x_amd64.deb
sudo dpkg -i photos0x_amd64.deb
sudo apt-get install -f # resolve any missing runtime dependencies
```

*Linux GUI Prerequisites*: The GUI application requires `libgtk-4` and `libwebkitgtk-6.0` (standard on modern Ubuntu 24.04+, Fedora 39+, and Debian 12+).

### Standalone Headless CLI

If you are running on a server, NAS (Synology, TrueNAS, Unraid), or headless container, download the standalone CLI binary. It requires **no graphical libraries, no display server (X11/Wayland), and no WebKit dependencies**:

```bash
# Example for Linux 64-bit
curl -LO https://github.com/shishir0x/photos0x/releases/latest/download/photos0x-cli_amd64
chmod +x photos0x-cli_amd64
sudo mv photos0x-cli_amd64 /usr/local/bin/photos0x-cli

# Test installation
photos0x-cli version
```

---

## Quick Start

### 1. Connect Account

gotohp uses Google's standard Embedded Setup workflow to authenticate your account:

1. Launch `gotohp` and click **Add Google account**.
2. Click **Open sign-in** to open Google's authentication page in your web browser.
3. Sign in to your Google Account and click **I agree** on the setup screen.
   *(Note: The page continuing to buffer or display a loading spinner after clicking "I agree" is expected behavior).*
4. Open your browser Developer Tools (**F12** or right-click → *Inspect*):
   - Navigate to **Application** (Chrome/Brave/Edge) or **Storage** (Firefox).
   - In the left sidebar under **Cookies**, click `https://accounts.google.com`.
   - Locate the cookie named **`oauth_token`** and copy its **Value**.
5. Return to gotohp, paste the token into the text field, and click **Connect account**.

### 2. Upload Photos & Videos

1. Drag photos, videos, or folders directly into the application window.
2. Choose your drop zone:
   - **Upload Only**: Add files directly to your main library.
   - **Upload to Album**: Provide an album title to organize the files into an album.
   - **Auto Album**: Automatically creates albums named after the folders being dragged.
3. The upload view displays live progress, remaining item counts, active transfer speed, and thread states.

### 3. Review Results

When complete, an upload summary drawer displays the total number of successfully uploaded, skipped (duplicate), and failed items. Click **Copy as JSON** to copy the detailed upload report (including Google media keys) to your clipboard.

---

## Command-Line Documentation

The command-line interface provides complete control over upload options, account management, and automated workflows.

### Binary Overview

Both binaries support CLI execution:
- `gotohp-cli [command]` — Standalone CLI binary (recommended for headless servers).
- `gotohp [command]` — Desktop binary (invoking with commands runs the CLI; running with no arguments opens the GUI).

### Global Flags

Applicable to root and subcommands:

| Flag | Short | Description |
| :--- | :--- | :--- |
| `--config <path>` | `-c` | Specify custom path to `gotohp.config` |
| `--help` | `-h` | Display help information |
| `--version` | `-v` | Display application version |

---

### Command: `upload`

Uploads files or directories to Google Photos.

```bash
gotohp-cli upload <path> [<path> ...] [flags]
```

> **Important**: The configuration file is used only to retrieve account credentials. All operational preferences (threads, quota, quality, proxy) are controlled by CLI flags so scripts behave consistently across environments.

#### Available Flags

| Flag | Short | Type | Default | Description |
| :--- | :--- | :--- | :--- | :--- |
| `--account` | | string | *(selected)* | Email address of the account to use |
| `--proxy` | | string | `""` | HTTP/HTTPS/SOCKS5 proxy URL |
| `--use-quota` | | boolean | `false` | Count uploads against the account's storage quota |
| `--saver` | | boolean | `false` | Upload in Storage Saver quality (default is original quality) |
| `--recursive` | `-r` | boolean | `false` | Scan subdirectories recursively |
| `--threads` | `-t` | int | `3` | Number of concurrent upload threads |
| `--force` | `-f` | boolean | `false` | Upload even if the file already exists in library |
| `--delete` | `-d` | boolean | `false` | Delete local file from disk after successful upload |
| `--disable-filter` | | boolean | `false` | Disable supported file extension checks |
| `--date-from-filename` | | boolean | `false` | Extract media timestamp from filename pattern |
| `--exclude` | `-e` | string | `""` | Exclude directory names matching pattern (e.g. `@eaDir`, `.git`) |
| `--album` | `-a` | string | `""` | Target album name (creates if needed); set to `AUTO` for folder-based albums |
| `--pair-live-photos` | | boolean | `false` | Enable Apple Live Photo pairing |
| `--upload-incomplete-live-photos` | | boolean | `false` | Upload unmatched Live Photo member as standalone item |
| `--update-existing-photos-to-live` | | boolean | `false` | Attach matching MOV video to existing library photo |
| `--ignore-apple-metadata` | | boolean | `false` | Match Live Photo pairs by filename stem instead of Apple metadata |
| `--log-level` | `-l` | string | `"info"` | Log verbosity: `debug`, `info`, `warn`, `error` |
| `--no-tui` | | boolean | `false` | Disable the interactive terminal UI (outputs standard text logs) |

---

### Command: `creds` / `credentials`

Manage saved Google Photos accounts and credentials.

#### `creds add <auth-string | oauth-token>`
Adds an account using either an `oauth_token` cookie or a raw credential string:

```bash
# Add using an oauth_token cookie
gotohp-cli creds add "oauth2_4/0AY0e-g..."

# Add via standard input (prevents token appearing in shell history)
gotohp-cli creds add - < /path/to/token.txt

# Specify a proxy for the initial token exchange
gotohp-cli creds add "oauth2_4/..." --proxy "http://127.0.0.1:8080"
```

#### `creds list` (alias: `ls`)
Lists all configured accounts. The active account is marked with an asterisk (`*`):

```bash
gotohp-cli creds list
```

#### `creds set <email>` (alias: `select`)
Switches the active default account. Supports partial string matching:

```bash
gotohp-cli creds set user@gmail.com
gotohp-cli creds set user    # Matches if "user" uniquely identifies an account
```

#### `creds remove <email>` (alias: `rm`)
Removes an account from configuration:

```bash
gotohp-cli creds remove user@gmail.com
```

---

### Command: `version` and `help`

```bash
# View version
gotohp-cli version

# Detailed help for any command
gotohp-cli upload --help
gotohp-cli creds --help
```

---

### Scripting and Headless Examples

#### Recursive batch upload with high concurrency:
```bash
gotohp-cli upload /mnt/storage/photos \
  --recursive \
  --threads 6 \
  --exclude "@eaDir" \
  --date-from-filename
```

#### Headless cron job with plain text logging:
```bash
gotohp-cli upload /data/camera_backup \
  --recursive \
  --no-tui \
  --log-level info >> /var/log/gotohp-backup.log 2>&1
```

#### Upload into automatically named albums:
```bash
# Folders like "Vacation 2024" and "Birthdays" will become Google Photos albums:
gotohp-cli upload /home/user/Pictures --recursive --album AUTO
```

#### Upload Live Photos:
```bash
gotohp-cli upload /Volumes/iPhonePhotos \
  --recursive \
  --pair-live-photos \
  --threads 4
```

---

## Configuration and Credentials

### Config File Locations

When no `--config` flag is passed, gotohp resolves configuration in the following order of precedence:

1. **Portable Config**: An existing file named `gotohp.config` located in the same directory as the executable.
2. **Working Directory**: An existing file named `gotohp.config` in the current working directory.
3. **User Config Directory (Default)**:
   - **Windows**: `%APPDATA%\gotohp\gotohp.config` (e.g. `C:\Users\<user>\AppData\Roaming\gotohp\gotohp.config`)
   - **Linux**: `~/.config/gotohp/gotohp.config` (or `$XDG_CONFIG_HOME/gotohp/gotohp.config`)
   - **macOS**: `~/Library/Application Support/gotohp/gotohp.config`

### Portable Mode

To run gotohp portably (keeping configuration, tokens, and settings alongside the binary on a flash drive or standalone directory), create an empty file named `gotohp.config` next to `gotohp.exe` or `gotohp-cli`.

### Configuration File Structure

The file uses standard YAML:

```yaml
account:
  credentials:
    - "Email=user@example.com&Token=oauth2_token_string..."
  selected: "user@example.com"

preferences:
  proxy: ""
  use_quota: false
  saver: false
  recursive: false
  force_upload: false
  pair_live_photos: false
  skip_incomplete_live_photos: true
  update_existing_photos_to_live: false
  upload_threads: 3
  delete_from_host: false
  disable_unsupported_files_filter: false
  set_date_from_filename: false
  exclude_pattern: ""
```

---

### Authentication Methods

#### Method 1: Embedded Setup Cookie (Recommended)
This is the standard, root-free method documented in [Quick Start](#1-connect-account). It requests a standard Google account master token using the `oauth_token` cookie generated by Google's native embedded sign-in endpoint.

#### Method 2: Android ReVanced Auth String (No root required)
If you run Google Photos ReVanced with GmsCore:
1. Connect your Android device via USB with ADB debugging enabled.
2. In a terminal, monitor logcat output:
   - **Windows**: `adb logcat | FINDSTR "auth%2Fphotos.native"`
   - **Linux / macOS**: `adb logcat | grep "auth%2Fphotos.native"`
3. Remove and re-add your Google Account in GmsCore, then open Google Photos.
4. Copy the logged line beginning with `androidId=` through the end of the line.
5. Paste into gotohp (`Add Google account` → `Advanced` → `Add raw credential`).

#### Method 3: Official Android APK Capture (Root required)
1. Using a rooted device or emulator with [HTTP Toolkit](https://httptoolkit.com).
2. Intercept ADB traffic and filter requests containing `https://www.googleapis.com/auth/photos.native`.
3. Copy the raw request body into gotohp as a raw credential.
4. If gotohp reports that the credential requires a **Token Binding Key**, connect your rooted device via ADB and click **Read from ADB**. gotohp will read the `lstBindingKeyAlias` from Android's AccountManager database.

---

## Troubleshooting and FAQ

### "No credentials found" or "Connect Google Photos" prompt
- **Cause**: No account has been added yet or the selected account's credentials have expired.
- **Solution**: Follow the [Quick Start](#1-connect-account) steps to add your account using the `oauth_token` cookie. In CLI, verify with `gotohp-cli creds list`.

### The Embedded Setup page spins indefinitely on "I agree"
- **Cause**: This is normal behavior. Google's mobile webview setup flow does not redirect to a completion page inside standard desktop browsers.
- **Solution**: Once you click "I agree", the required `oauth_token` cookie has already been generated. Open DevTools (**F12**) → Storage/Application → Cookies → `accounts.google.com` to copy it.

### Upload starts but files are skipped
- **Cause**: By default, gotohp hashes each item and queries Google Photos for existing matches. If a file with an identical SHA-1 hash is already present in your library, it is skipped to save time and bandwidth.
- **Solution**: To force a re-upload regardless of library state, enable **Force Upload** in GUI Settings or pass the `--force` (`-f`) flag in CLI.

### Apple Live Photos upload as static images or separate videos
- **Cause**: Live Photo pairing was not enabled, or companion `.MOV` video files were missing.
- **Solution**:
  - In GUI: Enable **Pair Apple Live Photos** in Settings.
  - In CLI: Pass `--pair-live-photos`.
  - Both the `.HEIC`/`.JPG` image and its matching `.MOV` video must reside in the same folder.

### "Token binding key required" on Android credentials
- **Cause**: Official Google Photos Android app credentials generated on modern devices may be bound to hardware cryptographic keys stored in Android's `AccountManager` database.
- **Solution**: Connect the device with USB debugging and root access, then click **Read from ADB** in the GUI.

### Webview startup issues on Linux
- **Cause**: Missing WebKitGTK runtime libraries.
- **Solution**: Install the required packages: `sudo apt-get install libgtk-4-dev libwebkitgtk-6.0-dev`. Alternatively, use the standalone `gotohp-cli` which requires no graphical packages.

---

## Development and Building from Source

### Prerequisites

- **Go**: Version `1.22` or higher (tested with Go `1.27`).
- **Node.js**: Version `20.19.0+`, `22.13.0+`, or `24+` with `npm`.
- **Wails v3**: `v3.0.0-beta.21`
  ```bash
  go install -v github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.21
  ```
- **Task** (Optional, recommended task runner):
  ```bash
  go install github.com/go-task/task/v3/cmd/task@latest
  ```

---

### Repository Setup

```bash
git clone https://github.com/shishir0x/photos0x.git
cd gotohp
```

### Building with Task

The project includes preconfigured `Taskfile.yml` build definitions:

```bash
# Build the application for your current operating system
task build

# Create a production package
task package

# Run development mode
task dev

# Run code formatters and linters
task lint
task fmt
```

---

### Manual Build Commands

#### 1. Compile the Standalone CLI Binary (Pure Go, Headless)
```bash
# Windows (PowerShell)
$env:CGO_ENABLED="0"; go build -tags cli -trimpath -ldflags="-w -s" -o bin/gotohp-cli.exe .

# Linux / macOS (Bash)
CGO_ENABLED=0 go build -tags cli -trimpath -ldflags="-w -s" -o bin/gotohp-cli .
```

#### 2. Compile the Desktop GUI Binary
First build the frontend assets:
```bash
cd frontend
npm install
npm run build
cd ..
```

Then compile the Wails desktop binary:
```bash
# Windows
go build -tags production -ldflags="-w -s -H windowsgui" -o bin/gotohp.exe .

# Linux / macOS
go build -tags production -ldflags="-w -s" -o bin/gotohp .
```

---

### Running in Development Mode

Run the hot-reloading development server:
```bash
wails3 dev -config ./build/config.yml -port 9245
```

### Testing, Linting, and Formatting

```bash
# Run all Go tests
go test ./...

# Run frontend linter
cd frontend && npm run lint && cd ..

# Format Go code
gofumpt -l -w .
```

---

## Architecture and Project Structure

```
gotohp/
├── core/                       # Reusable Go library implementing the core backend
│   ├── album.go                # Google Photos album creation & management
│   ├── api.go                  # Google Photos private API client & auth session
│   ├── configmanager.go        # Config persistence, paths, and migrations
│   ├── filename_parser.go      # Timestamp extraction from file naming patterns
│   ├── googleauth.go           # EmbeddedSetup token exchange & credential validation
│   ├── livephoto.go            # Apple Live Photo pairing & classification
│   ├── livephoto_metadata.go   # QuickTime Content Identifier metadata reader
│   ├── upload.go               # Multi-threaded upload manager & Scotty committer
│   └── upload_options.go       # Upload configuration definitions & flags
├── internal/
│   ├── cli/                    # CLI implementation (Cobra commands & Bubbletea TUI)
│   │   ├── cli.go              # CLI root command dispatcher
│   │   ├── credentials.go      # Account & credential CLI subcommands
│   │   ├── upload.go           # Terminal progress reporter & upload runner
│   │   └── upload_args.go      # Upload flag definitions & validator
│   └── gui/                    # GUI bridges & Wails progress event reporters
│       ├── wails_events.go     # Event structures emitted to Vue frontend
│       └── wails_reporter.go   # Progress reporter forwarding to Wails events
├── frontend/                   # Vue 3 desktop interface
│   ├── src/
│   │   ├── App.vue             # Main window layout, drop zones, account select
│   │   ├── SettingsPanel.vue   # Slide-out settings drawer & preference switches
│   │   ├── Upload.vue          # Live progress, transfer speed & thread status
│   │   └── components/         # Radix Vue / Reka UI components & GoogleAuthSetup
│   ├── package.json            # Vite, Tailwind CSS v4, Vue 3 dependencies
│   └── vite.config.ts          # Frontend bundler configuration
├── generated/                  # Compiled Protocol Buffers for Google Photos RPC
├── build/                      # Platform packaging assets (macOS DMG, Windows icons, nfpm)
├── main.go                     # Desktop GUI application entry point
├── main_cli.go                 # Standalone CLI binary entry point (`-tags cli`)
└── Taskfile.yml                # Task runner build automation
```

---

## Security, Privacy, and Limitations

- **Local Credential Storage**: Authentication tokens are stored locally in plain-text YAML within `gotohp.config`, secured by operating system file-system permissions (`0600`). Never commit this file or share its contents publicly.
- **Quota & Protocol Longevity**: Features such as unlimited storage or specific device simulation rely on endpoints provided by Google for mobile devices. Google may modify, restrict, or rate-limit these services at any time. gotohp should not replace an independent, 3-2-1 backup strategy.
- **Direct Communications**: gotohp connects directly to Google services (`accounts.google.com`, `android.clients.google.com`, and `photosdata-pa.googleapis.com`). No telemetry or user data is transmitted to third-party servers.

---

## Contributing and Support

Contributions are welcome! Please follow these guidelines:

1. **Pull Requests**:
   - Limit each PR to one focused change.
   - Use [Conventional Commits](https://www.conventionalcommits.org) titles with lowercase descriptions: `<type>(<scope>): <description>` (e.g. `feat(cli): add exclude flag` or `fix(settings): make rows clickable`).
   - Run tests before opening a PR: `go test ./...`.
2. **Issue Reporting**:
   - Report bugs or request features through the [GitHub Issue Tracker](https://github.com/shishir0x/photos0x/issues).
   - Please include your OS version, binary type (GUI or CLI), and error output (with sensitive tokens redacted).

---

## License and Acknowledgements

This project is licensed under the **MIT License** — see the [LICENSE](LICENSE) file for complete details.

### Upstream Project
gotohp is based on the original work by [xob0t](https://github.com/xob0t/gotohp). We are deeply grateful for their foundational research and reverse engineering of the Google Photos client protocols.

### Key Open-Source Libraries
- [Wails](https://wails.io) — Native desktop application framework for Go.
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) & [Lip Gloss](https://github.com/charmbracelet/lipgloss) — Terminal UI toolkit.
- [Cobra](https://github.com/spf13/cobra) — Modern CLI command parser.
- [Vue 3](https://vuejs.org) & [Vite](https://vitejs.dev) — Frontend framework and bundler.
- [Tailwind CSS](https://tailwindcss.com) & [Reka UI](https://reka-ui.com) — Accessible, modern interface components.
