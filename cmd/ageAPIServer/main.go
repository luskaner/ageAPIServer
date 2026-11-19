package main

import (
	"os"

	"github.com/luskaner/ageAPIServer/internal/cmd"
	"github.com/luskaner/ageAPIServer/internal/logger"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "development"

func main() {
	logger.Initialize(os.Stdout)
	cmd.Version = version
	err, exitCode := cmd.Execute(os.Args[1:])
	if err != nil {
		logger.Fail("%s", err.Error())
	}
	os.Exit(exitCode)
}
