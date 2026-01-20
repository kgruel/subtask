# Golden snapshots (cmd/subtask)

These files snapshot the CLI output in two modes:

- `*.txt`: plain output (non-TTY / agent-friendly)
- `*.ansi`: pretty output (TTY / human), including ANSI escape codes

Covered commands:
- `subtask list` (`testdata/list/`)
- `subtask show` (`testdata/show/`)
- Representative error messages (`testdata/errors/`)

Regenerate snapshots:
```sh
go test ./cmd/subtask -update
```

If you're running a broader `go test` command that includes packages which don't
define `-update`, you can also use:
```sh
SUBTASK_UPDATE_GOLDEN=1 go test ./...
```

Tip: keep snapshots stable by avoiding machine-specific paths and nondeterministic time output in tests.

