package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kgruel/subtask/pkg/routine"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/store"
)

func TestTUIWriteActions_SendOverlayPlumbing(t *testing.T) {
	m := writeActionTestModel("task/send")
	rec := installRecorder(&m)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmd != nil {
		t.Fatalf("expected no command")
	}
	m = next.(model)
	if !m.sendInput.active() {
		t.Fatalf("expected send input active")
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")})
	m = next.(model)
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(model)
	if m.sendInput.active() {
		t.Fatalf("expected send input closed")
	}
	assertSpawn(t, rec, "task/send", []string{"send", "task/send", "--", "hello"})
	if cmd == nil {
		t.Fatalf("expected spawn command")
	}
}

func TestTUIWriteActions_SendPromptStartingWithFlagUsesSeparator(t *testing.T) {
	m := writeActionTestModel("task/send-flag")
	rec := installRecorder(&m)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("--help should be sent")})
	m = next.(model)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	assertSpawn(t, rec, "task/send-flag", []string{"send", "task/send-flag", "--", "--help should be sent"})
	if cmd == nil {
		t.Fatalf("expected spawn command")
	}
}

func TestTUIWriteActions_DiffSStillTogglesSideBySide(t *testing.T) {
	m := writeActionTestModel("task/diff")
	m.tab = tabDiff

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = next.(model)
	if !m.diffSideBySide {
		t.Fatalf("expected diff side-by-side toggle")
	}
	if m.sendInput.active() {
		t.Fatalf("did not expect send input on diff tab")
	}
}

func TestTUIWriteActions_StageLinearWouldToast(t *testing.T) {
	m := writeActionTestModel("task/stage")
	rec := installRecorder(&m)
	m.detail = routineDetail("task/stage", "plan", &routine.Routine{
		Name: "test",
		Steps: []routine.Step{
			{ID: "plan"},
			{ID: "implement"},
			{ID: "done", Kind: routine.KindTerminal},
		},
	})

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'>'}})
	assertSpawn(t, rec, "task/stage", []string{"stage", "task/stage", "implement"})
	if cmd == nil {
		t.Fatalf("expected spawn command")
	}
}

func TestTUIWriteActions_StageGatePicker(t *testing.T) {
	m := writeActionTestModel("task/gate")
	rec := installRecorder(&m)
	m.detail = routineDetail("task/gate", "review", &routine.Routine{
		Name: "test",
		Steps: []routine.Step{
			{ID: "review", Kind: routine.KindGate, Options: []routine.Option{
				{Name: "revise", Next: "implement"},
				{Name: "ship", Next: "done"},
			}},
			{ID: "implement"},
			{ID: "done", Kind: routine.KindTerminal},
		},
	})

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'>'}})
	m = next.(model)
	if !m.stagePicker.active() {
		t.Fatalf("expected stage picker active")
	}
	if got := strings.Join(m.stagePicker.options, ","); got != "implement,done" {
		t.Fatalf("unexpected picker options %q", got)
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if m.stagePicker.active() {
		t.Fatalf("expected stage picker closed")
	}
	assertSpawn(t, rec, "task/gate", []string{"stage", "task/gate", "done"})
	if cmd == nil {
		t.Fatalf("expected spawn command")
	}
}

func TestTUIWriteActions_StageNoRoutineToast(t *testing.T) {
	m := writeActionTestModel("task/plain")
	m.detail = store.TaskView{
		Task:       &task.Task{Name: "task/plain"},
		TaskStatus: task.TaskStatusOpen,
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'>'}})
	m = next.(model)
	if m.toast.text != "task has no routine" {
		t.Fatalf("expected no-routine toast, got %q", m.toast.text)
	}
}

func TestTUIWriteActions_StageWaitsForSelectedDetailAfterNavigation(t *testing.T) {
	m := writeActionTestModel("task/a")
	rec := installRecorder(&m)
	m.tasks = []store.TaskListItem{
		{Name: "task/a", TaskStatus: task.TaskStatusOpen},
		{Name: "task/b", TaskStatus: task.TaskStatusOpen},
	}
	m.detail = routineDetail("task/a", "plan", &routine.Routine{
		Name: "test",
		Steps: []routine.Step{
			{ID: "plan"},
			{ID: "done", Kind: routine.KindTerminal},
		},
	})

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(model)
	if cmd == nil {
		t.Fatalf("expected detail fetch command")
	}
	if m.selectedTaskName != "task/b" || m.detailTaskName != "" {
		t.Fatalf("expected navigation to clear detail, selected=%q detailTaskName=%q", m.selectedTaskName, m.detailTaskName)
	}

	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'>'}})
	m = next.(model)
	if cmd != nil {
		t.Fatalf("expected no command while detail is stale")
	}
	if len(*rec) != 0 {
		t.Fatalf("expected no spawn while detail is stale, got %#v", *rec)
	}
	if m.toast.text != "task details still loading" {
		t.Fatalf("expected loading toast, got %q", m.toast.text)
	}
}

