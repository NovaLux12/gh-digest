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

const version = "0.3.1"

// JSONSchema is the JSON Schema for the --format json output.
const JSONSchema = `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "gh-digest",
  "type": "object",
  "properties": {
    "owner": {
      "type": "string",
      "description": "GitHub user or org whose repos are summarised"
    },
    "generated": {
      "type": "string",
      "format": "date-time",
      "description": "ISO-8601 UTC timestamp when this digest was generated"
    },
    "repos": {
      "type": "array",
      "description": "Per-repo summary objects",
      "items": {
        "type": "object",
        "properties": {
          "name":           { "type": "string" },
          "full_name":      { "type": "string" },
          "description":   { "type": "string" },
          "private":       { "type": "boolean" },
          "archived":      { "type": "boolean" },
          "visibility":    { "type": "string" },
          "pushed_at":     { "type": "string", "format": "date-time" },
          "updated_at":    { "type": "string", "format": "date-time" },
          "html_url":      { "type": "string", "format": "uri" },
          "stargazers_count": { "type": "integer" },
          "open_issues_count": { "type": "integer" },
          "open_issues":   { "type": "integer" },
          "open_prs":      { "type": "integer" },
          "open_items":    {
            "type": "array",
            "description": "Open issues and PRs (present when --items is passed)",
            "items": {
              "type": "object",
              "properties": {
                "type":       { "type": "string", "enum": ["issue", "pr"] },
                "number":     { "type": "integer" },
                "title":      { "type": "string" },
                "created_at": { "type": "string", "format": "date-time" },
                "html_url":   { "type": "string", "format": "uri" },
                "age_days":   { "type": "integer" }
              },
              "required": ["type", "number", "title", "created_at", "html_url", "age_days"]
            }
          },
          "latest_release_tag_name": { "type": "string" },
          "latest_release_published_at": { "type": "string", "format": "date-time" }
        }
      }
    },
    "stale_flags": {
      "type": "array",
      "description": "Staleness signals across the repos",
      "items": {
        "type": "object",
        "properties": {
          "kind":    { "type": "string", "enum": ["repo", "issue", "pr", "release-gap"] },
          "repo":    { "type": "string" },
          "detail":  { "type": "string" },
          "age_days": { "type": "integer" },
          "link":    { "type": "string", "format": "uri" }
        },
        "required": ["kind", "repo", "detail", "age_days"]
      }
    }
  },
  "required": ["owner", "generated", "repos"]
}
`

func main() {
	var (
		owner       = flag.String("owner", "", "GitHub user or org name (required)")
		format      = flag.String("format", "markdown", "Output format: markdown or json")
		staleDays   = flag.Int("stale-days", 30, "Days before an item is flagged stale")
		staleOnly   = flag.Bool("stale-only", false, "Show only stale items")
		includeArc  = flag.Bool("include-archived", false, "Include archived repos")
		maxRepos    = flag.Int("max-repos", 100, "Max repos to inspect")
		showVersion = flag.Bool("version", false, "Print version and exit")
		sinceFlag   = flag.String("since", "", "Include only repos pushed on or after this date (YYYY-MM-DD)")
		jsonSchema  = flag.Bool("json-schema", false, "Print JSON Schema for --format json output and exit")
		itemsFlag   = flag.Bool("items", false, "List open issues/PRs per repo (adds an Open items section / open_items in JSON)")
		failOnStale = flag.Bool("fail-on-stale", false, "Exit with status 2 if any stale signal is found (useful for CI)")
		sortBy      = flag.String("sort", "pushed", "Sort repos by: pushed (oldest first), name, stars, updated, issues")
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
	if *jsonSchema {
		fmt.Print(JSONSchema)
		return
	}
	var sinceTime time.Time
	if *sinceFlag != "" {
		t, err := time.Parse("2006-01-02", *sinceFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: --since expects a date in YYYY-MM-DD format, got %q\n", *sinceFlag)
			os.Exit(2)
		}
		sinceTime = t
	}
	if *owner == "" {
		flag.Usage()
		os.Exit(2)
	}
	switch *sortBy {
	case "pushed", "name", "stars", "updated", "issues":
	default:
		fmt.Fprintf(os.Stderr, "error: --sort must be one of pushed, name, stars, updated, issues (got %q)\n", *sortBy)
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
		if !sinceTime.IsZero() && r.PushedAt.Before(sinceTime) {
			continue
		}
		s := RepoSummary{Repo: r}
		items, err := client.ListOpenItems(*owner, r.Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: %s: %v\n", r.Name, err)
			continue
		}
		s.OpenIssues, s.OpenPRs = splitItems(items)
		if *itemsFlag {
			s.OpenItems = items
		}
		s.LatestRelease, err = client.LatestRelease(*owner, r.Name)
		if err != nil && !IsNotFound(err) {
			// Non-fatal: a repo with no releases is fine.
			s.LatestReleaseErr = err.Error()
		}
		summaries = append(summaries, s)
	}

	stale := FlagStale(summaries, cutoff)
	sorted := sortSummaries(summaries, *sortBy)

	switch *format {
	case "json":
		out, _ := json.MarshalIndent(struct {
			Owner     string        `json:"owner"`
			Generated time.Time     `json:"generated"`
			Repos     []RepoSummary `json:"repos"`
			Stale     []StaleFlag   `json:"stale_flags"`
		}{*owner, time.Now().UTC(), sorted, stale}, "", "  ")
		fmt.Println(string(out))
	case "markdown":
		fallthrough
	default:
		RenderMarkdown(os.Stdout, *owner, sorted, cutoff, *staleOnly, *itemsFlag, *sortBy)
	}
	if *failOnStale && len(stale) > 0 {
		fmt.Fprintf(os.Stderr, "fail-on-stale: %d stale signal(s) detected\n", len(stale))
		os.Exit(2)
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
	return sortSummaries(s, "pushed")
}

// sortSummaries returns a copy of s sorted by the requested key.
// Valid keys: pushed (oldest first), name (A-Z), stars (desc), updated (newest first), issues (most open issues first).
func sortSummaries(s []RepoSummary, sortBy string) []RepoSummary {
	out := append([]RepoSummary(nil), s...)
	sort.Slice(out, func(i, j int) bool {
		switch sortBy {
		case "name":
			return out[i].Repo.Name < out[j].Repo.Name
		case "stars":
			if out[i].Repo.Stargazers == out[j].Repo.Stargazers {
				return out[i].Repo.Name < out[j].Repo.Name
			}
			return out[i].Repo.Stargazers > out[j].Repo.Stargazers
		case "updated":
			if out[i].Repo.UpdatedAt.Equal(out[j].Repo.UpdatedAt) {
				return out[i].Repo.Name < out[j].Repo.Name
			}
			return out[i].Repo.UpdatedAt.After(out[j].Repo.UpdatedAt)
		case "issues":
			ai := out[i].OpenIssues + out[i].OpenPRs
			aj := out[j].OpenIssues + out[j].OpenPRs
			if ai == aj {
				return out[i].Repo.Name < out[j].Repo.Name
			}
			return ai > aj
		case "pushed":
			fallthrough
		default:
			ai := out[i].Repo.PushedAt
			aj := out[j].Repo.PushedAt
			if ai.Equal(aj) {
				return out[i].Repo.Name < out[j].Repo.Name
			}
			return ai.Before(aj)
		}
	})
	return out
}
