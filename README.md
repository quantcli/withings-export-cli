# withings-export-cli

Export health data from [Withings](https://www.withings.com) scales, watches, and trackers. A command-line tool to back up measurements, sleep summaries, activity, and workouts — all from your terminal, with CSV or JSON output.

[![Latest Release](https://img.shields.io/github/v/release/quantcli/withings-export-cli)](https://github.com/quantcli/withings-export-cli/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/github/go-mod/go-version/quantcli/withings-export-cli)](go.mod)
![Platforms](https://img.shields.io/badge/platform-macOS%20%7C%20Linux%20%7C%20Windows-lightgrey)

## Features

- **Measurements** — weight, body fat %, muscle mass, hydration, bone mass, blood pressure, heart rate, SpO₂, temperature, VO₂ max, ECG intervals
- **Sleep summaries** — per-night sleep stages, sleep score, HR, RR, snoring, apnea-hypopnea index
- **Activity** — daily steps, distance, elevation, calories, HR zones
- **Workouts** — runs, walks, bikes, swims, etc. with duration, HR, distance, and more
- **Date filtering** — relative (`30d`, `4w`, `6m`, `1y`) or absolute (`2026-01-01`)
- **CSV by default, JSON with `--json`** — pipe into your tool of choice
- **Multi-platform** — pre-built binaries for macOS (Intel + Apple Silicon), Linux, and Windows

## Quick Start

```sh
# Install with Homebrew
brew tap quantcli/tap
brew install withings-export

# Create a Withings developer app at https://developer.withings.com/,
# set the callback URL to http://127.0.0.1, then:
export WITHINGS_CLIENT_ID=...
export WITHINGS_CLIENT_SECRET=...
export WITHINGS_CALLBACK_URL='https://redirectmeto.com/http://localhost:8128/oauth/authorize'

# Log in (opens your browser) and pull recent data
withings-export auth login
withings-export measurements --since 30d
withings-export sleep --since 7d
```

## Install

**Homebrew (macOS / Linux):**
```sh
brew tap quantcli/tap
brew install withings-export
```

Or download a pre-built binary from the [releases page](https://github.com/quantcli/withings-export-cli/releases/latest):

**macOS (Apple Silicon):**
```sh
curl -Lo /tmp/withings-export.zip https://github.com/quantcli/withings-export-cli/releases/latest/download/withings-export_darwin_arm64.zip
unzip -jo /tmp/withings-export.zip -d ~/bin && rm /tmp/withings-export.zip
chmod +x ~/bin/withings-export
```

**macOS (Intel):**
```sh
curl -Lo /tmp/withings-export.zip https://github.com/quantcli/withings-export-cli/releases/latest/download/withings-export_darwin_amd64.zip
unzip -jo /tmp/withings-export.zip -d ~/bin && rm /tmp/withings-export.zip
chmod +x ~/bin/withings-export
```

**Linux (amd64):**
```sh
curl -Lo /tmp/withings-export.zip https://github.com/quantcli/withings-export-cli/releases/latest/download/withings-export_linux_amd64.zip
unzip -jo /tmp/withings-export.zip -d ~/bin && rm /tmp/withings-export.zip
chmod +x ~/bin/withings-export
```

**Windows (amd64):**

Download `withings-export_windows_amd64.zip` from the [releases page](https://github.com/quantcli/withings-export-cli/releases/latest), extract, and add the directory to your PATH.

## Authentication

Withings uses OAuth2 — you need your own developer app.

1. Go to <https://developer.withings.com/>, sign in, and **create a new public API app**.
2. Withings requires an **HTTPS** callback URL, so use the [redirectmeto.com](https://redirectmeto.com) bounce trick. Register this as your callback:

   ```
   https://redirectmeto.com/http://localhost:8128/oauth/authorize
   ```

   (redirectmeto.com takes any URL after its host and 302-redirects your browser there, so Withings' HTTPS requirement is satisfied while the auth code ends up on your local server.)
3. Copy the **Client ID** and **Consumer Secret** into env vars (or you'll be prompted):

```sh
export WITHINGS_CLIENT_ID=...
export WITHINGS_CLIENT_SECRET=...
export WITHINGS_CALLBACK_URL='https://redirectmeto.com/http://localhost:8128/oauth/authorize'
withings-export auth login
```

If `WITHINGS_CALLBACK_URL` is unset the CLI falls back to binding a random port on `127.0.0.1` — only works if your Withings app allows a plain `http://` callback.

Tokens are stored at `~/.config/withings-export/auth.json` (mode `0600`). Access tokens are refreshed automatically when they expire.

```sh
withings-export auth login      # OAuth2 in your browser
withings-export auth logout     # Remove stored tokens
withings-export auth refresh    # Force refresh and print status
```

## Usage

### Measurements (scales, BP monitors, ECG)

```sh
withings-export measurements                   # last 30 days, CSV to stdout
withings-export measurements --since 1y        # last year
withings-export measurements --since 2026-01-01
withings-export measurements --types 1,6,76    # only weight, body fat %, muscle mass
withings-export measurements --json            # JSON instead of CSV
```

Common measure type codes: `1`=weight (kg), `6`=body fat %, `8`=fat mass (kg), `9`=diastolic BP, `10`=systolic BP, `11`=heart pulse, `54`=SpO₂ %, `76`=muscle mass, `77`=hydration, `88`=bone mass.

### Sleep

```sh
withings-export sleep                  # last 30 nights, CSV
withings-export sleep --since 6m       # last 6 months
withings-export sleep --json
```

Includes total sleep time, stages (light/deep/REM), sleep score, heart rate, respiratory rate, snoring episodes, and apnea-hypopnea index (if supported by your device).

### Activity

```sh
withings-export activity               # last 30 days, CSV
withings-export activity --since 1y
withings-export activity --json
```

Daily steps, distance, elevation, calories, time in HR zones.

### Workouts

```sh
withings-export workouts               # last 90 days, CSV
withings-export workouts --since 6m
withings-export workouts --json
```

Per-workout category (run/walk/bike/etc.), duration, calories, HR, distance, elevation.

## Output Format

**CSV (default):** header row + one row per data point. Suitable for spreadsheets, Grafana, pandas, etc.

**JSON (`--json`):** pretty-printed JSON array. Good for `jq` or custom scripts.

Example — average weight over the last year:
```sh
withings-export measurements --since 1y --types 1 --json \
  | jq '[.[] | .value] | add / length'
```

## About Withings

Withings makes connected health hardware — smart scales (Body+, Body Scan), hybrid watches (ScanWatch), BP monitors, sleep trackers. All data is stored in [Health Mate](https://www.withings.com/us/en/health-mate) and exposed via the [Withings Public API](https://developer.withings.com/).

This CLI is unofficial and not affiliated with Withings.

## License

MIT — see [LICENSE](LICENSE).
