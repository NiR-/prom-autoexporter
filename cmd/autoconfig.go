package cmd

import (
	"context"
	"os"
	"time"

	"github.com/NiR-/prom-autoexporter/backend"
	"github.com/NiR-/prom-autoexporter/log"
	"github.com/NiR-/prom-autoexporter/models"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	cli "gopkg.in/urfave/cli.v1"
)

func AutoConfig(c *cli.Context) {
	// @TODO: validate promNetwork isn't empty / interval default value
	promNetwork := c.String("network")
	interval := c.Duration("interval")
	filepath := c.String("filepath")

	ctx := log.WithDefaultLogger(context.Background())
	log.ConfigureDefaultLogger(c.String("level"))

	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		logrus.Errorf("%+v", errors.WithStack(err))
		return
	}

	defer cli.Close()
	cli.NegotiateAPIVersion(ctx)

	b := backend.NewSwarmBackend(cli, promNetwork, models.NewPredefinedExporterFinder())
	go reconfigurePrometheus(ctx, b, filepath)

	t := time.NewTicker(interval)
	for {
		select {
		case _ = <-t.C:
			reconfigurePrometheus(ctx, b, filepath)
		}
	}
}

func reconfigurePrometheus(ctx context.Context, b backend.Backend, filepath string) {
	logger := log.GetLogger(ctx)
	logger.Info("Reconfiguring prometheus...")

	staticConfig, err := b.GetPromStaticConfig(ctx)
	if err != nil {
		logger.Error(err)
		return
	}

	f, err := os.OpenFile(filepath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		logger.Error(err)
		return
	}
	defer f.Close()

	configfile, err := staticConfig.ToJSON()
	if err != nil {
		logger.Error(err)
		return
	}

	f.Write(configfile)
}
