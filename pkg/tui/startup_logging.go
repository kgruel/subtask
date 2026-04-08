package tui

import (
	"fmt"
	"sync"
	"time"

	"github.com/kgruel/subtask/pkg/logging"
)

var (
	tuiStartMu sync.Mutex
	tuiStartAt time.Time

	tuiStartupOnce  sync.Once
	firstRenderOnce sync.Once
)

func recordStartup(now time.Time) {
	tuiStartMu.Lock()
	tuiStartAt = now
	tuiStartMu.Unlock()

	tuiStartupOnce.Do(func() {
		logging.Info("tui", "start")
		if logging.DebugEnabled() {
			logging.Info("tui", fmt.Sprintf("debug enabled log=%s", logging.LogPath()))
			logging.Debug("tui", "startup begin")
		}
	})
}

func sinceStartup() time.Duration {
	tuiStartMu.Lock()
	start := tuiStartAt
	tuiStartMu.Unlock()
	if start.IsZero() {
		return 0
	}
	return time.Since(start)
}

func logFirstRenderOnce() {
	if !logging.DebugEnabled() {
		return
	}
	firstRenderOnce.Do(func() {
		logging.Debug("tui", fmt.Sprintf("first render (+%s)", sinceStartup().Round(time.Millisecond)))
	})
}
