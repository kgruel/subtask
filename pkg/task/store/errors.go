package store

import "errors"

var (
	ErrBranchDeleted   = errors.New("branch deleted")
	ErrBranchMissing   = errors.New("branch missing")
	ErrBaseMissing     = errors.New("base missing")
	ErrCommitMissing   = errors.New("commit missing")
	ErrMergeBaseMissing = errors.New("merge-base missing")
	ErrGitNotRepo      = errors.New("not a git repository")
)

