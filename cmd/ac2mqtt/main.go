package main

import (
	"flag"

	"github.com/huguesb/ac2mqtt/common/logging"
	"github.com/huguesb/ac2mqtt/common/version"
	"github.com/huguesb/ac2mqtt/config"
	"github.com/huguesb/ac2mqtt/gateway"
	log "github.com/sirupsen/logrus"
)

func main() {
	configPath := flag.String("config", "./config.yml", "The path to the configuration")
	strictConfig := flag.Bool("strict-config", false, "Use strict parsing for the config file; will throw errors for invalid fields")
	versionFlag := flag.Bool("version", false, "Prints the version and exits")
	flag.Parse()

	if *versionFlag {
		version.Print()
		return
	}

	conf, err := config.ReadConfig(*configPath, *strictConfig)
	logging.Setup(conf.Logging) // logging should be set up with logging config before logging a possible error in the config, weird, I know
	if err != nil {
		log.WithError(err).Fatal("Failed to load config")
	}
	log.WithFields(log.Fields{
		"configfile": *configPath,
	}).Debug("Config loaded")

	g := gateway.NewGateway(conf)
	ctx := g.Start()
	<-ctx.Done()
}
