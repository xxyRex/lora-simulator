package main

import (
	"github.com/brocaar/chirpstack-simulator/internal/as"
	"github.com/brocaar/chirpstack-simulator/internal/config"
	log "github.com/sirupsen/logrus"
)

// var version string // set by the compiler

func main() {
	//批量导入
	// cmd.Execute(version)
	log.Info(config.C.ChirpStack.API.Server)
	log.Info(as.LoginDeviceHub())
}
