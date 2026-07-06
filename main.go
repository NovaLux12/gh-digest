// Command gh-digest summarises GitHub account activity.
//
// Usage:
//
//	gh-digest --owner <name> [--format markdown|json] [--stale-days 30] [--stale-only]
//
// Authentication: reads GH_TOKEN (or GITHUB_TOKEN) for higher rate limits.
// With no token: 60 requests/hour. With a PAT: 5000/hour.
//
// Output is grouped by repo and flags stale items (open > stale-days, no
// commits > stale-days) at the bottom. Suitable for pasting into heartbeat
// reports or piping into a digest cron.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

const version = "0.1.0"

func main() {
	var (
		owner       = flag.String("owner", "", "GitHub user or org name (required)")
		format      = flag.String("format", "markdown", "Output format: markdown or json")
		staleDays   = flag.Int("stale-days", 30, "Days before an item is flagged stale")
		staleOnly   = flag.Bool("stale-only", false, "Show only stale items")
		includeArc  = flag.Bool("include-archived", false, "Include archived repos")
		maxRepos    = flag.Int("max-repos", 100, "Max repos to inspect")
		showVersion = flag.Bool("version", false, "Print version and exit")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "gh-digest v%s — summarise GitHub account activity\n\n", version)
		fmt.Fprintf(os.Stderr, "Usage: %s --owner <name> [flags]\n\nFlags:\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVersion {
		fmt.Printf("gh-digest v%s\n", version)
		return
	}
	if *owner == "" {
		flag.Usage()
		os.Exit(2)
	}

	token := tokenFromEnv()
	client := NewClient(token)

	repos, err := client.ListRepos(*owner, *maxRepos)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error listing repos for %s: %v\n", *owner, err)
		os.Exit(1)
	}

	cutoff := time.Now().AddDate(0, 0, -*staleDays)

	var summaries []RepoSummary
	for _, r := range repos {
		if r.Archived && !*includeArc {
			continue
		}
		s := RepoSummary{Repo: r}
		s.OpenIssues, s.OpenPRs, err = client.CountOpenIssuesAndPRs(*owner, r.Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: %s: %v\n", r.Name, err)
			continue
		}
		s.LatestRelease, err = client.LatestRelease(*owner, r.Name)
		if err != nil && !IsNotFound(err) {
			// Non-fatal: a repo with no releases is fine.
			s.LatestReleaseErr = err.Error()
		}
		summaries = append(summaries, s)
	}

	switch *format {
	case "json":
		out, _ := json.MarshalIndent(struct {
			Owner     string         `json:"owner"`
			Generated time.Time      `json:"generated"`
			Repos     []RepoSummary  `json:"repos"`
			Stale     []StaleFlag    `json:"stale_flags"`
		}{*owner, time.Now().UTC(), summaries, FlagStale(summaries, cutoff)}, "", "  ")
		fmt.Println(string(out))
	case "markdown":
		fallthrough
	default:
		RenderMarkdown(os.Stdout, *owner, summaries, cutoff, *staleOnly)
	}
}

func tokenFromEnv() string {
	if t := os.Getenv("GH_TOKEN"); t != "" {
		return t
	}
	return os.Getenv("GITHUB_TOKEN")
}

// helper used by both renderers
func plural(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

func pad(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}

func sortByStaleness(s []RepoSummary) []RepoSummary {
	out := append([]RepoSummary(nil), s...)
	sort.Slice(out, func(i, j int) bool {
		ai := out[i].Repo.PushedAt
		aj := out[j].Repo.PushedAt
		if ai.Equal(aj) {
			return out[i].Repo.Name < out[j].Repo.Name
		}
		return ai.Before(aj)
	})
	return out
}