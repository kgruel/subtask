package index_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	taskindex "github.com/kgruel/subtask/pkg/task/index"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/task/migrate/gitredesign"
)

func BenchmarkIndex_Refresh_NoChanges_100Tasks(b *testing.B) {
	setupTempProject(b)

	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("bench/%03d", i)
		requireNoError(b, (&task.Task{Name: name, Title: "t", BaseBranch: "main", Description: "d", Schema: gitredesign.TaskSchemaVersion}).Save())
		requireNoError(b, history.WriteAll(name, []history.Event{
			{TS: time.Now().UTC(), Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
			{TS: time.Now().UTC(), Type: "stage.changed", Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
		}))
	}

	idx, err := taskindex.OpenDefault()
	requireNoError(b, err)
	defer idx.Close()

	ctx := context.Background()
	requireNoError(b, idx.Refresh(ctx, taskindex.RefreshPolicy{}))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		requireNoError(b, idx.Refresh(ctx, taskindex.RefreshPolicy{}))
	}
}

func BenchmarkIndex_List_NoChanges_100Tasks(b *testing.B) {
	setupTempProject(b)

	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("bench/%03d", i)
		requireNoError(b, (&task.Task{Name: name, Title: "t", BaseBranch: "main", Description: "d", Schema: gitredesign.TaskSchemaVersion}).Save())
		requireNoError(b, history.WriteAll(name, []history.Event{
			{TS: time.Now().UTC(), Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
			{TS: time.Now().UTC(), Type: "stage.changed", Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
		}))
	}

	idx, err := taskindex.OpenDefault()
	requireNoError(b, err)
	defer idx.Close()

	ctx := context.Background()
	requireNoError(b, idx.Refresh(ctx, taskindex.RefreshPolicy{}))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		requireNoError(b, idx.Refresh(ctx, taskindex.RefreshPolicy{}))
		_, err := idx.ListAll(ctx)
		requireNoError(b, err)
	}
}

func BenchmarkIndex_Detail_Cached(b *testing.B) {
	setupTempProject(b)

	name := "bench/detail"
	requireNoError(b, (&task.Task{Name: name, Title: "t", BaseBranch: "main", Description: "d", Schema: gitredesign.TaskSchemaVersion}).Save())
	requireNoError(b, history.WriteAll(name, []history.Event{
		{TS: time.Now().UTC(), Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
		{TS: time.Now().UTC(), Type: "stage.changed", Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
	}))

	idx, err := taskindex.OpenDefault()
	requireNoError(b, err)
	defer idx.Close()

	ctx := context.Background()
	requireNoError(b, idx.Refresh(ctx, taskindex.RefreshPolicy{}))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, ok, err := idx.Get(ctx, name)
		if err != nil || !ok {
			b.Fatalf("get: ok=%v err=%v", ok, err)
		}
	}
}

func setupTempProject(tb testing.TB) {
	tb.Helper()

	root, err := os.MkdirTemp("", "subtask-bench-*")
	requireNoError(tb, err)

	orig, err := os.Getwd()
	requireNoError(tb, err)

	requireNoError(tb, os.MkdirAll(filepath.Join(root, ".subtask", "tasks"), 0o755))
	requireNoError(tb, os.MkdirAll(filepath.Join(root, ".subtask", "internal"), 0o755))

	requireNoError(tb, os.Chdir(root))
	tb.Cleanup(func() {
		_ = os.Chdir(orig)
		_ = os.RemoveAll(root)
	})
}

func requireNoError(tb testing.TB, err error) {
	tb.Helper()
	if err != nil {
		tb.Fatal(err)
	}
}
