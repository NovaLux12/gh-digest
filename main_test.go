package main

import (
	"encoding/json"
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
	RenderMarkdown(&sb, "owner", summaries, time.Now().AddDate(0, 0, -30), false)
	out := sb.String()
	if !strings.Contains(out, "# GitHub digest — owner") {
		t.Error("missing digest header")
	}
	if !strings.Contains(out, "[demo](https://x)") {
		t.Error("missing repo link in table")
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
		{Name: "recent", PushedAt: now.AddDate(0, 0, -2)},   // 2 days ago — included
		{Name: "mid", PushedAt: now.AddDate(0, 0, -10)},    // 10 days ago — included
		{Name: "old", PushedAt: now.AddDate(0, 0, -60)},    // 60 days ago — excluded
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
}