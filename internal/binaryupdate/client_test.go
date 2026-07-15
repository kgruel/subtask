package binaryupdate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Client{
		HTTP:       srv.Client(),
		APIBaseURL: srv.URL,
		Repo:       "owner/repo",
	}
}

func TestGetRelease_AttachesTokenWhenSet(t *testing.T) {
	var gotAuth string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v1.0.0"}`))
	})
	c.Token = "abc123"

	_, err := c.LatestRelease(context.Background())
	require.NoError(t, err)
	require.Equal(t, "Bearer abc123", gotAuth)
}

func TestGithubTokenFromEnv_Precedence(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "github-token")
	t.Setenv("GH_TOKEN", "gh-token")
	require.Equal(t, "github-token", githubTokenFromEnv())

	t.Setenv("GITHUB_TOKEN", "")
	require.Equal(t, "gh-token", githubTokenFromEnv())

	t.Setenv("GITHUB_TOKEN", "   ")
	require.Equal(t, "gh-token", githubTokenFromEnv())

	t.Setenv("GH_TOKEN", "")
	require.Equal(t, "", githubTokenFromEnv())
}

func TestGetRelease_NoAuthHeaderWhenTokenUnset(t *testing.T) {
	var gotAuth string
	sawHeader := false
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, sawHeader = r.Header["Authorization"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v1.0.0"}`))
	})

	_, err := c.LatestRelease(context.Background())
	require.NoError(t, err)
	require.False(t, sawHeader)
	require.Empty(t, gotAuth)
}

func TestGetRelease_RateLimited_WithResetHeader(t *testing.T) {
	reset := time.Now().Add(23 * time.Minute).Unix()
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-ratelimit-remaining", "0")
		w.Header().Set("x-ratelimit-reset", strconv.FormatInt(reset, 10))
		w.WriteHeader(http.StatusForbidden)
	})

	_, err := c.LatestRelease(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "rate limit exceeded")
	require.Contains(t, err.Error(), "unauthenticated requests share 60/hour per IP")
	require.Contains(t, err.Error(), "Set GITHUB_TOKEN to authenticate")
	require.Contains(t, err.Error(), "Retry in ~23m")
}

func TestGetRelease_RateLimited_WithoutResetHeader(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-ratelimit-remaining", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	})

	_, err := c.LatestRelease(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "rate limit exceeded")
	require.Contains(t, err.Error(), "Set GITHUB_TOKEN to authenticate")
	require.NotContains(t, err.Error(), "Retry in")
}

func TestGetRelease_RateLimited_Authenticated(t *testing.T) {
	reset := time.Now().Add(5 * time.Minute).Unix()
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-ratelimit-remaining", "0")
		w.Header().Set("x-ratelimit-reset", strconv.FormatInt(reset, 10))
		w.WriteHeader(http.StatusForbidden)
	})
	c.Token = "abc123"

	_, err := c.LatestRelease(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "rate limit exceeded")
	require.Contains(t, err.Error(), "authenticated request limit reached")
	require.NotContains(t, err.Error(), "Set GITHUB_TOKEN")
	require.Contains(t, err.Error(), "Retry in ~5m")
}

func TestGetRelease_TooManyRequests_NoHeaders(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})

	_, err := c.LatestRelease(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "rate limit exceeded")
	require.Contains(t, err.Error(), "Set GITHUB_TOKEN to authenticate")
	require.NotContains(t, err.Error(), "Retry in")
}

func TestGetRelease_SecondaryLimit_RetryAfterOnly(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusForbidden)
	})

	_, err := c.LatestRelease(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "rate limit exceeded")
	require.Contains(t, err.Error(), "Retry in ~2m")
}

func TestGetRelease_RetryAfterPreferredOverReset(t *testing.T) {
	reset := time.Now().Add(23 * time.Minute).Unix()
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-ratelimit-remaining", "0")
		w.Header().Set("x-ratelimit-reset", strconv.FormatInt(reset, 10))
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusForbidden)
	})

	_, err := c.LatestRelease(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "Retry in ~1m")
	require.NotContains(t, err.Error(), "Retry in ~23m")
}

func TestGetRelease_PlainForbidden_PassThroughWithURL(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	_, err := c.LatestRelease(context.Background())
	require.Error(t, err)
	require.NotContains(t, err.Error(), "rate limit exceeded")
	require.Contains(t, err.Error(), c.APIBaseURL)
	require.Contains(t, err.Error(), "/repos/owner/repo/releases/latest")
}
