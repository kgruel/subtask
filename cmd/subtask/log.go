package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kgruel/subtask/pkg/render"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/task/migrate"
	"github.com/kgruel/subtask/pkg/task/store"
)

// LogCmd implements 'subtask log' (conversation + lifecycle history).
type LogCmd struct {
	Task     string `arg:"" help:"Task name"`
	Events   bool   `help:"Show lifecycle events only"`
	Messages bool   `help:"Show messages only"`
	Since    string `help:"Show entries since duration or timestamp (e.g., '5m', '1h', '1d', '2024-01-01T10:00:00Z')"`
}

func (c *LogCmd) Run() error {
	if c.Events && c.Messages {
		return fmt.Errorf("--events and --messages are mutually exclusive")
	}

	if _, err := preflightProject(); err != nil {
		return err
	}

	if err := migrate.EnsureSchema(c.Task); err != nil {
		return err
	}

	var since time.Time
	if strings.TrimSpace(c.Since) != "" {
		t, err := parseSince(c.Since)
		if err != nil {
			return fmt.Errorf("invalid --since value: %w", err)
		}
		since = t
	}

	evs, err := history.Read(c.Task, history.ReadOptions{
		Since:        since,
		MessagesOnly: c.Messages,
		EventsOnly:   c.Events,
	})
	if err != nil {
		return err
	}
	if len(evs) == 0 {
		return nil
	}

	if render.Pretty {
		if view, err := store.BuildView(context.Background(), c.Task, nil, store.BuildViewOptions{}); err == nil {
			agentLabel := view.Agent.Label()
			tag := render.Dim(view.Name) + render.Dim(" · ") + render.Dim(agentLabel) + render.Dim(" · ") + render.Status(view.StatusText)
			fmt.Println(tag)
		}
	}

	for _, ev := range evs {
		fmt.Println(formatHistoryEvent(ev))
	}
	return nil
}

func formatHistoryEvent(ev history.Event) string {
	if render.Pretty {
		return formatHistoryEventPretty(ev)
	}
	return formatHistoryEventPlain(ev)
}

func formatHistoryEventPretty(ev history.Event) string {
	dimTS := ""
	if !ev.TS.IsZero() {
		dimTS = render.Dim(ev.TS.Local().Format("15:04:05"))
	}

	if ev.Type == "message" {
		role := strings.TrimSpace(ev.Role)
		if role == "" {
			role = "unknown"
		}
		content := strings.TrimRight(ev.Content, "\n")
		if strings.TrimSpace(content) == "" {
			content = "(empty)"
		}
		if role == "worker" {
			var d struct {
				Agent history.EventAgent `json:"agent"`
			}
			_ = json.Unmarshal(ev.Data, &d)
			if strings.TrimSpace(d.Agent.Name) != "" {
				role = fmt.Sprintf("worker (%s)", strings.TrimSpace(d.Agent.Name))
			}
		}
		var styledRole string
		if strings.HasPrefix(role, "worker") {
			styledRole = render.Bold(role)
		} else {
			styledRole = render.Highlight(role)
		}
		if dimTS != "" {
			return dimTS + " " + styledRole + ": " + content
		}
		return styledRole + ": " + content
	}

	// Lifecycle events — build as dim tokens with optional colored outcome/verb.
	line := formatLifecyclePretty(ev)
	if dimTS != "" {
		return dimTS + " " + line
	}
	return line
}

