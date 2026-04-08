package git

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kgruel/subtask/pkg/logging"
)

const (
	gitSlowThreshold  = 25 * time.Millisecond
	gitBatchFlushIdle = 150 * time.Millisecond
)

var gitCmdBatcher cmdBatcher

type cmdBatcher struct {
	mu    sync.Mutex
	count int
	total time.Duration
	timer *time.Timer
}

func (b *cmdBatcher) add(d time.Duration) {
	b.mu.Lock()
	b.count++
	b.total += d

	if b.timer == nil {
		b.timer = time.AfterFunc(gitBatchFlushIdle, b.flush)
	} else {
		b.timer.Reset(gitBatchFlushIdle)
	}
	b.mu.Unlock()
}

func (b *cmdBatcher) flushNow() {
	count, total := b.drain()
	if count == 0 {
		return
	}
	logging.Debug("git", fmt.Sprintf("batch: %d commands in %s", count, total.Round(time.Millisecond)))
}

func (b *cmdBatcher) flush() {
	count, total := b.drain()
	if count == 0 {
		return
	}
	logging.Debug("git", fmt.Sprintf("batch: %d commands in %s", count, total.Round(time.Millisecond)))
}

func (b *cmdBatcher) drain() (int, time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	count := b.count
	total := b.total
	b.count = 0
	b.total = 0
	return count, total
}

func logGitCommandTiming(args []string, d time.Duration) {
	if !logging.DebugEnabled() {
		return
	}

	if d >= gitSlowThreshold {
		gitCmdBatcher.flushNow()
		logging.Debug("git", fmt.Sprintf("%s (%s)", strings.Join(args, " "), d.Round(time.Millisecond)))
		return
	}

	gitCmdBatcher.add(d)
}