func TestTUIWriteActions_WorkingTaskBlocksSend(t *testing.T) {
	m := writeActionTestModel("task/working")
	m.tasks[0].WorkerStatus = task.WorkerStatusRunning
	m.detail.WorkerStatus = task.WorkerStatusRunning

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = next.(model)
	if cmd != nil {
		t.Fatalf("expected no command")
	}
	if m.sendInput.active() {
		t.Fatalf("did not expect send input")
	}
	if m.toast.text != "worker already running" {
		t.Fatalf("expected working toast, got %q", m.toast.text)
	}
}

func TestTUIWriteActions_StageNoNextToast(t *testing.T) {
	m := writeActionTestModel("task/no-next")
	m.detail = routineDetail("task/no-next", "lonely", &routine.Routine{
		Name:  "test",
		Steps: []routine.Step{{ID: "lonely"}},
	})

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'>'}})
	m = next.(model)
	if cmd != nil {
		t.Fatalf("expected no command")
	}
	if m.toast.text != "no next step in routine" {
		t.Fatalf("expected no-next toast, got %q", m.toast.text)
	}
}

func TestTUIWriteActions_MergeAliasWorksInDetail(t *testing.T) {
	m := writeActionTestModel("task/merge")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	m = next.(model)
	if cmd != nil {
		t.Fatalf("expected no command")
	}
	if !m.confirm.active() || m.confirm.kind != actionMerge {
		t.Fatalf("expected merge confirmation")
	}
}

func TestTUIWriteActions_QuickExitShowsToastAndAlert(t *testing.T) {
	m := writeActionTestModel("task/spawn")

	next, cmd := m.Update(spawnExitedMsg{
		action:   "send",
		taskName: "task/spawn",
		err:      "first line\nrecovery hint: run subtask send manually",
	})
	m = next.(model)
	if cmd != nil {
		t.Fatalf("expected no command")
	}
	if !strings.Contains(m.toast.text, "send failed:") {
		t.Fatalf("expected failure toast, got %q", m.toast.text)
	}
	if !m.alert.active() || !strings.Contains(m.alert.body, "recovery hint") {
		t.Fatalf("expected alert with full stderr, got %#v", m.alert)
	}
}

func TestCappedBufferDropsAfterLimit(t *testing.T) {
	buf := newCappedBuffer(5)
	if n, err := buf.Write([]byte("abcdef")); err != nil || n != 6 {
		t.Fatalf("write 1 = (%d, %v), want (6, nil)", n, err)
	}
	if n, err := buf.Write([]byte("ghij")); err != nil || n != 4 {
		t.Fatalf("write 2 = (%d, %v), want (4, nil)", n, err)
	}
	if got := buf.String(); got != "abcde" {
		t.Fatalf("expected capped buffer %q, got %q", "abcde", got)
	}
}

func writeActionTestModel(taskName string) model {
	m := newModel()
	m.mode = viewDetail
	m.tab = tabOverview
	m.width = 120
	m.height = 40
	m.selectedTaskName = taskName
	m.detailTaskName = taskName
	m.tasks = []store.TaskListItem{{
		Name:       taskName,
		TaskStatus: task.TaskStatusOpen,
	}}
	m.detail = store.TaskView{
		Task:       &task.Task{Name: taskName},
		TaskStatus: task.TaskStatusOpen,
	}
	return m
}

type spawnRecord struct {
	taskName string
	args     []string
}

func installRecorder(m *model) *[]spawnRecord {
	records := &[]spawnRecord{}
	m.subtaskSpawner = func(taskName string, args []string) tea.Cmd {
		copied := append([]string(nil), args...)
		*records = append(*records, spawnRecord{taskName: taskName, args: copied})
		return func() tea.Msg {
			return spawnStartedMsg{action: args[0], taskName: taskName}
		}
	}
	return records
}

func assertSpawn(t *testing.T, records *[]spawnRecord, taskName string, args []string) {
	t.Helper()
	if len(*records) != 1 {
		t.Fatalf("expected one spawn record, got %d", len(*records))
	}
	got := (*records)[0]
	if got.taskName != taskName {
		t.Fatalf("expected task %q, got %q", taskName, got.taskName)
	}
	if !reflect.DeepEqual(got.args, args) {
		t.Fatalf("expected args %#v, got %#v", args, got.args)
	}
}

func routineDetail(taskName, stage string, r *routine.Routine) store.TaskView {
	return store.TaskView{
		Task:       &task.Task{Name: taskName, Routine: r.Name},
		TaskStatus: task.TaskStatusOpen,
		Stage:      stage,
		Routine:    r,
	}
}
