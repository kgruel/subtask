package workflow

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestStageFields_UnmarshalYAML(t *testing.T) {
	data := []byte(`name: test
description: Test workflow
stages:
  - name: doing
    advance: auto
    produces: [plan.md, implementation.go]
    consumes: [task.md]
  - name: ready
    instructions: Done.
`)
	wf, err := parseWorkflow(data)
	require.NoError(t, err)
	require.Len(t, wf.Stages, 2)

	doing := wf.Stages[0]
	require.Equal(t, "auto", doing.Advance)
	require.Equal(t, []string{"plan.md", "implementation.go"}, doing.Produces)
	require.Equal(t, []string{"task.md"}, doing.Consumes)

	ready := wf.Stages[1]
	require.Empty(t, ready.Advance)
	require.Empty(t, ready.Produces)
	require.Empty(t, ready.Consumes)
}

func TestNextStage_ReturnsEmptyForLastStage(t *testing.T) {
	data := []byte(`name: test
description: Test
stages:
  - name: doing
    instructions: Work.
  - name: ready
    advance: auto
    instructions: Done.
`)
	wf, err := parseWorkflow(data)
	require.NoError(t, err)

	require.Equal(t, "", wf.NextStage("ready"), "NextStage on last stage must return empty string")
}

func TestStageFields_AbsentFieldsAreZero(t *testing.T) {
	data := []byte(`name: test
description: Test
stages:
  - name: doing
    instructions: Work.
`)
	wf, err := parseWorkflow(data)
	require.NoError(t, err)

	st := wf.Stages[0]
	require.Empty(t, st.Advance)
	require.Nil(t, st.Produces)
	require.Nil(t, st.Consumes)
}

func TestStageFields_MarshalRoundTrip(t *testing.T) {
	orig := Stage{
		Name:     "doing",
		Advance:  "auto",
		Produces: []string{"plan.md", "impl.go"},
		Consumes: []string{"task.md"},
	}
	data, err := yaml.Marshal(orig)
	require.NoError(t, err)

	var got Stage
	require.NoError(t, yaml.Unmarshal(data, &got))
	require.Equal(t, orig.Advance, got.Advance)
	require.Equal(t, orig.Produces, got.Produces)
	require.Equal(t, orig.Consumes, got.Consumes)
}
