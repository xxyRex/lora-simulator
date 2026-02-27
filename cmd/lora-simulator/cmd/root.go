package cmd

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/brocaar/lora-simulator/internal/config"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string
var workdir string
var version string

// Execute executes the root command.
func Execute(v string) {
	version = v
	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}

var rootCmd = &cobra.Command{
	Use:   "lora-simulator",
	Short: "Lora Simulator",
	Long:  `Lora Simulator simulates device uplinks`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// 切换工作目录（所有相对路径均基于此目录）
		if workdir != "" {
			// 保存原始目录作为共享只读资源（payload_en_decoder 等）的基准路径
			originalDir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get working directory: %w", err)
			}
			config.BaseDir = originalDir

			if err := os.Chdir(workdir); err != nil {
				return fmt.Errorf("failed to change working directory to %q: %w", workdir, err)
			}
			log.Infof("working directory changed to: %s (base dir: %s)", workdir, originalDir)
		}

		// 在工作目录下打开日志文件
		file, err := os.OpenFile("simulator.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
		if err != nil {
			return fmt.Errorf("failed to open simulator.log: %w", err)
		}
		log.SetOutput(file)
		return nil
	},
	RunE: run,
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "path to configuration file (optional)")
	rootCmd.PersistentFlags().StringVar(&workdir, "workdir", "", "working directory for this instance (configs, logs, temp all relative to this path)")
	rootCmd.PersistentFlags().Int("log-level", 4, "debug=5, info=4, error=2, fatal=1, panic=0")

	viper.BindPFlag("general.log_level", rootCmd.PersistentFlags().Lookup("log-level"))

	viper.SetDefault("application_server.api.server", "127.0.0.1:8080")
	viper.SetDefault("application_server.integration.mqtt.server", "tcp://127.0.0.1:1883")
	viper.SetDefault("network_server.gateway.backend.mqtt.server", "tcp://127.0.0.1:1883")
	viper.SetDefault("prometheus.bind", "0.0.0.0:9000")

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(configCmd)
}

func initConfig() {
	config.Version = version

	if cfgFile != "" {
		b, err := ioutil.ReadFile(cfgFile)
		if err != nil {
			log.WithError(err).WithField("config", cfgFile).Fatal("error loading config file")
		}
		viper.SetConfigType("toml")
		if err := viper.ReadConfig(bytes.NewBuffer(b)); err != nil {
			log.WithError(err).WithField("config", cfgFile).Fatal("error loading config file")
		}
	} else {
		viper.SetConfigName("lora-simulator")
		viper.AddConfigPath(".")
		viper.AddConfigPath("$HOME/.config/lora-simulator")
		viper.AddConfigPath("/etc/lora-simulator")
		if err := viper.ReadInConfig(); err != nil {
			switch err.(type) {
			case viper.ConfigFileNotFoundError:
			default:
				log.WithError(err).Fatal("read configuration file error")
			}
		}
	}

	if err := viper.Unmarshal(&config.C); err != nil {
		log.WithError(err).Fatal("unmarshal config error")
	}
}

