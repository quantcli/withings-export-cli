# Worked example: withings-export

**Audience:** Withings device owners (scale, sleep tracker, activity watch) who want their health data in a terminal or piped to an LLM agent.

**Prerequisites:** `withings-export` installed (see [GETTING_STARTED.md](https://github.com/quantcli/common/blob/main/GETTING_STARTED.md)), a Withings account, `withings-export auth login` completed.

---

## 1. Authenticate

Withings uses OAuth2. Run the login flow once:

```sh
withings-export auth login
```

This opens a browser tab. Complete the OAuth consent and the token is stored at `~/.config/withings-export/auth.json`.

Check readiness:

```sh
withings-export auth status
```

**Expected when logged in:**

```
logged in
```

Exit 0. **Expected when not logged in:**

```
Error: not logged in — run: withings-export auth login
```

Exit 1. `auth status` makes no network call — it only reads the local token file.

**HTTPS callback workaround:** If your Withings OAuth app requires HTTPS, register `https://redirectmeto.com/http://localhost:8128/oauth/authorize` as the callback URL and set:

```sh
export WITHINGS_CALLBACK_URL="https://redirectmeto.com/http://localhost:8128/oauth/authorize"
```

---

## 2. Orient with `prime`

```sh
withings-export prime
```

**Output** (reproduced at HEAD, 2026-05-19):

```
withings-export — primer for LLM agents
=======================================

WHAT IT IS
  CLI for personal Withings data: activity, sleep, workouts, body
  measurements, minute-level intraday samples (HR/HRV/SpO2/steps).

I/O
  stdout: data in --format markdown (default), json, or csv.
  stderr: errors. Exit 0 on success including empty results.

AUTH
  withings-export auth login          OAuth2 in browser; tokens stored locally.
  withings-export auth status         Exit 0 if usable, 1 with reason. No network call.
  withings-export auth refresh|logout

  Optional env: WITHINGS_CLIENT_ID, WITHINGS_CLIENT_SECRET, WITHINGS_CALLBACK_URL.
  HTTPS-callback workaround: register https://redirectmeto.com/http://localhost:8128/oauth/authorize
  (verbatim) and set WITHINGS_CALLBACK_URL to the same string.

DATE FLAGS  (every subcommand)
  --since VALUE / --until VALUE
  VALUE: today | yesterday | YYYY-MM-DD | Nd/Nw/Nm/Ny
  See https://github.com/quantcli/common/blob/main/CONTRACT.md#3-date-flags

SUBCOMMANDS  (defaults in parens)
  activity      (30d) daily steps/distance/calories/HR zones
  sleep         (30d) stages, score, HR/RR; --derive polyfills missing nights
  workouts      (90d) runs/walks/bikes/lifts with calories/HR/distance
  measurements  (30d) weight/fat/BP/SpO2/temp; --types LIST filters
  intraday      (1d)  minute-level HR/HRV/SpO2/steps; dense — keep windows narrow

  Inspect any subcommand's row schema with: <subcommand> --since 1d --format json

EXAMPLES
  withings-export sleep --since 7d
  withings-export workouts --since 30d --format json |
    jq '.[] | {date, category, hr: .data.hr_average}'
  withings-export measurements --since 30d --types 1 --format json |
    jq 'sort_by(.date) | last'

GOTCHAS
  - Times are LOCAL; JSON epoch seconds are zone-agnostic.
  - 'intraday' is a firehose — wide windows take minutes.
  - Withings rate-limits aggressive callers (HTTP 601). 'sleep --derive' throttles itself.
  - Sleep score / apnea fields appear only on supported devices.
  - 'workouts.category' is an integer code in JSON; markdown/CSV map common codes to names.
```

---

## 3. Export last week's activity

```sh
withings-export activity --since 7d
```

**Expected output** (markdown, one row per day):

```
| Date       | Steps | Distance (km) | Calories | Active (min) |
|------------|-------|---------------|----------|--------------|
| 2026-05-12 | 9240  | 6.8           | 2341     | 42           |
| 2026-05-13 | 7110  | 5.2           | 2190     | 28           |
```

---

## 4. Export sleep summaries

```sh
withings-export sleep --since 7d
```

For structured data including HR and sleep stages:

```sh
withings-export sleep --since 7d --format json | jq '.[0]'
```

---

## 5. Get most recent body weight measurement

```sh
withings-export measurements --since 30d --types 1 --format json \
  | jq 'sort_by(.date) | last'
```

`--types 1` filters to weight measurements (Withings measurement type 1). Omit `--types` to get all measurement types.

---

## 6. Export workout HR data

```sh
withings-export workouts --since 30d --format json \
  | jq '.[] | {date, category, hr: .data.hr_average}'
```

`category` is an integer in JSON; markdown and CSV output maps common codes to names like `Running`, `Cycling`.

---

## 7. Check flag validation (contract §4, §7)

These run without credentials (hermetic by [CONTRACT.md §7](https://github.com/quantcli/common/blob/main/CONTRACT.md#7-hermeticity)):

```sh
withings-export activity --help    # exits 0; no network call
```

**Expected:** help text including `--since`, `--until`, `--format` flags; exit 0.

```sh
withings-export activity --format lol 2>&1; echo "exit: $?"
```

**Expected:**

```
Error: invalid argument "lol" for "--format" flag: must be one of: markdown, json, csv
exit: 1
```

Error on stderr, nothing on stdout, exit 1.

---

## 8. Run the contract conformance suite

`withings-export` has the most complete compat coverage of the three CLIs: the suite stands up a stub HTTP server and a fake token so the data-path subtests run fully without real Withings credentials.

```sh
git clone https://github.com/quantcli/withings-export-cli
cd withings-export-cli
go build -o /tmp/withings-export .
WITHINGS_EXPORT_BIN=/tmp/withings-export go test -tags=compat ./...
```

**Expected:**

```
ok      github.com/quantcli/withings-export-cli    0.003s
ok      github.com/quantcli/withings-export-cli/internal/auth    0.003s
```

The suite covers CONTRACT.md §4 (format flag surface, `--format json` returns a JSON array, `--format csv` returns a header row, default equals `--format markdown`) and §7 (hermeticity) across all five data subcommands. All cells in the CONTRACT.md Status table for `withings-export` are **machine**-attested.

---

## What to look at next

- `withings-export intraday --help` — minute-level HR/HRV/SpO2 (keep windows narrow)
- `withings-export prime` — jq recipes and rate-limit gotchas
- [CONTRACT.md §3](https://github.com/quantcli/common/blob/main/CONTRACT.md#3-date-flags) — date flag semantics
- [CONTRACT.md §4](https://github.com/quantcli/common/blob/main/CONTRACT.md#4-output-format) — output format contract (`csv` is withings-only today)
- [crono-export example](https://github.com/quantcli/crono-export-cli/blob/main/docs/example.md) — if you also track nutrition
- [liftoff-export example](https://github.com/quantcli/liftoff-export-cli/blob/main/docs/example.md) — if you also track gym workouts
