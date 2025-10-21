package main

import (
	"github.com/brocaar/lora-simulator/cmd/lora-simulator/cmd"
	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

var version string // set by the compiler

func main() {
	// 配置日志轮转
	logRotate := &lumberjack.Logger{
		Filename:   "simulator.log", // 日志文件路径
		MaxSize:    10,              // 单个日志文件的最大大小（MB），超过此大小会自动轮转
		MaxBackups: 5,              // 保留的旧日志文件的最大数量
		MaxAge:     300,             // 保留旧日志文件的最大天数
		Compress:   true,            // 是否压缩旧日志文件（压缩为.gz格式）
		LocalTime:  true,            // 使用本地时间而不是UTC时间
	}

	logrus.SetOutput(logRotate)

	//批量导入
	cmd.Execute(version)
}
