package history

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/kgruel/subtask/pkg/logging"
	"github.com/kgruel/subtask/pkg/task"
)

type Event struct {
	TS      time.Time       `json:"ts"`
	Type    string          `json:"type"`
	Role    string          `json:"role,omitempty"`
	Content string          `json:"content,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func NewRunID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func Append(taskName string, ev Event) error {
	if ev.TS.IsZero() {
		ev.TS = time.Now().UTC()
	}
	if strings.TrimSpace(ev.Type) == "" {
		return fmt.Errorf("history append: type is required")
	}

	return task.WithLock(taskName, func() error {
		return AppendLocked(taskName, ev)
	})
}

// AppendLocked appends an event to history.jsonl. The caller must hold the task lock.
func AppendLocked(taskName string, ev Event) error {
	if ev.TS.IsZero() {
		ev.TS = time.Now().UTC()
	}
	path := task.HistoryPath(taskName)
	if err := os.MkdirAll(task.Dir(taskName), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

func WriteAll(taskName string, events []Event) error {
	return task.WithLock(taskName, func() error {
		return WriteAllLocked(taskName, events)
	})
}

// WriteAllLocked rewrites history.jsonl atomically. The caller must hold the task lock.
func WriteAllLocked(taskName string, events []Event) error {
	if err := os.MkdirAll(task.Dir(taskName), 0o755); err != nil {
		return err
	}
	path := task.HistoryPath(taskName)
	tmp := path + ".tmp"

	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	buf := bufio.NewWriterSize(f, 128*1024)
	for _, ev := range events {
		if ev.TS.IsZero() {
			ev.TS = time.Now().UTC()
		}
		b, err := json.Marshal(ev)
		if err != nil {
			f.Close()
			_ = os.Remove(tmp)
			return err
		}
		if _, err := buf.Write(append(b, '\n')); err != nil {
			f.Close()
			_ = os.Remove(tmp)
			return err
		}
	}
	if err := buf.Flush(); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

type ReadOptions struct {
	Since          time.Time
	MessagesOnly   bool
	EventsOnly     bool
	MaxEvents      int
	IncludeInvalid bool
}

func Read(taskName string, opts ReadOptions) ([]Event, error) {
	path := task.HistoryPath(taskName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var out []Event
	lines := bytes.Split(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")), []byte("\n"))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			if opts.IncludeInvalid {
				out = append(out, Event{Type: "invalid", Content: string(line)})
			}
			continue
		}
		if !opts.Since.IsZero() && !ev.TS.IsZero() && ev.TS.Before(opts.Since) {
			continue
		}
		isMsg := ev.Type == "message"
		if opts.MessagesOnly && !isMsg {
			continue
		}
		if opts.EventsOnly && isMsg {
			continue
		}
		out = append(out, ev)
		if opts.MaxEvents > 0 && len(out) >= opts.MaxEvents {
			break
		}
	}
	return out, nil
}

type TailInfo struct {
	LastTS                 time.Time
	TaskStatus             task.TaskStatus
	Stage                  string
	LastMergedCommit       string
	LastMergedMethod       string
	LastMergedBaseCommit   string
	LastMergedBranchHead   string
	LastMergedLinesAdded   int
	LastMergedLinesRemoved int
	LastMergedFrozenError  string
	LastClosedLinesAdded   int
	LastClosedLinesRemoved int
	LastClosedFrozenError  string
	BaseBranch             string
	BaseCommit             string

	LastRunDurationMS int
	LastRunToolCalls  int
	LastRunOutcome    string

	RunningSince time.Time
	RunningRunID string
}

func Tail(taskName string) (TailInfo, error) {
	path := task.HistoryPath(taskName)
	return TailPath(path)
}

func TailPath(path string) (TailInfo, error) {
	debug := logging.DebugEnabled()
	var start time.Time
	if debug {
		start = time.Now()
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return TailInfo{}, nil
		}
		return TailInfo{}, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return TailInfo{}, err
	}
	size := st.Size()
	if size == 0 {
		return TailInfo{}, nil
	}

	const maxBytes = 256 * 1024
	readN := int64(maxBytes)
	if size < readN {
		readN = size
	}

	if _, err := f.Seek(-readN, io.SeekEnd); err != nil {
		return TailInfo{}, err
	}
	buf := make([]byte, readN)
	if _, err := io.ReadFull(f, buf); err != nil {
		return TailInfo{}, err
	}
	if readN < size {
		if idx := bytes.IndexByte(buf, '\n'); idx >= 0 {
			buf = buf[idx+1:]
		}
	}

	lines := bytes.Split(bytes.TrimSpace(bytes.ReplaceAll(buf, []byte("\r\n"), []byte("\n"))), []byte("\n"))
	events := make([]Event, 0, len(lines))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		events = append(events, ev)
	}

	var info TailInfo
	var taskStatusSet bool
	var mergedStatsSet bool
	var closedStatsSet bool

	// Track run completion by run_id for "running since" detection.
	finishedByRun := make(map[string]struct{})
	for _, ev := range events {
		countForRecency := true
		if ev.Type == "task.merged" {
			var d struct {
				Via string `json:"via"`
			}
			countForRecency = json.Unmarshal(ev.Data, &d) != nil || strings.TrimSpace(d.Via) != "detected"
		}
		if countForRecency && ev.TS.After(info.LastTS) {
			info.LastTS = ev.TS
		}
		if ev.Type != "worker.finished" {
			continue
		}
		var d struct {
			RunID      string `json:"run_id"`
			DurationMS int    `json:"duration_ms"`
			ToolCalls  int    `json:"tool_calls"`
			Outcome    string `json:"outcome"`
		}
		if err := json.Unmarshal(ev.Data, &d); err != nil {
			continue
		}
		if d.RunID != "" {
			finishedByRun[d.RunID] = struct{}{}
		}
	}

	// Walk backwards for most recent state-ish events.
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		switch ev.Type {
		case "task.closed":
			if !taskStatusSet {
				info.TaskStatus = task.TaskStatusClosed
				taskStatusSet = true
			}
			if !closedStatsSet {
				var d struct {
					ChangesAdded   int    `json:"changes_added"`
					ChangesRemoved int    `json:"changes_removed"`
					FrozenError    string `json:"frozen_error"`
				}
				_ = json.Unmarshal(ev.Data, &d)
				info.LastClosedLinesAdded = d.ChangesAdded
				info.LastClosedLinesRemoved = d.ChangesRemoved
				info.LastClosedFrozenError = strings.TrimSpace(d.FrozenError)
				closedStatsSet = true
			}
		case "task.merged":
			if !taskStatusSet {
				info.TaskStatus = task.TaskStatusMerged
				taskStatusSet = true
			}
			var d struct {
				Commit         string `json:"commit"`
				Method         string `json:"method"`
				BaseCommit     string `json:"base_commit"`
				BranchHead     string `json:"branch_head"`
				ChangesAdded   int    `json:"changes_added"`
				ChangesRemoved int    `json:"changes_removed"`
				FrozenError    string `json:"frozen_error"`
			}
			_ = json.Unmarshal(ev.Data, &d)
			if info.LastMergedCommit == "" {
				info.LastMergedCommit = strings.TrimSpace(d.Commit)
			}
			if info.LastMergedMethod == "" {
				info.LastMergedMethod = strings.TrimSpace(d.Method)
			}
			if info.LastMergedBaseCommit == "" {
				info.LastMergedBaseCommit = strings.TrimSpace(d.BaseCommit)
			}
			if info.LastMergedBranchHead == "" {
				info.LastMergedBranchHead = strings.TrimSpace(d.BranchHead)
			}
			if !mergedStatsSet {
				info.LastMergedLinesAdded = d.ChangesAdded
				info.LastMergedLinesRemoved = d.ChangesRemoved
				info.LastMergedFrozenError = strings.TrimSpace(d.FrozenError)
				mergedStatsSet = true
			}
		case "task.opened":
			if !taskStatusSet {
				info.TaskStatus = task.TaskStatusOpen
				taskStatusSet = true
			}
			if info.BaseCommit == "" || info.BaseBranch == "" {
				var d struct {
					BaseBranch string `json:"base_branch"`
					BaseCommit string `json:"base_commit"`
				}
				_ = json.Unmarshal(ev.Data, &d)
				if info.BaseBranch == "" {
					info.BaseBranch = strings.TrimSpace(d.BaseBranch)
				}
				if info.BaseCommit == "" {
					info.BaseCommit = strings.TrimSpace(d.BaseCommit)
				}
			}
		case "stage.changed":
			if info.Stage == "" {
				var d struct {
					To string `json:"to"`
				}
				_ = json.Unmarshal(ev.Data, &d)
				info.Stage = strings.TrimSpace(d.To)
			}
		case "worker.finished":
			if info.LastRunOutcome == "" {
				var d struct {
					RunID      string `json:"run_id"`
					DurationMS int    `json:"duration_ms"`
					ToolCalls  int    `json:"tool_calls"`
					Outcome    string `json:"outcome"`
				}
				_ = json.Unmarshal(ev.Data, &d)
				info.LastRunDurationMS = d.DurationMS
				info.LastRunToolCalls = d.ToolCalls
				info.LastRunOutcome = strings.TrimSpace(d.Outcome)
			}
		case "worker.started":
			if info.RunningSince.IsZero() {
				var d struct {
					RunID string `json:"run_id"`
				}
				_ = json.Unmarshal(ev.Data, &d)
				if d.RunID != "" {
					if _, ok := finishedByRun[d.RunID]; !ok {
						info.RunningSince = ev.TS
						info.RunningRunID = d.RunID
					}
				}
			}
		}

		if !info.RunningSince.IsZero() &&
			info.Stage != "" &&
			info.LastRunOutcome != "" &&
			taskStatusSet {
			// Enough to satisfy typical callers.
			break
		}
	}

	if !taskStatusSet {
		// History exists but contains no task status event; treat as open.
		info.TaskStatus = task.TaskStatusOpen
	}

	if debug {
		d := time.Since(start)
		if d >= 5*time.Millisecond {
			logging.Debug("io", fmt.Sprintf("history.tail path=%s (%s)", path, d.Round(time.Millisecond)))
		}
	}
	return info, nil
}
