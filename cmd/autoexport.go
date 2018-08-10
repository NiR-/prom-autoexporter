package cmd

import (
	"context"

	"github.com/NiR-/prom-autoexporter/backend"
	"github.com/NiR-/prom-autoexporter/log"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	cli "gopkg.in/urfave/cli.v1"
)

func AutoExport(c *cli.Context) {
	promNetwrk := c.String("network")
	ctx := context.Background()

	ctx = log.WithDefaultLogger(ctx)
	log.ConfigureDefaultLogger(c.String("level"))

	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		logrus.Errorf("%+v", errors.WithStack(err))
		return
	}

	defer cli.Close()
	cli.NegotiateAPIVersion(ctx)

	b := backend.NewBackend(cli)

	logrus.Info("Removing stale exporters...")

	if err := b.CleanupStaleExporters(ctx); err != nil {
		logrus.Errorf("%+v", err)
	}

	logrus.Info("Starting missing exporters...")

	if err := b.StartMissingExporters(ctx, promNetwrk); err != nil {
		logrus.Errorf("%+v", err)
	}

	logrus.Info("Start listening for new Docker events...")
	b.ListenEventsForExported(ctx, promNetwrk)
}
