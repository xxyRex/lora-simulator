package main

import (
	"os"

	"github.com/brocaar/lora-simulator/cmd/lora-simulator/cmd"
	"github.com/sirupsen/logrus"
)

var version string // set by the compiler

func main() {
	file, err := os.OpenFile("simulator.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		logrus.Fatal(err)
	}
	defer file.Close()
	logrus.SetOutput(file)

	//批量导入
	cmd.Execute(version)
}
