package main

import (
	"github.com/brocaar/chirpstack-simulator/cmd/chirpstack-simulator/cmd"
)

var version string // set by the compiler

func main() {
	//批量导入
	cmd.Execute(version)
}
