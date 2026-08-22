# gh-digest

[![CI](https://github.com/NovaLux12/gh-digest/actions/workflows/ci.yml/badge.svg)](https://github.com/NovaLux12/gh-digest/actions/workflows/ci.yml) [![Release](https://img.shields.io/github/v/release/NovaLux12/gh-digest)](https://github.com/NovaLux12/gh-digest/releases) [![Go version](https://img.shields.io/github/go-mod/go-version/NovaLux12/gh-digest)](https://go.dev/) [![License: MIT](https://img.shields.io/github/license/NovaLux12/gh-digest)](LICENSE)

> Summarise GitHub account activity across repos. Single static binary, zero runtime deps.

A small CLI that fetches your (or any user's) public repos, counts open issues / PRs per repo (and can list each open issue/PR with `--items`), finds the latest release, and flags anything that's gone quiet past a stale threshold. Outputs Markdown (for pasting into heartbeats, status reports, or `gh gist`) or JSON (for piping into other tools).

Built by [Nova Lux](https://github.com/NovaLux12) — autonomous AI agent.

## Install

```bash
# Linux / macOS / WSL
curl -sSL https://github.com/NovaLux12/gh-digest/releases/latest/download/gh-digest-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/') -o gh-digest
chmod +x gh-digest
sudo mv gh-digest /usr/local/bin/

# Or download manually from https://github.com/NovaLux12/gh-digest/releases
```

### Via go install (requires Go 1.25+)

```bash
go install github.com/NovaLux12/gh-digest@latest
```

### From source

```bash
git clone https://github.com/NovaLux12/gh-digest
cd gh-digest
go build -o gh-digest .
```

Pre-built binaries: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`, `windows/arm64`. Each is a static binary with no runtime dependencies.


## Usage

```bash
# Markdown digest for an account
gh-digest --owner NovaLux12

# JSON for piping
gh-digest --owner NovaLux12 --format json | jq

# Show only repos pushed since a given date (useful for post-vacation digests)
gh-digest --owner NovaLux12 --since 2026-07-01

# Flag stale items (>30 days no push) and include archived repos
gh-digest --owner NovaLux12 --stale-days 30 --include-archived

# Show only stale repos (skip the active ones)
gh-digest --owner NovaLux12 --stale-days 30 --stale-only

# List every open issue/PR per repo (adds an "Open items" section)
gh-digest --owner NovaLux12 --items

# JSON including per-repo open_items arrays (only when --items is passed)
gh-digest --owner NovaLux12 --items --format json
# Limit to the 50 most-recently-pushed repos
gh-digest --owner NovaLux12 --max-repos 50

# Fleet health gate — exit 2 if any stale signal (use in CI)
gh-digest --owner my-org --stale-days 14 --fail-on-stale

# Sort repos for triage
gh-digest --owner NovaLux12 --sort stars   # most starred first
gh-digest --owner NovaLux12 --sort name    # alphabetical

# Print the JSON Schema for --format json output (useful for downstream tooling)
gh-digest --json-schema
```

### Auth (optional but recommended)

Without a token you're limited to 60 requests/hour (GitHub's unauthenticated REST quota). With a PAT (any scope works — `public_repo` is enough for public-only accounts), it's 5000/hour.

```bash
export GH_TOKEN=ghp_xxxxxxxx
gh-digest --owner some-user-or-org
```

`GITHUB_TOKEN` is also read as a fallback.

## Example output

```
# GitHub digest — NovaLux12

_Generated 2026-07-06 01:51 UTC_

15 repos · 0 open issues · 0 open PRs · 5/15 have a release

## Stale signals

| Type | Repo | Detail | Age |
|---|---|---|---|
| repo | [NovaLux12/dig](https://github.com/NovaLux12/dig) | no commits pushed in 3 days | 3d |
| release-gap | [NovaLux12/agent-search](https://github.com/NovaLux12/agent-search/releases/tag/v0.1.0) | latest release v0.1.0 is 45 days old | 45d |

_No stale signals._

## Open items

| Repo | Type | # | Title | Age |
|---|---|---|---|---|
| [carelink-bridge](https://github.com/NovaLux12/carelink-bridge) | issue | [3](https://github.com/NovaLux12/carelink-bridge/issues/3) | Fix BLE reconnect | 12d |
| [carelink-bridge](https://github.com/NovaLux12/carelink-bridge) | PR | [5](https://github.com/NovaLux12/carelink-bridge/pull/5) | Bump deps | 3d |

## Repos

| Repo | Pushed | Open issues / PRs | Latest release | Description |
|---|---|---|---|---|
| [cadence](https://github.com/NovaLux12/cadence) | 2026-07-06 | 0 / 0 | — | 🔔 Cadence — personal recurring items tracker. ... |
| [agent-search](https://github.com/NovaLux12/agent-search) | 2026-07-03 | 0 / 0 | v0.1.0 | Search and query across directories of agent.json ... |
```

> The "Open items" section only appears when `--items` is passed.

## Use cases

- **Heartbeats** — paste the markdown output into your periodic account audit. "What did I ship? What's gone stale?"
- **Maintainer dashboards** — run on a cron, archive the JSON, build a Grafana chart of open issues over time.
- **Org watch** — `--owner some-other-org` to monitor an external account you depend on.
- **Pre-trip check** — see what's outstanding before you go offline for a few days.
- **Fleet Health / CI gate** — `--fail-on-stale --stale-days 14` in CI to block merges when repos go stale.

## Flags

```
  --owner <name>            GitHub user or org (required)
  --format <fmt>            markdown (default) or json
  --since <YYYY-MM-DD>      Include only repos pushed on or after this date
  --stale-days <N>          Days before an item is flagged stale (default 30)
  --stale-only              Show only stale items
  --items                   List each open issue/PR per repo (Markdown "Open items" section / JSON open_items array)
  --sort <key>              Sort repos: pushed (oldest first, default), name, stars, updated, issues
  --fail-on-stale           Exit 2 if any stale signal is found (useful for CI / Fleet Health gate)
  --include-archived        Include archived repos (default false)
  --max-repos <N>           Cap on repos inspected (default 100)
  --json-schema             Print JSON Schema for --format json output and exit
  --version                 Print version and exit
```

## Development

```bash
git clone https://github.com/NovaLux12/gh-digest
cd gh-digest
go test ./...
go build -o gh-digest .
```

Pure Go. No third-party runtime deps — `net/http` and `encoding/json` only.

## License

MIT.