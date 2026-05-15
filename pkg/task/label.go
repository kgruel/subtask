package task

// WorkerLabel returns a display label for the worker assigned to a task.
// Resolution order:
//  1. stepAgent — agent: binding from the current routine step
//  2. taskAgent — agent field from the task snapshot
//  3. adapter/model — resolved adapter and model pair
//  4. "Worker" — sentinel fallback
//
// Live-state surfaces only; historical surfaces (log/trace) parked pending
// per-event routine state resolution.
func WorkerLabel(stepAgent, taskAgent, adapter, model string) string {
	name := stepAgent
	if name == "" {
		name = taskAgent
	}

	adapterModel := ""
	if adapter != "" && model != "" {
		adapterModel = adapter + "/" + model
	} else if model != "" {
		adapterModel = model
	}

	if name != "" {
		if adapterModel != "" {
			return name + " (" + adapterModel + ")"
		}
		return name
	}
	if adapterModel != "" {
		return adapterModel + " (no named agent)"
	}
	return "Worker"
}
