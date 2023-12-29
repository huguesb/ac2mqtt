package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type HomeAssistant struct {
	Enabled  *bool   `yaml:"enabled,omitempty"`
	LWTTopic *string `yaml:"lwt_topic,omitempty"`
}

type MQTT struct {
	Enabled           *bool          `yaml:"enabled,omitempty"`
	RefreshPeriod     *time.Duration `yaml:"refresh_period,omitempty"`
	BrokerUrl         string         `yaml:"broker_url"`
	BrokerAddress     string         `yaml:"broker_address"`
	BrokerPort        int            `yaml:"broker_port"`
	ClientID          string         `yaml:"client_id"`
	Username          string         `yaml:"username"`
	Password          string         `yaml:"password"`
	TopicPrefix       string         `yaml:"topic_prefix"`
	LWTTopic          *string        `yaml:"lwt_topic,omitempty"`
	LWTOnlinePayload  string         `yaml:"lwt_online_payload"`
	LWTOfflinePayload string         `yaml:"lwt_offline_payload"`
}

type Logging struct {
	Type       string `yaml:"type"`
	Level      string `yaml:"level"`
	Timestamps *bool  `yaml:"timestamps,omitempty"`
	WithCaller bool   `yaml:"with_caller,omitempty"`
}

type Config struct {
	HciIndex      int            `yaml:"hci_index"`
	MQTT          *MQTT          `yaml:"mqtt,omitempty"`
	HomeAssistant *HomeAssistant `yaml:"homeassistant,omitempty"`
	Logging       Logging        `yaml:"logging"`
	Debug         bool           `yaml:"debug"`
}

func ReadConfig(configFile string, strict bool) (Config, error) {
	if _, err := os.Stat(configFile); errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("no config found! Tried to open \"%s\"", configFile)
	}

	f, err := os.Open(configFile)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()

	var conf Config
	decoder := yaml.NewDecoder(f)
	decoder.KnownFields(strict)
	err = decoder.Decode(&conf)

	if err != nil {
		return Config{}, err
	}
	return conf, nil
}
