package binaryupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const defaultAPIBaseURL = "https://api.github.com"

type Client struct {
	HTTP       *http.Client
	APIBaseURL string // e.g. https://api.github.com
	Repo       string // owner/name
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
		Repo:       "zippoxer/subtask",
	}
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

	res, err := c.HTTP.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("github api: %s", res.Status)
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
