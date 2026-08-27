package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNotFoundError(t *testing.T) {
	err := &NotFoundError{Path: "/repos/foo/bar"}
	if !IsNotFound(err) {
		t.Error("IsNotFound should return true for *NotFoundError")
	}
	if IsNotFound(nil) {
		t.Error("IsNotFound(nil) should be false")
	}
	if IsNotFound(errOther("oops")) {
		t.Error("IsNotFound(other) should be false")
	}
}

type errOther string

func (e errOther) Error() string { return string(e) }

func TestPlural(t *testing.T) {
	cases := []struct {
		n    int
		sing string
		plur string
		want string
	}{
		{0, "issue", "issues", "0 issues"},
		{1, "issue", "issues", "1 issue"},
		{5, "PR", "PRs", "5 PRs"},
	}
	for _, c := range cases {
		got := plural(c.n, c.sing, c.plur)
		if got != c.want {
			t.Errorf("plural(%d, %q, %q) = %q, want %q", c.n, c.sing, c.plur, got, c.want)
		}
	}
}

func TestPad(t *testing.T) {
	if got := pad("ab", 5); got != "ab   " {
		t.Errorf("pad short: got %q", got)
	}
	if got := pad("hello", 3); got != "hello" {
		t.Errorf("pad long: got %q", got)
	}
}

func TestFlagStale(t *testing.T) {
	now := time.Now()
	cutoff := now.AddDate(0, 0, -30) // 30 days ago
	oldPush := cutoff.AddDate(0, 0, -5)
	freshPush := now.AddDate(0, 0, -2)

	summaries := []RepoSummary{
		{
			Repo: Repo{
				FullName: "owner/old",
				PushedAt: oldPush,
				HTMLURL:  "https://example.com/old",
			},
		},
		{
			Repo: Repo{
				FullName: "owner/fresh",
				PushedAt: freshPush,
				HTMLURL:  "https://example.com/fresh",
			},
			LatestRelease: &Release{
				TagName:     "v0.1.0",
				PublishedAt: now.AddDate(0, 0, -3),
				HTMLURL:     "https://example.com/release",
			},
		},
	}
	got := FlagStale(summaries, cutoff)
	if len(got) != 1 {
		t.Fatalf("expected 1 stale flag, got %d: %+v", len(got), got)
	}
	if got[0].Kind != "repo" || got[0].Repo != "owner/old" {
		t.Errorf("unexpected flag: %+v", got[0])
	}
}

func TestSortByStaleness(t *testing.T) {
	now := time.Now()
	summaries := []RepoSummary{
		{Repo: Repo{Name: "new", PushedAt: now.AddDate(0, 0, -1)}},
		{Repo: Repo{Name: "oldest", PushedAt: now.AddDate(0, 0, -30)}},
		{Repo: Repo{Name: "older", PushedAt: now.AddDate(0, 0, -10)}},
	}
	got := sortByStaleness(summaries)
	if got[0].Repo.Name != "oldest" || got[1].Repo.Name != "older" || got[2].Repo.Name != "new" {
		t.Errorf("sort order: %+v", got)
	}
}

func TestRenderMarkdownIncludesOwner(t *testing.T) {
	var sb strings.Builder
	summaries := []RepoSummary{{Repo: Repo{Name: "demo", FullName: "owner/demo", HTMLURL: "https://x", PushedAt: time.Now()}}}
	RenderMarkdown(&sb, "owner", summaries, time.Now().AddDate(0, 0, -30), false, false, "pushed")
	out := sb.String()
	if !strings.Contains(out, "# GitHub digest — owner") {
		t.Error("missing digest header")
	}
	if !strings.Contains(out, "[demo](https://x)") {
		t.Error("missing repo link in table")
	}
}