// formatLifecyclePretty renders a lifecycle event as per-token dim strings
// with colored outcome/verb tokens where the spec calls for it.
func formatLifecyclePretty(ev history.Event) string {
	switch ev.Type {
	case "task.opened":
		var d struct {
			Reason     string `json:"reason"`
			From       string `json:"from"`
			Title      string `json:"title"`
			BaseBranch string `json:"base_branch"`
			Workflow   string `json:"workflow"`
		}
		_ = json.Unmarshal(ev.Data, &d)
		desc := "task opened"
		if d.Reason != "" {
			desc = fmt.Sprintf("task opened (%s)", d.Reason)
			if d.Reason == "reopen" && d.From != "" {
				desc += fmt.Sprintf(" from %s", d.From)
			}
		}
		if d.BaseBranch != "" {
			desc += fmt.Sprintf(" base=%s", d.BaseBranch)
		}
		if d.Workflow != "" {
			desc += fmt.Sprintf(" workflow=%s", d.Workflow)
		}
		return render.Dim(desc)

	case "stage.changed":
		var d struct {
			From string `json:"from"`
			To   string `json:"to"`
		}
		_ = json.Unmarshal(ev.Data, &d)
		desc := "stage changed"
		if d.From != "" || d.To != "" {
			desc = fmt.Sprintf("stage changed: %s → %s", d.From, d.To)
		}
		return render.Dim(desc)

	case "task.commit":
		var d struct {
			SHA     string `json:"sha"`
			Subject string `json:"subject"`
		}
		_ = json.Unmarshal(ev.Data, &d)
		if d.SHA != "" || d.Subject != "" {
			return render.Status("commit") + render.Dim(fmt.Sprintf(" %s %q", shortSHA(d.SHA), strings.TrimSpace(d.Subject)))
		}
		return render.Dim("commit")

	case "task.merged":
		var d struct {
			Commit string `json:"commit"`
			Into   string `json:"into"`
			Via    string `json:"via"`
			Method string `json:"method"`
		}
		_ = json.Unmarshal(ev.Data, &d)
		commit := strings.TrimSpace(d.Commit)
		into := strings.TrimSpace(d.Into)
		via := strings.TrimSpace(d.Via)
		method := strings.TrimSpace(d.Method)
		if commit != "" && into != "" {
			return render.Status("merged") + render.Dim(fmt.Sprintf(" %s into %s", shortSHA(commit), into))
		} else if into != "" {
			desc := "marked merged into " + into
			if method != "" {
				desc += " (" + method + ")"
			} else if via != "" {
				desc += " (" + via + ")"
			}
			return render.Dim(desc)
		}
		return render.Dim("task.merged")

	case "task.closed":
		var d struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(ev.Data, &d)
		if d.Reason != "" {
			return render.Dim(fmt.Sprintf("task closed (%s)", d.Reason))
		}
		return render.Dim("task closed")

	case "review.started":
		var d struct {
			RunID        string             `json:"run_id"`
			Kind         string             `json:"kind"`
			Adapter      string             `json:"adapter"`
			Model        string             `json:"model"`
			Reasoning    string             `json:"reasoning"`
			Instructions string             `json:"instructions"`
			Agent        history.EventAgent `json:"agent"`
		}
		_ = json.Unmarshal(ev.Data, &d)
		var pieces []string
		pieces = append(pieces, render.Dim("review started"))
		if d.RunID != "" {
			pieces = append(pieces, render.Dim("run="+d.RunID))
		}
		if d.Kind != "" {
			pieces = append(pieces, render.Dim("kind="+d.Kind))
		}
		if label := eventAgentLabel(d.Agent); label != "" {
			pieces = append(pieces, render.Dim("agent="+label))
		}
		if d.Adapter != "" {
			pieces = append(pieces, render.Dim("adapter="+d.Adapter))
		}
		if d.Model != "" {
			pieces = append(pieces, render.Dim("model="+d.Model))
		}
		if d.Reasoning != "" {
			pieces = append(pieces, render.Dim("reasoning="+d.Reasoning))
		}
		return strings.Join(pieces, " ")

	case "review.finished":
		var d struct {
			RunID      string             `json:"run_id"`
			Kind       string             `json:"kind"`
			DurationMS int                `json:"duration_ms"`
			Outcome    string             `json:"outcome"`
			File       string             `json:"file"`
			Error      string             `json:"error"`
			Adapter    string             `json:"adapter"`
			Model      string             `json:"model"`
			Reasoning  string             `json:"reasoning"`
			Agent      history.EventAgent `json:"agent"`
		}
		_ = json.Unmarshal(ev.Data, &d)
		var pieces []string
		pieces = append(pieces, render.Dim("review finished"))
		if d.RunID != "" {
			pieces = append(pieces, render.Dim("run="+d.RunID))
		}
		if d.Kind != "" {
			pieces = append(pieces, render.Dim("kind="+d.Kind))
		}
		if d.Outcome != "" {
			pieces = append(pieces, render.Dim("outcome=")+render.Status(d.Outcome))
		}
		if label := eventAgentLabel(d.Agent); label != "" {
			pieces = append(pieces, render.Dim("agent="+label))
		}
		if d.Adapter != "" {
			pieces = append(pieces, render.Dim("adapter="+d.Adapter))
		}
		if d.Model != "" {
			pieces = append(pieces, render.Dim("model="+d.Model))
		}
		if d.Reasoning != "" {
			pieces = append(pieces, render.Dim("reasoning="+d.Reasoning))
		}
		if d.Outcome == "error" && d.Error != "" {
			pieces = append(pieces, render.Dim(fmt.Sprintf("error=%q", d.Error)))
		}
		if d.DurationMS > 0 {
			pieces = append(pieces, render.Dim("duration="+(time.Duration(d.DurationMS)*time.Millisecond).String()))
		}
		if d.Outcome == "success" && d.File != "" {
			pieces = append(pieces, render.Dim("file="+d.File))
		}
		return strings.Join(pieces, " ")

	case "worker.started":
		var d struct {
			RunID     string             `json:"run_id"`
			SessionID string             `json:"session_id"`
			Agent     history.EventAgent `json:"agent"`
		}
		_ = json.Unmarshal(ev.Data, &d)
		var pieces []string
		pieces = append(pieces, render.Dim("worker started"))
		if d.RunID != "" {
			pieces = append(pieces, render.Dim("run="+d.RunID))
		}
		if d.SessionID != "" {
			pieces = append(pieces, render.Dim("session="+d.SessionID))
		}
		if label := eventAgentLabel(d.Agent); label != "" {
			pieces = append(pieces, render.Dim("agent="+label))
		}
		return strings.Join(pieces, " ")

	case "worker.finished":
		var d struct {
			RunID        string             `json:"run_id"`
			DurationMS   int                `json:"duration_ms"`
			ToolCalls    int                `json:"tool_calls"`
			Outcome      string             `json:"outcome"`
			ErrorMessage string             `json:"error_message"`
			Error        string             `json:"error"`
			Agent        history.EventAgent `json:"agent"`
		}
		_ = json.Unmarshal(ev.Data, &d)
		var pieces []string
		pieces = append(pieces, render.Dim("worker finished"))
		if d.RunID != "" {
			pieces = append(pieces, render.Dim("run="+d.RunID))
		}
		if d.Outcome != "" {
			pieces = append(pieces, render.Dim("outcome=")+render.Status(d.Outcome))
		}
		if label := eventAgentLabel(d.Agent); label != "" {
			pieces = append(pieces, render.Dim("agent="+label))
		}
		if strings.TrimSpace(d.ErrorMessage) == "" {
			d.ErrorMessage = d.Error
		}
		if d.Outcome == "error" && strings.TrimSpace(d.ErrorMessage) != "" {
			pieces = append(pieces, render.Dim(fmt.Sprintf("error=%q", strings.TrimSpace(d.ErrorMessage))))
		}
		if d.DurationMS > 0 {
			pieces = append(pieces, render.Dim("duration="+(time.Duration(d.DurationMS)*time.Millisecond).String()))
		}
		if d.ToolCalls > 0 {
			pieces = append(pieces, render.Dim(fmt.Sprintf("tool_calls=%d", d.ToolCalls)))
		}
		return strings.Join(pieces, " ")

	case "artifact.produced":
		var d struct {
			Kind  string             `json:"kind"`
			Path  string             `json:"path"`
			Agent history.EventAgent `json:"agent"`
		}
		_ = json.Unmarshal(ev.Data, &d)
		var pieces []string
		pieces = append(pieces, render.Dim("artifact produced"))
		if d.Kind != "" {
			pieces = append(pieces, render.Dim("kind="+d.Kind))
		}
		if d.Path != "" {
			pieces = append(pieces, render.Dim("path="+d.Path))
		}
		if label := eventAgentLabel(d.Agent); label != "" {
			pieces = append(pieces, render.Dim("by="+label))
		}
		return strings.Join(pieces, " ")

	default:
		return render.Dim(strings.TrimSpace(ev.Type))
	}
}

