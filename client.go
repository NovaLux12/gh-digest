package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Repo is the subset of GitHub's repo object we care about.
type Repo struct {
	Name        string    `json:"name"`
	FullName    string    `json:"full_name"`
	Description string    `json:"description"`
	Private     bool      `json:"private"`
	Archived    bool      `json:"archived"`
	Visibility  string    `json:"visibility"`
	PushedAt    time.Time `json:"pushed_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	HTMLURL     string    `json:"html_url"`
	Stargazers  int       `json:"stargazers_count"`
	OpenIssues  int       `json:"open_issues_count"` // includes PRs per GitHub convention
}

// Release is the subset of GitHub's release object we display.
type Release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
}

// Item is one open issue or PR fetched from /repos/{owner}/{repo}/issues.
// Type is "issue" or "pr" (GitHub convention: the pull_request field is
// present only on PRs); AgeDays is computed from CreatedAt at fetch time.
type Item struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	HTMLURL   string    `json:"html_url"`
	Type      string    `json:"type"`     // "issue" | "pr"
	AgeDays   int       `json:"age_days"` // days since CreatedAt
}

// RepoSummary aggregates the per-repo data after we've collected it.
type RepoSummary struct {
	Repo             Repo
	OpenIssues       int
	OpenPRs          int
	OpenItems        []Item `json:"open_items,omitempty"`
	LatestRelease    *Release
	LatestReleaseErr string
}

// Client wraps http.Client with auth and base URL.
type Client struct {
	HTTP   *http.Client
	Token  string
	APIURL string // e.g. https://api.github.com
}

// NewClient returns a Client configured for the GitHub REST API.
func NewClient(token string) *Client {
	return &Client{
		HTTP:   &http.Client{Timeout: 30 * time.Second},
		Token:  token,
		APIURL: "https://api.github.com",
	}
}

// get performs an authenticated GET and decodes JSON into out.
// Returns a typed error for 404 (IsNotFound) and a generic error otherwise.
func (c *Client) get(path string, out any, query url.Values) error {
	u := c.APIURL + path
	if query != nil && len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	req.Header.Set("User-Agent", "gh-digest/0.1")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return &NotFoundError{Path: path}
	}
	if resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0" {
		return fmt.Errorf("rate limited — set GH_TOKEN for higher quota")
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("GET %s: %d %s: %s", path, resp.StatusCode, resp.Status, strings.TrimSpace(string(body)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// NotFoundError is returned by Client.get for 404 responses.
type NotFoundError struct{ Path string }

func (e *NotFoundError) Error() string { return "not found: " + e.Path }

// IsNotFound reports whether err is or wraps a NotFoundError.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := err.(*NotFoundError); ok {
		return true
	}
	return false
}

// ListRepos returns up to maxRepos non-fork repos for the owner.
// Uses the /users/{owner}/repos endpoint with pagination.
func (c *Client) ListRepos(owner string, maxRepos int) ([]Repo, error) {
	var all []Repo
	page := 1
	for len(all) < maxRepos {
		var batch []Repo
		q := url.Values{}
		q.Set("per_page", "100")
		q.Set("page", fmt.Sprintf("%d", page))
		q.Set("type", "owner") // exclude forks
		q.Set("sort", "pushed")
		err := c.get("/users/"+owner+"/repos", &batch, q)
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}
		all = append(all, batch...)
		page++
		if page > 5 { // hard cap at 5 pages = 500 repos
			break
		}
	}
	if len(all) > maxRepos {
		all = all[:maxRepos]
	}
	return all, nil
}

// ListOpenItems returns the open issues and PRs for a repo. GitHub's
// /issues endpoint returns both in one payload; PRs are identified by the
// presence of a pull_request field (GitHub convention).
func (c *Client) ListOpenItems(owner, repo string) ([]Item, error) {
	var raw []json.RawMessage
	q := url.Values{}
	q.Set("state", "open")
	q.Set("per_page", "100")
	err := c.get(fmt.Sprintf("/repos/%s/%s/issues", owner, repo), &raw, q)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	items := make([]Item, 0, len(raw))
	for _, r := range raw {
		var probe struct {
			Number      int              `json:"number"`
			Title       string           `json:"title"`
			CreatedAt   time.Time        `json:"created_at"`
			HTMLURL     string           `json:"html_url"`
			PullRequest *json.RawMessage `json:"pull_request"`
		}
		if err := json.Unmarshal(r, &probe); err != nil {
			continue
		}
		it := Item{
			Number:    probe.Number,
			Title:     probe.Title,
			CreatedAt: probe.CreatedAt,
			HTMLURL:   probe.HTMLURL,
			AgeDays:   ageDays(probe.CreatedAt, now),
		}
		if probe.PullRequest != nil {
			it.Type = "pr"
		} else {
			it.Type = "issue"
		}
		items = append(items, it)
	}
	return items, nil
}

// CountOpenIssuesAndPRs returns (open issues, open PRs) for a repo, derived
// from the same single /issues fetch used by ListOpenItems (no double-fetch).
func (c *Client) CountOpenIssuesAndPRs(owner, repo string) (int, int, error) {
	items, err := c.ListOpenItems(owner, repo)
	if err != nil {
		return 0, 0, err
	}
	issues, prs := splitItems(items)
	return issues, prs, nil
}

// splitItems classifies a set of open items into (issues, PRs).
func splitItems(items []Item) (issues, prs int) {
	for _, it := range items {
		if it.Type == "pr" {
			prs++
		} else {
			issues++
		}
	}
	return issues, prs
}

// ageDays returns the whole days elapsed between createdAt and now.
// Future timestamps clamp to 0.
func ageDays(createdAt, now time.Time) int {
	d := int(now.Sub(createdAt).Hours() / 24)
	if d < 0 {
		return 0
	}
	return d
}

// LatestRelease returns the most recent non-draft release, or nil if the repo
// has no releases.
func (c *Client) LatestRelease(owner, repo string) (*Release, error) {
	var r Release
	err := c.get(fmt.Sprintf("/repos/%s/%s/releases/latest", owner, repo), &r, nil)
	if err != nil {
		if IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}
