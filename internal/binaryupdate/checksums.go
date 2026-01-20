package binaryupdate

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

func SHA256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func FindSHA256ForFilename(checksums []byte, filename string) (string, bool) {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return "", false
	}

	s := bufio.NewScanner(bytes.NewReader(checksums))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		sum := fields[0]
		name := fields[len(fields)-1]
		name = strings.TrimPrefix(name, "*") // sha256sum -b
		if name == filename {
			return sum, true
		}
	}
	return "", false
}

func VerifySHA256(checksums []byte, filename string, data []byte) error {
	expected, ok := FindSHA256ForFilename(checksums, filename)
	if !ok {
		return fmt.Errorf("checksum not found for %q", filename)
	}
	if _, err := hex.DecodeString(expected); err != nil {
		return errors.New("invalid checksum format")
	}
	actual := SHA256Hex(data)
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum mismatch for %q", filename)
	}
	return nil
}
