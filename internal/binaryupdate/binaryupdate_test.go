package binaryupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFindSHA256ForFilename(t *testing.T) {
	sum := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	data := []byte(sum + "  subtask_1.2.3_darwin_amd64.tar.gz\n")
	got, ok := FindSHA256ForFilename(data, "subtask_1.2.3_darwin_amd64.tar.gz")
	require.True(t, ok)
	require.Equal(t, sum, got)
}

func TestExtractSubtaskBinary_TarGz(t *testing.T) {
	var tarBuf bytes.Buffer
	gw := gzip.NewWriter(&tarBuf)
	tw := tar.NewWriter(gw)
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: "subtask",
		Mode: 0o755,
		Size: int64(len("hello")),
	}))
	_, _ = tw.Write([]byte("hello"))
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	out, err := ExtractSubtaskBinary("linux", "subtask_1.0.0_linux_amd64.tar.gz", tarBuf.Bytes())
	require.NoError(t, err)
	require.Equal(t, []byte("hello"), out)
}

func TestExtractSubtaskBinary_Zip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("subtask.exe")
	require.NoError(t, err)
	_, _ = w.Write([]byte("hi"))
	require.NoError(t, zw.Close())

	out, err := ExtractSubtaskBinary("windows", "subtask_1.0.0_windows_amd64.zip", buf.Bytes())
	require.NoError(t, err)
	require.Equal(t, []byte("hi"), out)
}

func TestSelectReleaseAssets(t *testing.T) {
	rel := Release{
		TagName: "v1.0.0",
		Assets: []Asset{
			{Name: "checksums.txt", BrowserDownloadURL: "https://example.invalid/checksums.txt"},
			{Name: "subtask_1.0.0_linux_amd64.tar.gz", BrowserDownloadURL: "https://example.invalid/linux.tar.gz"},
		},
	}
	archive, checksums, err := SelectReleaseAssets(rel, "linux", "amd64")
	require.NoError(t, err)
	require.Equal(t, "subtask_1.0.0_linux_amd64.tar.gz", archive.Name)
	require.Equal(t, "checksums.txt", checksums.Name)
}