func TestSortSummaries(t *testing.T) {
	now := time.Now()
	summaries := []RepoSummary{
		{Repo: Repo{Name: "beta", PushedAt: now.AddDate(0, 0, -1), UpdatedAt: now.AddDate(0, 0, -1), Stargazers: 5}, OpenIssues: 1, OpenPRs: 1},
		{Repo: Repo{Name: "alpha", PushedAt: now.AddDate(0, 0, -10), UpdatedAt: now.AddDate(0, 0, -5), Stargazers: 10}, OpenIssues: 3, OpenPRs: 0},
		{Repo: Repo{Name: "gamma", PushedAt: now.AddDate(0, 0, -5), UpdatedAt: now.AddDate(0, 0, -2), Stargazers: 20}, OpenIssues: 0, OpenPRs: 0},
	}
	// name
	got := sortSummaries(summaries, "name")
	if got[0].Repo.Name != "alpha" || got[1].Repo.Name != "beta" || got[2].Repo.Name != "gamma" {
		t.Errorf("sort name: got %v", []string{got[0].Repo.Name, got[1].Repo.Name, got[2].Repo.Name})
	}
	// stars desc
	got = sortSummaries(summaries, "stars")
	if got[0].Repo.Name != "gamma" || got[1].Repo.Name != "alpha" || got[2].Repo.Name != "beta" {
		t.Errorf("sort stars: got %v", []string{got[0].Repo.Name, got[1].Repo.Name, got[2].Repo.Name})
	}
	// pushed oldest first
	got = sortSummaries(summaries, "pushed")
	if got[0].Repo.Name != "alpha" || got[2].Repo.Name != "beta" {
		t.Errorf("sort pushed: got %v", []string{got[0].Repo.Name, got[1].Repo.Name, got[2].Repo.Name})
	}
	// updated newest first
	got = sortSummaries(summaries, "updated")
	if got[0].Repo.Name != "beta" || got[1].Repo.Name != "gamma" || got[2].Repo.Name != "alpha" {
		t.Errorf("sort updated: got %v", []string{got[0].Repo.Name, got[1].Repo.Name, got[2].Repo.Name})
	}
	// issues most first
	got = sortSummaries(summaries, "issues")
	if got[0].Repo.Name != "alpha" || got[1].Repo.Name != "beta" || got[2].Repo.Name != "gamma" {
		t.Errorf("sort issues: got %v", []string{got[0].Repo.Name, got[1].Repo.Name, got[2].Repo.Name})
	}
	// unknown defaults to pushed
	got = sortSummaries(summaries, "bogus")
	if got[0].Repo.Name != "alpha" {
		t.Errorf("sort unknown should default to pushed, got %v", got[0].Repo.Name)
	}
}

func TestFailOnStaleDetection(t *testing.T) {
	now := time.Now()
	cutoff := now.AddDate(0, 0, -30)
	oldPush := cutoff.AddDate(0, 0, -5)
	freshPush := now.AddDate(0, 0, -2)
	summaries := []RepoSummary{
		{Repo: Repo{FullName: "owner/old", PushedAt: oldPush, HTMLURL: "https://x/old"}},
		{Repo: Repo{FullName: "owner/fresh", PushedAt: freshPush, HTMLURL: "https://x/fresh"}},
	}
	stale := FlagStale(summaries, cutoff)
	if len(stale) == 0 {
		t.Fatal("expected stale signals for old repo")
	}
	// Simulate --fail-on-stale logic: should trigger when stale non-empty
	failOnStale := true
	shouldFail := failOnStale && len(stale) > 0
	if !shouldFail {
		t.Error("expected fail-on-stale to trigger")
	}
	// No stale -> should not fail
	summaries2 := []RepoSummary{{Repo: Repo{FullName: "owner/fresh", PushedAt: freshPush, HTMLURL: "https://x/fresh"}}}
	stale2 := FlagStale(summaries2, cutoff)
	if len(stale2) != 0 {
		t.Fatalf("expected no stale, got %d", len(stale2))
	}
	shouldFail2 := failOnStale && len(stale2) > 0
	if shouldFail2 {
		t.Error("should not fail when no stale signals")
	}
}

func TestRenderMarkdownSort(t *testing.T) {
	now := time.Now()
	summaries := []RepoSummary{
		{Repo: Repo{Name: "beta", FullName: "owner/beta", HTMLURL: "https://x/beta", PushedAt: now.AddDate(0, 0, -1)}},
		{Repo: Repo{Name: "alpha", FullName: "owner/alpha", HTMLURL: "https://x/alpha", PushedAt: now.AddDate(0, 0, -10)}},
	}
	var sb strings.Builder
	RenderMarkdown(&sb, "owner", summaries, now.AddDate(0, 0, -30), false, false, "name")
	out := sb.String()
	alphaIdx := strings.Index(out, "alpha")
	betaIdx := strings.Index(out, "beta")
	if alphaIdx == -1 || betaIdx == -1 {
		t.Fatalf("missing repos in output: %q", out)
	}
	if alphaIdx > betaIdx {
		t.Error("expected alpha before beta when sorted by name")
	}
}

