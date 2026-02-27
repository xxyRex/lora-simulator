package main

import (
	"github.com/brocaar/lora-simulator/cmd/lora-simulator/cmd"
)

var version string // set by the compiler

func main() {
	cmd.Execute(version)
}
