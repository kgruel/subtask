package main

import "github.com/kgruel/subtask/pkg/task/ops"

type cliOpsLogger struct{}

func (cliOpsLogger) Info(msg string) {
	printInfo(msg)
}

func (cliOpsLogger) Warning(msg string) {
	printWarning(msg)
}

func (cliOpsLogger) Success(msg string) {
	printSuccess(msg)
}

var _ ops.Logger = cliOpsLogger{}
