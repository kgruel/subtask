# Golden snapshots (harness)

These files snapshot prompt construction output from `harness.BuildPrompt`:
- `testdata/prompt/`

Regenerate snapshots:
```sh
go test ./harness -update
```

If you're running a broader `go test` command that includes packages which don't
define `-update`, you can also use:
```sh
SUBTASK_UPDATE_GOLDEN=1 go test ./...
```
