package main

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/strslice"
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"
)

type Exporter struct {
	Image    string
	Cmd      string
	Exported types.ContainerJSON
}

func NewExporter(p PredefinedExporter, container types.ContainerJSON) (Exporter, error) {
	cmd, err := tplToStr(p.Cmd, container)
	if err != nil {
		return Exporter{}, err
	}

	exporter := Exporter{
		Image:    p.Image,
		Cmd:      cmd,
		Exported: container,
	}

	return exporter, err
}

func runExporter(ctx context.Context, cli *client.Client, exporter Exporter, cfg *config) error {
	config := container.Config{
		User:  "1000",
		Cmd:   strslice.StrSlice{exporter.Cmd},
		Image: exporter.Image,
		Labels: map[string]string{
			LABEL_EXPORTED_ID:   exporter.Exported.ID,
			LABEL_EXPORTED_NAME: exporter.Exported.Name,
		},
	}
	hostConfig := container.HostConfig{
		NetworkMode: container.NetworkMode(fmt.Sprintf("container:%s", exporter.Exported.ID)),
		RestartPolicy: container.RestartPolicy{
			Name:              "on-failure",
			MaximumRetryCount: 10,
		},
	}
	networkingConfig := network.NetworkingConfig{}

	exporterName := fmt.Sprintf("%s-exporter", exporter.Exported.Name)
	container, err := cli.ContainerCreate(ctx, &config, &hostConfig, &networkingConfig, exporterName)
	if err != nil {
		return err
	}

	logrus.WithFields(logrus.Fields{
		"exporter.id":   container.ID,
		"exported.name": exporter.Exported.Name,
		"image":         exporter.Image,
		"cmd":           exporter.Cmd,
	}).Info("Exporter container created.")

	if len(container.Warnings) > 0 {
		logrus.WithFields(logrus.Fields{
			"warnings": container.Warnings,
		}).Warning("Docker emitted warnings during container create.")
	}

	endpointSettings := network.EndpointSettings{}
	err = cli.NetworkConnect(ctx, cfg.network, exporter.Exported.ID, &endpointSettings)
	if err != nil {
		return err
	}

	logrus.WithFields(logrus.Fields{
		"exporter.id":   container.ID,
		"exporter.name": exporter,
		"exported.name": exporter.Exported.Name,
		"network":       cfg.network,
	}).Info("Exporter connected to prometheus network.")

	err = cli.ContainerStart(ctx, container.ID, types.ContainerStartOptions{})
	if err != nil {
		return err
	}

	logrus.WithFields(logrus.Fields{
		"exporter.id":   container.ID,
		"exporter.name": exporter,
		"image":         exporter.Image,
		"cmd":           exporter.Cmd,
	}).Info("Exporter container started.")

	return nil
}

func stopExporter(ctx context.Context, cli *client.Client, exported string) error {
	exporter := fmt.Sprintf("%s-exporter", exported)
	err := cli.ContainerStop(ctx, exporter, nil)
	if err != nil {
		return err
	}

	err = cli.ContainerRemove(ctx, exporter, types.ContainerRemoveOptions{
		Force: true,
	})
	if err != nil {
		return err
	}

	logrus.WithFields(logrus.Fields{
		"exporter.name": exporter,
		"exported.name": exported,
	}).Info("Exporter container stopped.")

	return nil
}

func cleanupExporter(ctx context.Context, cli *client.Client, exporter types.Container) error {
	// If the exported container is still alive, we stop cleanup process
	_, err := cli.ContainerInspect(ctx, exporter.Labels[LABEL_EXPORTED_NAME])
	if err == nil {
		return nil
	}
	if !client.IsErrNotFound(err) {
		return err
	}

	stopExporter(ctx, cli, exporter.Labels[LABEL_EXPORTED_NAME])

	return nil
}
