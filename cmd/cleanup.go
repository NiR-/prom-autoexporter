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

func Cleanup(c *cli.Context) {
	ctx := log.WithDefaultLogger(context.Background())
	log.ConfigureDefaultLogger(c.String("level"))

	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		logrus.Fatalf("%+v", errors.WithStack(err))
	}

	defer cli.Close()
	cli.NegotiateAPIVersion(ctx)

	b := backend.NewBackend(cli)

	if err := b.CleanupAllExporters(ctx); err != nil {
		logrus.Fatalf("%+v", err)
	}
}
