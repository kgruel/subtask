package binaryupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultAPIBaseURL = "https://api.github.com"

type Client struct {
	HTTP       *http.Client
	APIBaseURL string // e.g. https://api.github.com
	Repo       string // owner/name
	Token      string // GitHub token, e.g. from GITHUB_TOKEN/GH_TOKEN
}

type Release struct {
	TagName string  `json:"tag_name"`
	HTMLURL string  `json:"html_url"`
	Assets  []Asset `json:"assets"`
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		HTTP:       httpClient,
		APIBaseURL: defaultAPIBaseURL,
		Repo:       "kgruel/subtask",
		Token:      githubTokenFromEnv(),
	}
}

// githubTokenFromEnv resolves an ambient GitHub token from GITHUB_TOKEN, then
// GH_TOKEN. Deliberately env-only (not `gh auth token`) — explicit,
// dependency-free, and predictable. Users on `gh`-authenticated machines can
// bridge with `export GITHUB_TOKEN=$(gh auth token)`.
func githubTokenFromEnv() string {
	for _, name := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return ""
}

func (c *Client) LatestRelease(ctx context.Context) (Release, error) {
	return c.getRelease(ctx, fmt.Sprintf("/repos/%s/releases/latest", c.Repo))
}

func (c *Client) ReleaseByTag(ctx context.Context, tag string) (Release, error) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return Release{}, errors.New("empty tag")
	}
	return c.getRelease(ctx, fmt.Sprintf("/repos/%s/releases/tags/%s", c.Repo, tag))
}

func (c *Client) getRelease(ctx context.Context, path string) (Release, error) {
	if c == nil || c.HTTP == nil {
		return Release{}, errors.New("nil client")
	}
	base := strings.TrimRight(c.APIBaseURL, "/")
	u := base + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "subtask")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	res, err := c.HTTP.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return Release{}, rateLimitError(res, u, c.Token != "")
	}

	var rel Release
	if err := json.NewDecoder(res.Body).Decode(&rel); err != nil {
		return Release{}, err
	}
	if rel.TagName == "" {
		return Release{}, errors.New("github api: missing tag_name")
	}
	return rel, nil
}

// rateLimitError builds an actionable error for a non-200 getRelease
// response. GitHub signals primary rate-limiting via 403/429 plus
// x-ratelimit-remaining: 0, but secondary/abuse limits return 403 or 429 with
// x-ratelimit-remaining positive or absent — Retry-After is the marker there.
// Anything else falls back to a pass-through error that still names the
// requested URL for debuggability.
func rateLimitError(res *http.Response, url string, authenticated bool) error {
	status := res.StatusCode
	rateLimited := status == http.StatusTooManyRequests ||
		(status == http.StatusForbidden &&
			(res.Header.Get("x-ratelimit-remaining") == "0" || res.Header.Get("Retry-After") != ""))
	if !rateLimited {
		return fmt.Errorf("github api: %s (url: %s)", res.Status, url)
	}

	retryClause := ""
	if after := res.Header.Get("Retry-After"); after != "" {
		if secs, err := strconv.ParseInt(after, 10, 64); err == nil && secs > 0 {
			retryClause = fmt.Sprintf(" Retry in ~%s.", formatDuration(time.Duration(secs)*time.Second))
		}
	}
	if retryClause == "" {
		if reset := res.Header.Get("x-ratelimit-reset"); reset != "" {
			if secs, err := strconv.ParseInt(reset, 10, 64); err == nil {
				if d := time.Until(time.Unix(secs, 0)); d > 0 {
					retryClause = fmt.Sprintf(" Retry in ~%s.", formatDuration(d))
				}
			}
		}
	}

	if authenticated {
		return fmt.Errorf("github api: rate limit exceeded (authenticated request limit reached).%s", orDefault(retryClause, " Retry later."))
	}
	return fmt.Errorf("github api: rate limit exceeded (unauthenticated requests share 60/hour per IP — common on corporate networks). Set GITHUB_TOKEN to authenticate.%s", retryClause)
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// formatDuration renders d as a coarse "~Nm"/"~Nh"/"~Ns" string, matching the
// granularity useful for a rate-limit retry hint.
func formatDuration(d time.Duration) string {
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d.Round(time.Minute).Hours()))
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Round(time.Minute).Minutes()))
	default:
		return fmt.Sprintf("%ds", int(d.Round(time.Second).Seconds()))
	}
}

func (c *Client) Download(ctx context.Context, url string) ([]byte, error) {
	if c == nil || c.HTTP == nil {
		return nil, errors.New("nil client")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("User-Agent", "subtask")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download: %s", res.Status)
	}
	return readAll(res.Body, 200<<20) // 200 MiB max
}