func formatHistoryEventPlain(ev history.Event) string {
	ts := ""
	if !ev.TS.IsZero() {
		ts = ev.TS.Local().Format(time.RFC3339)
	}

	if ev.Type == "message" {
		role := strings.TrimSpace(ev.Role)
		if role == "" {
			role = "unknown"
		}
		content := strings.TrimRight(ev.Content, "\n")
		if strings.TrimSpace(content) == "" {
			content = "(empty)"
		}
		if role == "worker" {
			var d struct {
				Agent history.EventAgent `json:"agent"`
			}
			_ = json.Unmarshal(ev.Data, &d)
			if strings.TrimSpace(d.Agent.Name) != "" {
				role = fmt.Sprintf("worker (%s)", strings.TrimSpace(d.Agent.Name))
			}
		}
		if ts != "" {
			return fmt.Sprintf("%s %s: %s", ts, role, content)
		}
		return fmt.Sprintf("%s: %s", role, content)
	}

	// Lifecycle / runtime events
	desc := strings.TrimSpace(ev.Type)
	switch ev.Type {
	case "task.opened":
		var d struct {
			Reason     string `json:"reason"`
			From       string `json:"from"`
			Title      string `json:"title"`
			BaseBranch string `json:"base_branch"`
			Workflow   string `json:"workflow"`
		}
		_ = json.Unmarshal(ev.Data, &d)
		if d.Reason != "" {
			desc = fmt.Sprintf("task opened (%s)", d.Reason)
			if d.Reason == "reopen" && d.From != "" {
				desc += fmt.Sprintf(" from %s", d.From)
			}
		}
		if d.BaseBranch != "" {
			desc += fmt.Sprintf(" base=%s", d.BaseBranch)
		}
		if d.Workflow != "" {
			desc += fmt.Sprintf(" workflow=%s", d.Workflow)
		}
	case "stage.changed":
		var d struct {
			From string `json:"from"`
			To   string `json:"to"`
		}
		_ = json.Unmarshal(ev.Data, &d)
		if d.From != "" || d.To != "" {
			desc = fmt.Sprintf("stage changed: %s → %s", d.From, d.To)
		}
	case "task.commit":
		var d struct {
			SHA     string `json:"sha"`
			Subject string `json:"subject"`
		}
		_ = json.Unmarshal(ev.Data, &d)
		if d.SHA != "" || d.Subject != "" {
			desc = fmt.Sprintf("commit %s %q", shortSHA(d.SHA), strings.TrimSpace(d.Subject))
		}
	case "task.merged":
		var d struct {
			Commit string `json:"commit"`
			Into   string `json:"into"`
			Via    string `json:"via"`
			Method string `json:"method"`
		}
		_ = json.Unmarshal(ev.Data, &d)
		commit := strings.TrimSpace(d.Commit)
		into := strings.TrimSpace(d.Into)
		via := strings.TrimSpace(d.Via)
		method := strings.TrimSpace(d.Method)
		if commit != "" && into != "" {
			desc = fmt.Sprintf("merged %s into %s", shortSHA(commit), into)
		} else if into != "" {
			// No-op / detected merges may not have a merge commit SHA. Avoid implying one exists.
			desc = "marked merged into " + into
			if method != "" {
				desc += " (" + method + ")"
			} else if via != "" {
				desc += " (" + via + ")"
			}
		}
	case "task.closed":
		var d struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(ev.Data, &d)
		if d.Reason != "" {
			desc = fmt.Sprintf("task closed (%s)", d.Reason)
		} else {
			desc = "task closed"
		}
	case "review.started":
		var d struct {
			RunID        string             `json:"run_id"`
			Kind         string             `json:"kind"`
			Adapter      string             `json:"adapter"`
			Model        string             `json:"model"`
			Reasoning    string             `json:"reasoning"`
			Instructions string             `json:"instructions"`
			Agent        history.EventAgent `json:"agent"`
		}
		_ = json.Unmarshal(ev.Data, &d)
		desc = "review started"
		if d.RunID != "" {
			desc += " run=" + d.RunID
		}
		if d.Kind != "" {
			desc += " kind=" + d.Kind
		}
		if label := eventAgentLabel(d.Agent); label != "" {
			desc += " agent=" + label
		}
		if d.Adapter != "" {
			desc += " adapter=" + d.Adapter
		}
		if d.Model != "" {
			desc += " model=" + d.Model
		}
		if d.Reasoning != "" {
			desc += " reasoning=" + d.Reasoning
		}
	case "review.finished":
		var d struct {
			RunID      string             `json:"run_id"`
			Kind       string             `json:"kind"`
			DurationMS int                `json:"duration_ms"`
			Outcome    string             `json:"outcome"`
			File       string             `json:"file"`
			Error      string             `json:"error"`
			Adapter    string             `json:"adapter"`
			Model      string             `json:"model"`
			Reasoning  string             `json:"reasoning"`
			Agent      history.EventAgent `json:"agent"`
		}
		_ = json.Unmarshal(ev.Data, &d)
		desc = "review finished"
		if d.RunID != "" {
			desc += " run=" + d.RunID
		}
		if d.Kind != "" {
			desc += " kind=" + d.Kind
		}
		if d.Outcome != "" {
			desc += " outcome=" + d.Outcome
		}
		if label := eventAgentLabel(d.Agent); label != "" {
			desc += " agent=" + label
		}
		if d.Adapter != "" {
			desc += " adapter=" + d.Adapter
		}
		if d.Model != "" {
			desc += " model=" + d.Model
		}
		if d.Reasoning != "" {
			desc += " reasoning=" + d.Reasoning
		}
		if d.Outcome == "error" && d.Error != "" {
			desc += fmt.Sprintf(" error=%q", d.Error)
		}
		if d.DurationMS > 0 {
			desc += " duration=" + (time.Duration(d.DurationMS) * time.Millisecond).String()
		}
		if d.Outcome == "success" && d.File != "" {
			desc += " file=" + d.File
		}
	case "worker.started":
		var d struct {
			RunID     string             `json:"run_id"`
			SessionID string             `json:"session_id"`
			Agent     history.EventAgent `json:"agent"`
		}
		_ = json.Unmarshal(ev.Data, &d)
		desc = "worker started"
		if d.RunID != "" {
			desc += " run=" + d.RunID
		}
		if d.SessionID != "" {
			desc += " session=" + d.SessionID
		}
		if label := eventAgentLabel(d.Agent); label != "" {
			desc += " agent=" + label
		}
	case "worker.finished":
		var d struct {
			RunID        string             `json:"run_id"`
			DurationMS   int                `json:"duration_ms"`
			ToolCalls    int                `json:"tool_calls"`
			Outcome      string             `json:"outcome"`
			ErrorMessage string             `json:"error_message"`
			Error        string             `json:"error"`
			Agent        history.EventAgent `json:"agent"`
		}
		_ = json.Unmarshal(ev.Data, &d)
		desc = "worker finished"
		if d.RunID != "" {
			desc += " run=" + d.RunID
		}
		if d.Outcome != "" {
			desc += " outcome=" + d.Outcome
		}
		if label := eventAgentLabel(d.Agent); label != "" {
			desc += " agent=" + label
		}
		if strings.TrimSpace(d.ErrorMessage) == "" {
			d.ErrorMessage = d.Error
		}
		if d.Outcome == "error" && strings.TrimSpace(d.ErrorMessage) != "" {
			desc += fmt.Sprintf(" error=%q", strings.TrimSpace(d.ErrorMessage))
		}
		if d.DurationMS > 0 {
			desc += " duration=" + (time.Duration(d.DurationMS) * time.Millisecond).String()
		}
		if d.ToolCalls > 0 {
			desc += fmt.Sprintf(" tool_calls=%d", d.ToolCalls)
		}
	case "artifact.produced":
		var d struct {
			Kind  string             `json:"kind"`
			Path  string             `json:"path"`
			Agent history.EventAgent `json:"agent"`
		}
		_ = json.Unmarshal(ev.Data, &d)
		desc = "artifact produced"
		if d.Kind != "" {
			desc += " kind=" + d.Kind
		}
		if d.Path != "" {
			desc += " path=" + d.Path
		}
		if label := eventAgentLabel(d.Agent); label != "" {
			desc += " by=" + label
		}
	}

	if ts != "" {
		return fmt.Sprintf("%s [%s]", ts, desc)
	}
	return fmt.Sprintf("[%s]", desc)
}

func eventAgentLabel(agent history.EventAgent) string {
	if agent == (history.EventAgent{}) {
		return ""
	}
	label := agent.ToAgentView().Label()
	if label == (task.AgentView{}).Label() {
		return ""
	}
	return label
}

func shortSHA(sha string) string {
	s := strings.TrimSpace(sha)
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
