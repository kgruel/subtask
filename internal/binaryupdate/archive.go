package binaryupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

func ExtractSubtaskBinary(goos string, archiveName string, archiveData []byte) ([]byte, error) {
	target := "subtask"
	if goos == "windows" {
		target = "subtask.exe"
	}

	switch {
	case strings.HasSuffix(archiveName, ".zip"):
		return extractFromZip(target, archiveData)
	case strings.HasSuffix(archiveName, ".tar.gz") || strings.HasSuffix(archiveName, ".tgz"):
		return extractFromTarGz(target, archiveData)
	default:
		return nil, fmt.Errorf("unsupported archive format: %s", archiveName)
	}
}

func extractFromZip(target string, data []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if filepath.Base(f.Name) != target {
			continue
		}
		if f.UncompressedSize64 > 200<<20 {
			return nil, errors.New("binary too large")
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return readAll(rc, 200<<20)
	}
	return nil, fmt.Errorf("binary %q not found in zip", target)
}

func extractFromTarGz(target string, data []byte) ([]byte, error) {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if h == nil {
			continue
		}
		if h.FileInfo().IsDir() {
			continue
		}
		if filepath.Base(h.Name) != target {
			continue
		}
		if h.Size > 200<<20 {
			return nil, errors.New("binary too large")
		}
		return readAll(tr, 200<<20)
	}
	return nil, fmt.Errorf("binary %q not found in tar.gz", target)
}
