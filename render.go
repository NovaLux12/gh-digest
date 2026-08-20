package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// StaleFlag describes an item that has gone quiet past the stale threshold.
type StaleFlag struct {
	Kind    string // "repo", "issue", "pr", "release-gap"
	Repo    string
	Detail  string
	AgeDays int
	Link    string
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
func RenderMarkdown(w io.Writer, owner string, summaries []RepoSummary, cutoff time.Time, staleOnly, showItems bool, sortBy string) {
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

	// Open items — between stale signals and the repos table, only when
	// --items was requested (independent of --stale-only).
	if showItems {
		RenderOpenItems(w, summaries)
	}

	// Per-repo table — sorted by most-recently pushed (stale at bottom).
	// Per-repo table — sorted per --sort flag (pushed = oldest stale first).
	fmt.Fprintln(w, "## Repos")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| Repo | Pushed | Open issues / PRs | Latest release | Description |")
	fmt.Fprintln(w, "|---|---|---|---|---|")
	rows := sortSummaries(summaries, sortBy)
	if !staleOnly {
		for _, s := range rows {
			rel := "—"
			if s.LatestRelease != nil {
				rel = s.LatestRelease.TagName
			}
			desc := truncate(strings.TrimSpace(s.Repo.Description), 60)
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
			desc := truncate(strings.TrimSpace(s.Repo.Description), 60)
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

// RenderOpenItems writes the "Open items" section: one row per open issue or
// PR, sorted by repo name then age (oldest first). If no repo has open items
// the section header is skipped and only "_No open items._" is printed
// (matching the stale-signals style).
func RenderOpenItems(w io.Writer, summaries []RepoSummary) {
	type row struct {
		repo, repoURL, typ, title, itemURL string
		num                                int
		age                                int
	}
	var rows []row
	for _, s := range summaries {
		for _, it := range s.OpenItems {
			typ := it.Type
			if it.Type == "pr" {
				typ = "PR"
			}
			rows = append(rows, row{
				repo:    s.Repo.Name,
				repoURL: s.Repo.HTMLURL,
				typ:     typ,
				num:     it.Number,
				title:   truncate(it.Title, 60),
				itemURL: it.HTMLURL,
				age:     it.AgeDays,
			})
		}
	}
	if len(rows) == 0 {
		fmt.Fprintln(w, "_No open items._")
		fmt.Fprintln(w)
		return
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].repo != rows[j].repo {
			return rows[i].repo < rows[j].repo
		}
		return rows[i].age > rows[j].age // oldest first
	})
	fmt.Fprintln(w, "## Open items")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| Repo | Type | # | Title | Age |")
	fmt.Fprintln(w, "|---|---|---|---|---|")
	for _, r := range rows {
		fmt.Fprintf(w, "| [%s](%s) | %s | [%d](%s) | %s | %dd |\n",
			r.repo, r.repoURL, r.typ, r.num, r.itemURL, r.title, r.age)
	}
	fmt.Fprintln(w)
}

// truncate shortens s to at most max characters (runes), appending "..."
// when it is cut.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-3]) + "..."
}
