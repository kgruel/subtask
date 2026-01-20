package tui

import (
	"fmt"
	"time"
)

var nowFunc = time.Now

func formatTimeAgo(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := nowFunc().Sub(t)
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh ago", int(d.Hours()))
}
