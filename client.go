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

// IssueLite is the minimum we need for stale detection on issues / PRs.
type IssueLite struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	HTMLURL   string    `json:"html_url"`
	IsPR      bool      `json:"-"` // derived
}

// RepoSummary aggregates the per-repo data after we've collected it.
type RepoSummary struct {
	Repo            Repo
	OpenIssues      int
	OpenPRs         int
	LatestRelease   *Release
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

// CountOpenIssuesAndPRs returns (open issues, open PRs) for a repo.
// GitHub's /issues endpoint returns both issues and PRs; we filter PRs by
// the presence of a pull_request field on each item.
func (c *Client) CountOpenIssuesAndPRs(owner, repo string) (int, int, error) {
	var items []json.RawMessage
	q := url.Values{}
	q.Set("state", "open")
	q.Set("per_page", "100")
	err := c.get(fmt.Sprintf("/repos/%s/%s/issues", owner, repo), &items, q)
	if err != nil {
		return 0, 0, err
	}
	issues, prs := 0, 0
	for _, raw := range items {
		var probe struct {
			PullRequest *json.RawMessage `json:"pull_request"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			continue
		}
		if probe.PullRequest != nil {
			prs++
		} else {
			issues++
		}
	}
	return issues, prs, nil
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