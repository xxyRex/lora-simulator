package config

import (
	"time"
)

// Version defines the version.
var Version string

// Config defines the configuration.
type Config struct {
	General struct {
		LogLevel    int    `mapstructure:"log_level"`
		ChannelPlan string `mapstructure:"channel_plan"`
	}

	LoraSimulator struct {
		API struct {
			Server               string        `mapstructure:"server"`
			LoginUrl             string        `mapstructure:"login_url"`
			Username             string        `mapstructure:"username"`
			Password             string        `mapstructure:"password"`
			CleanBeforeTest      bool          `mapstructure:"clean_before_test"`
			TeardownAfterTest    bool          `mapstructure:"teardown_after_test"`
			TestFeature          string        `mapstructure:"test_feature"`
			Insecure             bool          `mapstructure:"insecure"`
			SshPassword          string        `mapstructure:"ssh_password"`
			RestartAs            bool          `mapstructure:"restart_as"`
			FuotaTaskDeviceCount int           `mapstructure:"fuota_task_device_count"`
			UseNewDevice         bool          `mapstructure:"use_new_device"`
			ApiTest              bool          `mapstructure:"api_test"`
		} `mapstructure:"api"`

		Integration struct {
			MQTT struct {
				Server   string `mapstructure:"server"`
				Username string `mapstructure:"username"`
				Password string `mapstructure:"password"`
			} `mapstructure:"mqtt"`
		} `mapstructure:"integration"`

		Gateway struct {
			Backend struct {
				MQTT struct {
					Server   string `mapstructure:"server"`
					Username string `mapstructure:"username"`
					Password string `mapstructure:"password"`
				} `mapstructure:"mqtt"`
			} `mapstructure:"backend"`
		} `mapstructure:"gateway"`

		TestPayloadCodec struct {
			OldHost         string `mapstructure:"old_host"`
			NewHost         string `mapstructure:"new_host"`
			Enable          bool   `mapstructure:"enable"`
			TestCaseFile    string `mapstructure:"test_case_file"`
			TestSheet       string `mapstructure:"test_sheet"`
			TestDevice      string `mapstructure:"test_device"`
			TestResultDir   string `mapstructure:"test_result_dir"`
			TestCodecDir    string `mapstructure:"test_codec_dir"`
			TestDeviceSheet []struct {
				Sheet  string `mapstructure:"sheet"`
				Device string `mapstructure:"device"`
			} `mapstructure:"test_device_sheet"`
		} `mapstructure:"test_payload_codec"`
	} `mapstructure:"lora-simulator"`

	Simulator []struct {
		TenantID             string        `mapstructure:"tenant_id"`
		Duration             time.Duration `mapstructure:"duration"`
		SequenceJoin         bool          `mapstructure:"sequence_join"`
		SequenceJoinInterval time.Duration `mapstructure:"sequence_join_interval"`
		SequenceDeviceNumber int           `mapstructure:"sequence_device_number"`
		ActivationTime       time.Duration `mapstructure:"activation_time"`

		Device struct {
			Count                int           `mapstructure:"count"`
			WaitDeviceStableTime time.Duration `mapstructure:"wait_device_stable_time"`
			UplinkInterval       time.Duration `mapstructure:"uplink_interval"`
			FPort                uint8         `mapstructure:"f_port"`
			Payload              string        `mapstructure:"payload"`
			Frequency            int           `mapstructure:"frequency"`
			Bandwidth            int           `mapstructure:"bandwidth"`
			SpreadingFactor      int           `mapstructure:"spreading_factor"`
		} `mapstructure:"device"`

		Gateway struct {
			MinCount             int    `mapstructure:"min_count"`
			MaxCount             int    `mapstructure:"max_count"`
			EventTopicTemplate   string `mapstructure:"event_topic_template"`
			CommandTopicTemplate string `mapstructure:"command_topic_template"`
		} `mapstructure:"gateway"`
	} `mapstructure:"simulator"`

	Prometheus struct {
		Bind string `mapstructure:"bind"`
	} `mapstructure:"prometheus"`
}

type DeviceConfig struct {
	DevEUI         string        `mapstructure:"dev_eui"`
	AppKey         string        `mapstructure:"app_key"`
	UplinkInterval time.Duration `mapstructure:"uplink_interval"`
	FPort          uint8         `mapstructure:"f_port"`
	Payload        string        `mapstructure:"payload"`
}

// C holds the global configuration.
var C Config
