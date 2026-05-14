package gather

import (
	"context"

	"github.com/kgruel/subtask/pkg/task/index"
)

// Children returns all tasks that have parentName as their follow-up source.
// Results are ordered by most-recently-active first.
func Children(ctx context.Context, parentName string) ([]index.ListItem, error) {
	idx, err := index.OpenDefault()
	if err != nil {
		return nil, err
	}
	defer idx.Close()

	if err := idx.Refresh(ctx, index.RefreshPolicy{}); err != nil {
		return nil, err
	}

	return idx.ListChildren(ctx, parentName)
}
