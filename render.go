package main

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// StaleFlag describes an item that has gone quiet past the stale threshold.
type StaleFlag struct {
	Kind     string // "repo", "issue", "pr", "release-gap"
	Repo     string
	Detail   string
	AgeDays  int
	Link     string
}

// FlagStale returns the list of staleness signals across all repo summaries.
func FlagStale(summaries []RepoSummary, cutoff time.Time) []StaleFlag {
	var out []StaleFlag
	now := time.Now()
	for _, s := range summaries {
		agePush := int(now.Sub(s.Repo.PushedAt).Hours() / 24)
		if s.Repo.PushedAt.Before(cutoff) {
			out = append(out, StaleFlag{
				Kind:    "repo",
				Repo:    s.Repo.FullName,
				Detail:  fmt.Sprintf("no commits pushed in %d days", agePush),
				AgeDays: agePush,
				Link:    s.Repo.HTMLURL,
			})
		}
		// release gap: if repo has releases but latest is older than 2* cutoff
		if s.LatestRelease != nil {
			ageRel := int(now.Sub(s.LatestRelease.PublishedAt).Hours() / 24)
			if s.LatestRelease.PublishedAt.Before(cutoff.AddDate(0, 0, -30)) {
				out = append(out, StaleFlag{
					Kind:    "release-gap",
					Repo:    s.Repo.FullName,
					Detail:  fmt.Sprintf("latest release %s is %d days old", s.LatestRelease.TagName, ageRel),
					AgeDays: ageRel,
					Link:    s.LatestRelease.HTMLURL,
				})
			}
		}
	}
	return out
}

// RenderMarkdown writes a human-readable digest to w.
func RenderMarkdown(w io.Writer, owner string, summaries []RepoSummary, cutoff time.Time, staleOnly bool) {
	fmt.Fprintf(w, "# GitHub digest — %s\n\n", owner)
	fmt.Fprintf(w, "_Generated %s_\n\n", time.Now().Format("2006-01-02 15:04 UTC"))

	if len(summaries) == 0 {
		fmt.Fprintln(w, "_No repos found (or all filtered out by --include-archived=false)._")
		return
	}

	// Header table.
	totalIssues, totalPRs, withRel := 0, 0, 0
	for _, s := range summaries {
		totalIssues += s.OpenIssues
		totalPRs += s.OpenPRs
		if s.LatestRelease != nil {
			withRel++
		}
	}
	fmt.Fprintf(w, "%d repos · %s · %s · %d/%d have a release\n\n",
		len(summaries), plural(totalIssues, "open issue", "open issues"),
		plural(totalPRs, "open PR", "open PRs"), withRel, len(summaries))

	// Stale flags (always shown).
	stale := FlagStale(summaries, cutoff)
	if len(stale) > 0 {
		fmt.Fprintln(w, "## Stale signals")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "| Type | Repo | Detail | Age |")
		fmt.Fprintln(w, "|---|---|---|---|")
		for _, f := range stale {
			fmt.Fprintf(w, "| %s | [%s](%s) | %s | %dd |\n", f.Kind, f.Repo, f.Link, f.Detail, f.AgeDays)
		}
		fmt.Fprintln(w)
	} else {
		fmt.Fprintln(w, "_No stale signals._")
		fmt.Fprintln(w)
	}

	// Per-repo table — sorted by most-recently pushed (stale at bottom).
	fmt.Fprintln(w, "## Repos")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| Repo | Pushed | Open issues / PRs | Latest release | Description |")
	fmt.Fprintln(w, "|---|---|---|---|---|")
	rows := sortByStaleness(summaries)
	if !staleOnly {
		for _, s := range rows {
			rel := "—"
			if s.LatestRelease != nil {
				rel = s.LatestRelease.TagName
			}
			desc := strings.TrimSpace(s.Repo.Description)
			if len(desc) > 60 {
				desc = desc[:57] + "..."
			}
			fmt.Fprintf(w, "| [%s](%s) | %s | %d / %d | %s | %s |\n",
				s.Repo.Name, s.Repo.HTMLURL,
				s.Repo.PushedAt.Format("2006-01-02"),
				s.OpenIssues, s.OpenPRs,
				rel, desc,
			)
		}
	} else {
		for _, s := range rows {
			if !s.Repo.PushedAt.Before(cutoff) {
				continue
			}
			rel := "—"
			if s.LatestRelease != nil {
				rel = s.LatestRelease.TagName
			}
			desc := strings.TrimSpace(s.Repo.Description)
			if len(desc) > 60 {
				desc = desc[:57] + "..."
			}
			fmt.Fprintf(w, "| [%s](%s) | %s | %d / %d | %s | %s |\n",
				s.Repo.Name, s.Repo.HTMLURL,
				s.Repo.PushedAt.Format("2006-01-02"),
				s.OpenIssues, s.OpenPRs,
				rel, desc,
			)
		}
	}
	fmt.Fprintln(w)
}