func TestVersionFlag(t *testing.T) {
	// Compile-time guard: the version constant should be non-empty so the
	// --version flag prints something useful.
	if version == "" {
		t.Error("version constant is empty")
	}
}

func TestSince(t *testing.T) {
	// Simulate the --since filtering: repos pushed before the given date are excluded.
	// We test the filtering logic directly since main() requires flag parsing.
	now := time.Now()
	// sinceTime = 30 days ago; repos: 2 days ago (included), 10 days ago (included), 60 days ago (excluded)
	sinceTime := now.AddDate(0, 0, -30)
	repos := []Repo{
		{Name: "recent", PushedAt: now.AddDate(0, 0, -2)}, // 2 days ago — included
		{Name: "mid", PushedAt: now.AddDate(0, 0, -10)},   // 10 days ago — included
		{Name: "old", PushedAt: now.AddDate(0, 0, -60)},   // 60 days ago — excluded
	}

	var included []string
	for _, r := range repos {
		if r.PushedAt.Before(sinceTime) {
			continue
		}
		included = append(included, r.Name)
	}
	if len(included) != 2 {
		t.Errorf("expected 2 repos included (recent, mid), got %d: %v", len(included), included)
	}
	foundOld := false
	for _, name := range included {
		if name == "old" {
			foundOld = true
		}
	}
	if foundOld {
		t.Error("'old' repo should have been excluded by --since filter")
	}
}

func TestJSONSchema(t *testing.T) {
	// Verify the JSONSchema constant is valid JSON and contains expected fields.
	var v map[string]interface{}
	if err := json.Unmarshal([]byte(JSONSchema), &v); err != nil {
		t.Fatalf("JSONSchema is not valid JSON: %v", err)
	}
	if v["type"] != "object" {
		t.Errorf("expected type=object, got %v", v["type"])
	}
	props, ok := v["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("missing or invalid properties field")
	}
	for _, field := range []string{"owner", "generated", "repos"} {
		if _, ok := props[field]; !ok {
			t.Errorf("missing required property %q in schema", field)
		}
	}
	staleFlags, ok := props["stale_flags"]
	if !ok {
		t.Error("missing stale_flags property")
	} else {
		sfProps := staleFlags.(map[string]interface{})["items"].(map[string]interface{})["properties"].(map[string]interface{})
		if _, ok := sfProps["kind"]; !ok {
			t.Error("missing kind in stale_flags items")
		}
	}
	// repos items must declare open_items with the required item fields
	reposItems := props["repos"].(map[string]interface{})["items"].(map[string]interface{})
	repoProps := reposItems["properties"].(map[string]interface{})
	oi, ok := repoProps["open_items"]
	if !ok {
		t.Fatal("missing open_items property in repos items schema")
	}
	oiItems := oi.(map[string]interface{})["items"].(map[string]interface{})
	oiProps := oiItems["properties"].(map[string]interface{})
	for _, field := range []string{"type", "number", "title", "html_url", "created_at", "age_days"} {
		if _, ok := oiProps[field]; !ok {
			t.Errorf("missing open_items field %q in schema", field)
		}
	}
	required, ok := oiItems["required"].([]interface{})
	if !ok {
		t.Fatal("open_items items missing required array")
	}
	for _, field := range []string{"type", "number", "title", "html_url", "created_at", "age_days"} {
		found := false
		for _, r := range required {
			if r == field {
				found = true
			}
		}
		if !found {
			t.Errorf("open_items field %q not in required list", field)
		}
	}
	// enum must allow exactly issue/pr
	enum := oiProps["type"].(map[string]interface{})["enum"].([]interface{})
	if len(enum) != 2 || enum[0] != "issue" || enum[1] != "pr" {
		t.Errorf("type enum = %v, want [issue pr]", enum)
	}
}
func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"", 60, ""},
		{"short", 60, "short"},
		{strings.Repeat("a", 60), 60, strings.Repeat("a", 60)},
		{strings.Repeat("a", 61), 60, strings.Repeat("a", 57) + "..."},
		{strings.Repeat("a", 100), 60, strings.Repeat("a", 57) + "..."},
		// rune-aware: 21 emoji = 84 bytes but 21 runes → no truncation, no corruption
		{"🔔" + strings.Repeat("a", 59), 60, "🔔" + strings.Repeat("a", 59)},
	}
	for _, c := range cases {
		if got := truncate(c.in, c.max); got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
	}
}

