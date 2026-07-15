package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/internal/binaryupdate"
)

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int    `json:"size"`
	ID                 int64  `json:"id"`
}

type githubRelease struct {
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	URL         string        `json:"url"`
	HTMLURL     string        `json:"html_url"`
	Body        string        `json:"body"`
	PublishedAt string        `json:"published_at"`
	Draft       bool          `json:"draft"`
	Prerelease  bool          `json:"prerelease"`
	Assets      []githubAsset `json:"assets"`
}

func TestUpdateCheck_ShowsUpdateAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/kgruel/subtask/releases/latest" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(githubRelease{
			TagName: "v1.1.0",
			HTMLURL: "https://example.invalid/release/2",
			Assets:  []githubAsset{},
		})
	}))
	t.Cleanup(srv.Close)

	prevClient := newBinaryUpdateClient
	newBinaryUpdateClient = func() *binaryupdate.Client {
		c := binaryupdate.NewClient(srv.Client())
		c.APIBaseURL = srv.URL
		return c
	}
	t.Cleanup(func() { newBinaryUpdateClient = prevClient })

	prevVersion := version
	version = "v1.0.0"
	t.Cleanup(func() { version = prevVersion })

	stdout, stderr, err := captureStdoutStderr(t, (&UpdateCmd{Check: true}).Run)
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "Update available: yes")
}

func TestUpdateCheck_ShowsUpToDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/kgruel/subtask/releases/latest" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(githubRelease{
			TagName: "v1.1.0",
			HTMLURL: "https://example.invalid/release/2",
			Assets:  []githubAsset{},
		})
	}))
	t.Cleanup(srv.Close)

	prevClient := newBinaryUpdateClient
	newBinaryUpdateClient = func() *binaryupdate.Client {
		c := binaryupdate.NewClient(srv.Client())
		c.APIBaseURL = srv.URL
		return c
	}
	t.Cleanup(func() { newBinaryUpdateClient = prevClient })

	prevVersion := version
	version = "v1.1.0"
	t.Cleanup(func() { version = prevVersion })

	stdout, stderr, err := captureStdoutStderr(t, (&UpdateCmd{Check: true}).Run)
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "Update available: no")
}

func TestRefreshPluginAfterSwap_InvokesChildWithExeAndEnv(t *testing.T) {
	prev := runSyncPluginChild
	var gotExe string
	runSyncPluginChild = func(exe string) error {
		gotExe = exe
		return nil
	}
	t.Cleanup(func() { runSyncPluginChild = prev })

	_, stderr, err := captureStdoutStderr(t, func() error {
		refreshPluginAfterSwap("/path/to/subtask")
		return nil
	})
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Equal(t, "/path/to/subtask", gotExe)
}

func TestRefreshPluginAfterSwap_WarnsOnChildFailure(t *testing.T) {
	prev := runSyncPluginChild
	runSyncPluginChild = func(exe string) error {
		return errors.New("boom")
	}
	t.Cleanup(func() { runSyncPluginChild = prev })

	_, stderr, err := captureStdoutStderr(t, func() error {
		refreshPluginAfterSwap("/path/to/subtask")
		return nil
	})
	require.NoError(t, err)
	require.Contains(t, stderr, "could not refresh the installed plugin/skill")
}

func TestRefreshPluginAfterSwapQuiet_InvokesChildWithExe(t *testing.T) {
	prev := runSyncPluginChild
	var gotExe string
	runSyncPluginChild = func(exe string) error {
		gotExe = exe
		return nil
	}
	t.Cleanup(func() { runSyncPluginChild = prev })

	stdout, stderr, err := captureStdoutStderr(t, func() error {
		refreshPluginAfterSwapQuiet("/path/to/subtask")
		return nil
	})
	require.NoError(t, err)
	require.Empty(t, stdout)
	require.Empty(t, stderr)
	require.Equal(t, "/path/to/subtask", gotExe)
}

func TestRefreshPluginAfterSwapQuiet_LogsInsteadOfPrintingOnFailure(t *testing.T) {
	prev := runSyncPluginChild
	runSyncPluginChild = func(exe string) error {
		return errors.New("boom")
	}
	t.Cleanup(func() { runSyncPluginChild = prev })

	// Must not print: a background goroutine calling this could interleave
	// with an unrelated foreground command's output.
	stdout, stderr, err := captureStdoutStderr(t, func() error {
		refreshPluginAfterSwapQuiet("/path/to/subtask")
		return nil
	})
	require.NoError(t, err)
	require.Empty(t, stdout)
	require.Empty(t, stderr)
}
