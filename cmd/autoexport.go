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
	promNetwork := c.String("network")
	forceRecreate := c.Bool("force-recreate")

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

	logrus.Info("Removing stale exporters...")

	if forceRecreate {
		if err := b.CleanupAllExporters(ctx); err != nil {
			logrus.Errorf("%+v", err)
		}
	} else {
		if err := b.CleanupStaleExporters(ctx); err != nil {
			logrus.Errorf("%+v", err)
		}
	}

	logrus.Info("Starting missing exporters...")

	if err := b.StartMissingExporters(ctx, promNetwork); err != nil {
		logrus.Errorf("%+v", err)
	}

	logrus.Info("Start listening for new Docker events...")
	b.ListenEventsForExported(ctx, promNetwork)
}