func TestAgeDays(t *testing.T) {
	now := time.Date(2026, 8, 18, 15, 0, 0, 0, time.UTC)
	cases := []struct {
		created time.Time
		want    int
	}{
		{now.AddDate(0, 0, -12), 12},
		{now.Add(-24 * time.Hour), 1},
		{now.Add(-36 * time.Hour), 1},   // floor of 1.5 days
		{now.Add(-25 * time.Hour), 1},   // 25h → 1 day
		{now.Add(-10 * time.Minute), 0}, // under a day → 0
		{now, 0},
		{now.Add(2 * time.Hour), 0}, // future timestamps clamp to 0
	}
	for _, c := range cases {
		if got := ageDays(c.created, now); got != c.want {
			t.Errorf("ageDays(%v, %v) = %d, want %d", c.created, now, got, c.want)
		}
	}
}

// issueServer serves canned /issues payloads for owner/demo.
func issueServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/owner/demo/issues":
			if q := r.URL.Query().Get("state"); q != "open" {
				t.Errorf("expected state=open, got %q", q)
			}
			fmt.Fprint(w, `[
				{"number": 3, "title": "Fix BLE reconnect", "created_at": "2026-08-06T10:00:00Z", "html_url": "https://github.com/NovaLux12/demo/issues/3"},
				{"number": 5, "title": "Bump deps", "created_at": "2026-08-15T10:00:00Z", "html_url": "https://github.com/NovaLux12/demo/pull/5", "pull_request": {"url": "https://api.github.com/repos/NovaLux12/demo/pulls/5"}}
			]`)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestListOpenItemsClassification(t *testing.T) {
	srv := issueServer(t)
	defer srv.Close()
	c := NewClient("")
	c.APIURL = srv.URL

	items, err := c.ListOpenItems("owner", "demo")
	if err != nil {
		t.Fatalf("ListOpenItems: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d: %+v", len(items), items)
	}
	// item 1: plain issue
	if items[0].Number != 3 || items[0].Type != "issue" {
		t.Errorf("item[0] = %+v, want #3 type=issue", items[0])
	}
	if items[0].Title != "Fix BLE reconnect" || items[0].HTMLURL != "https://github.com/NovaLux12/demo/issues/3" {
		t.Errorf("item[0] fields wrong: %+v", items[0])
	}
	// item 2: PR via pull_request field
	if items[1].Number != 5 || items[1].Type != "pr" {
		t.Errorf("item[1] = %+v, want #5 type=pr", items[1])
	}
	// age matches the ageDays computation against the same clock
	for _, it := range items {
		if it.AgeDays != ageDays(it.CreatedAt, time.Now()) {
			t.Errorf("item #%d AgeDays = %d, want %d", it.Number, it.AgeDays, ageDays(it.CreatedAt, time.Now()))
		}
	}
	// Item JSON marshals per spec keys
	b, err := json.Marshal(items[0])
	if err != nil {
		t.Fatal(err)
	}
	var probe map[string]interface{}
	if err := json.Unmarshal(b, &probe); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"type", "number", "title", "created_at", "html_url", "age_days"} {
		if _, ok := probe[k]; !ok {
			t.Errorf("marshalled item missing key %q: %s", k, b)
		}
	}
}

func TestCountOpenIssuesAndPRsDerivesFromSamePayload(t *testing.T) {
	srv := issueServer(t)
	defer srv.Close()
	c := NewClient("")
	c.APIURL = srv.URL

	// Counts must equal what ListOpenItems returns (single fetch, no double call).
	issues, prs, err := c.CountOpenIssuesAndPRs("owner", "demo")
	if err != nil {
		t.Fatalf("CountOpenIssuesAndPRs: %v", err)
	}
	if issues != 1 || prs != 1 {
		t.Errorf("got issues=%d prs=%d, want 1/1", issues, prs)
	}
	items, err := c.ListOpenItems("owner", "demo")
	if err != nil {
		t.Fatal(err)
	}
	gi, gp := splitItems(items)
	if gi != issues || gp != prs {
		t.Errorf("splitItems mismatch: (%d,%d) != (%d,%d)", gi, gp, issues, prs)
	}
}

