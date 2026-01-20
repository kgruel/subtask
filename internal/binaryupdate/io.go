package binaryupdate

import (
	"fmt"
	"io"
)

func readAll(r io.Reader, max int64) ([]byte, error) {
	lr := &io.LimitedReader{R: r, N: max + 1}
	b, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("download too large (>%d bytes)", max)
	}
	return b, nil
}
