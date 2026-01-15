package main

import (
	"os"

	"github.com/gshaibi/kubectl-create-resource/pkg/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