func TestRenderOpenItems(t *testing.T) {
	now := time.Now()
	summaries := []RepoSummary{
		{
			Repo: Repo{Name: "zeta", HTMLURL: "https://github.com/NovaLux12/zeta"},
			OpenItems: []Item{
				{Number: 1, Title: "Newer issue", CreatedAt: now.AddDate(0, 0, -3), HTMLURL: "https://github.com/NovaLux12/zeta/issues/1", Type: "issue", AgeDays: 3},
				{Number: 2, Title: "Older PR", CreatedAt: now.AddDate(0, 0, -12), HTMLURL: "https://github.com/NovaLux12/zeta/pull/2", Type: "pr", AgeDays: 12},
			},
		},
		{
			Repo: Repo{Name: "alpha", HTMLURL: "https://github.com/NovaLux12/alpha"},
			OpenItems: []Item{
				{Number: 7, Title: "Alpha issue", CreatedAt: now.AddDate(0, 0, -1), HTMLURL: "https://github.com/NovaLux12/alpha/issues/7", Type: "issue", AgeDays: 1},
			},
		},
	}
	var sb strings.Builder
	RenderOpenItems(&sb, summaries)
	out := sb.String()

	for _, want := range []string{"## Open items", "| Repo | Type | # | Title | Age |"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
	// repo-name sort: alpha before zeta
	ai, zi := strings.Index(out, "[alpha]("), strings.Index(out, "[zeta](")
	if ai == -1 || zi == -1 || ai > zi {
		t.Errorf("expected alpha before zeta (repo-name sort):\n%s", out)
	}
	// oldest first within a repo: #2 (12d) before #1 (3d)
	oi, ni := strings.Index(out, "[2]("), strings.Index(out, "[1](")
	if oi == -1 || ni == -1 || oi > ni {
		t.Errorf("expected older item #2 before newer #1 within zeta:\n%s", out)
	}
	// type labels: issue lowercase, PR uppercase
	if !strings.Contains(out, "| issue |") || !strings.Contains(out, "| PR |") {
		t.Errorf("expected issue and PR type labels:\n%s", out)
	}
	// age column
	if !strings.Contains(out, "| 12d |") || !strings.Contains(out, "| 1d |") {
		t.Errorf("expected age cells 12d and 1d:\n%s", out)
	}
}

func TestRenderOpenItemsEmpty(t *testing.T) {
	var sb strings.Builder
	RenderOpenItems(&sb, []RepoSummary{{Repo: Repo{Name: "empty"}}})
	out := sb.String()
	if !strings.Contains(out, "_No open items._") {
		t.Errorf("expected no-items line, got:\n%s", out)
	}
	if strings.Contains(out, "## Open items") {
		t.Errorf("section header must be skipped when there are no open items:\n%s", out)
	}
}

func TestRenderMarkdownItemsGating(t *testing.T) {
	now := time.Now()
	summaries := []RepoSummary{
		{
			Repo:       Repo{Name: "demo", FullName: "owner/demo", HTMLURL: "https://github.com/NovaLux12/demo", PushedAt: now.AddDate(0, 0, -1)},
			OpenIssues: 1,
			OpenItems: []Item{
				{Number: 3, Title: "Fix BLE reconnect", CreatedAt: now.AddDate(0, 0, -12), HTMLURL: "https://github.com/NovaLux12/demo/issues/3", Type: "issue", AgeDays: 12},
			},
		},
	}
	cutoff := now.AddDate(0, 0, -30)
	var withSB, withoutSB strings.Builder
	RenderMarkdown(&withSB, "owner", summaries, cutoff, false, true, "pushed")
	RenderMarkdown(&withoutSB, "owner", summaries, cutoff, false, false, "pushed")

	withOut := withSB.String()
	if !strings.Contains(withOut, "## Open items") {
		t.Errorf("expected Open items section when showItems=true:\n%s", withOut)
	}
	// section sits between the stale block and the repos table
	si := strings.Index(withOut, "_No stale signals._")
	oi, ri := strings.Index(withOut, "## Open items"), strings.Index(withOut, "## Repos")
	if si == -1 || oi == -1 || ri == -1 || !(si < oi && oi < ri) {
		t.Errorf("section order wrong (stale=%d items=%d repos=%d):\n%s", si, oi, ri, withOut)
	}
	withoutOut := withoutSB.String()
	if strings.Contains(withoutOut, "## Open items") || strings.Contains(withoutOut, "_No open items._") {
		t.Errorf("items section must be absent when showItems=false:\n%s", withoutOut)
	}
	if !strings.Contains(withoutOut, "## Repos") {
		t.Errorf("repos section missing:\n%s", withoutOut)
	}
}

func TestRenderMarkdownTitleTruncation(t *testing.T) {
	var sb strings.Builder
	long := strings.Repeat("x", 80)
	now := time.Now()
	summaries := []RepoSummary{{
		Repo:      Repo{Name: "demo", HTMLURL: "https://github.com/NovaLux12/demo", PushedAt: now},
		OpenItems: []Item{{Number: 1, Title: long, CreatedAt: now.AddDate(0, 0, -2), HTMLURL: "https://github.com/NovaLux12/demo/issues/1", Type: "issue", AgeDays: 2}},
	}}
	RenderOpenItems(&sb, summaries)
	out := sb.String()
	if !strings.Contains(out, strings.Repeat("x", 57)+"...") {
		t.Errorf("expected 57 chars + ... in output:\n%s", out)
	}
	if strings.Contains(out, strings.Repeat("x", 80)) {
		t.Errorf("raw long title leaked into markdown:\n%s", out)
	}
}

// TestJSONOutputMatchesSchema marshals the JSON view types and asserts every
// emitted key is declared in the JSONSchema constant (regression guard for
// the v0.3.1 PascalCase output bug).
func TestJSONOutputMatchesSchema(t *testing.T) {
	now := time.Now()
	s := RepoSummary{
		Repo:          Repo{Name: "demo", FullName: "owner/demo", Description: "d", Private: false, Archived: false, Visibility: "public", PushedAt: now, UpdatedAt: now, HTMLURL: "https://github.com/owner/demo", Stargazers: 1, OpenIssues: 2},
		OpenIssues:    1,
		OpenPRs:       2,
		OpenItems:     []Item{{Number: 3, Title: "t", CreatedAt: now, HTMLURL: "https://github.com/owner/demo/issues/3", Type: "issue", AgeDays: 4}},
		LatestRelease: &Release{TagName: "v1.0.0", PublishedAt: now},
	}
	b, err := json.Marshal(newRepoJSON(s))
	if err != nil {
		t.Fatalf("marshal repoJSON: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var schema map[string]interface{}
	if err := json.Unmarshal([]byte(JSONSchema), &schema); err != nil {
		t.Fatalf("JSONSchema invalid: %v", err)
	}
	repoProps := schema["properties"].(map[string]interface{})["repos"].(map[string]interface{})["items"].(map[string]interface{})["properties"].(map[string]interface{})
	for k := range got {
		if _, ok := repoProps[k]; !ok {
			t.Errorf("repoJSON emits key %q which is not declared in JSONSchema repos.items.properties", k)
		}
	}
	for _, want := range []string{"name", "full_name", "pushed_at", "open_issues", "open_prs", "latest_release_tag_name", "latest_release_published_at"} {
		if _, ok := got[want]; !ok {
			t.Errorf("repoJSON missing documented key %q", want)
		}
	}
	// StaleFlag keys must match the stale_flags schema too.
	sb, err := json.Marshal(StaleFlag{Kind: "repo", Repo: "owner/demo", Detail: "quiet", AgeDays: 31, Link: "https://github.com/owner/demo"})
	if err != nil {
		t.Fatalf("marshal StaleFlag: %v", err)
	}
	var sgot map[string]interface{}
	if err := json.Unmarshal(sb, &sgot); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	staleProps := schema["properties"].(map[string]interface{})["stale_flags"].(map[string]interface{})["items"].(map[string]interface{})["properties"].(map[string]interface{})
	for k := range sgot {
		if _, ok := staleProps[k]; !ok {
			t.Errorf("StaleFlag emits key %q which is not declared in JSONSchema stale_flags.items.properties", k)
		}
	}
	for _, want := range []string{"kind", "repo", "detail", "age_days"} {
		if _, ok := sgot[want]; !ok {
			t.Errorf("StaleFlag missing documented key %q", want)
		}
	}
}
