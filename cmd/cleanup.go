package cmd

import (
	"context"

	"github.com/NiR-/prom-autoexporter/backend/docker"
	"github.com/NiR-/prom-autoexporter/models"
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

	b := docker.NewDockerBackend(cli, "", models.NewPredefinedExporterFinder())

	if err = b.CleanupExporters(ctx, true); err != nil {
		logrus.Fatal(err)
	}
}
