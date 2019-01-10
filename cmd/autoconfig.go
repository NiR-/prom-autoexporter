package cmd

import (
	"context"
	"os"
	"time"

	"github.com/NiR-/prom-autoexporter/backend"
	"github.com/NiR-/prom-autoexporter/log"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	cli "gopkg.in/urfave/cli.v1"
)

func AutoConfig(c *cli.Context) {
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

	b := backend.NewBackend(cli)
	t := time.NewTicker(interval)

	reconfigure := func() {
		if err := reconfigurePrometheus(ctx, b, promNetwork, filepath); err != nil {
			logrus.Errorf("%+v", err)
		}
	}

	go reconfigure()

	for {
		select {
		case _ = <-t.C:
			reconfigure()
		}
	}
}

func reconfigurePrometheus(ctx context.Context, b backend.Backend, promNetwork string, filepath string) error {
	logrus.Info("Reconfiguring prometheus...")

	staticConfig, err := b.GetPromStaticConfig(ctx, promNetwork)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(filepath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	configfile, err := staticConfig.ToJSON()
	if err != nil {
		return err
	}

	f.Write(configfile)

	return nil
}
