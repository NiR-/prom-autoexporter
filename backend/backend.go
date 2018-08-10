package backend

import (
	"context"
	"fmt"
	"strings"

	"github.com/NiR-/prom-autoexporter/models"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/strslice"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	LABEL_EXPORTED_ID   = "autoexporter.exported.id"
	LABEL_EXPORTED_NAME = "autoexporter.exported.name"
	LABEL_EXPORTER_NAME = "autoexporter.exporter"

	stepCreate   = "create"
	stepConnect  = "connect"
	stepStart    = "start"
	stepFinished = "finished"
)

type Backend struct {
	cli *client.Client
}

func NewBackend(cli *client.Client) Backend {
	return Backend{cli}
}

func (b Backend) RunExporter(ctx context.Context, exporter models.Exporter) {
	var err error
	ctx = context.WithValue(ctx, "step", stepCreate)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			if ctx, err = b.startUpProcess(ctx, exporter); err != nil {
				logrus.Errorf("%+v", err)
				return
			}

			if ctx.Value("step") == stepFinished {
				return
			}
		}
	}
}

func (b Backend) startUpProcess(ctx context.Context, exporter models.Exporter) (context.Context, error) {
	if ctx.Value("step") == stepCreate {
		cid, err := b.createContainer(ctx, exporter)
		if err != nil {
			return ctx, err
		}

		ctx = context.WithValue(ctx, "exporter.id", cid)
		ctx = context.WithValue(ctx, "step", stepConnect)
	} else if ctx.Value("step") == stepConnect {
		err := b.connectToNetwork(ctx, exporter, ctx.Value("exporter.id").(string))
		if err != nil {
			return ctx, err
		}

		ctx = context.WithValue(ctx, "step", stepStart)
	} else if ctx.Value("step") == stepStart {
		err := b.startContainer(ctx, exporter, ctx.Value("exporter.id").(string))
		if err != nil {
			return ctx, err
		}

		ctx = context.WithValue(ctx, "step", stepFinished)
	}

	return ctx, nil
}

func (b Backend) createContainer(ctx context.Context, exporter models.Exporter) (string, error) {
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
	container, err := b.cli.ContainerCreate(ctx, &config, &hostConfig, &networkingConfig, exporterName)
	if err != nil {
		return "", errors.WithStack(err)
	}

	logrus.WithFields(logrus.Fields{
		"exporter.id":   container.ID,
		"exported.name": exporter.Exported.Name,
		"image":         exporter.Image,
	}).Info("Exporter container created.")

	if len(container.Warnings) > 0 {
		logrus.WithFields(logrus.Fields{
			"warnings": container.Warnings,
		}).Warning("Docker emitted warnings during container create.")
	}

	return container.ID, nil
}

func (b Backend) connectToNetwork(ctx context.Context, exporter models.Exporter, cid string) error {
	endpointSettings := network.EndpointSettings{}
	err := b.cli.NetworkConnect(ctx, exporter.PromNetwork, exporter.Exported.Name, &endpointSettings)

	if err != nil && strings.Contains(err.Error(), "endpoint with name") {
		return nil
	} else if err != nil {
		return errors.WithStack(err)
	}

	logrus.WithFields(logrus.Fields{
		"exporter.id":   cid,
		"exporter.name": exporter.Name,
		"exported.name": exporter.Exported.Name,
	}).Info("Exporter connected to prometheus network.")

	return nil
}

func (b Backend) startContainer(ctx context.Context, exporter models.Exporter, cid string) error {
	logrus.WithFields(logrus.Fields{
		"exporter.id":   cid,
		"exporter.type": exporter.Name,
		"image":         exporter.Image,
	}).Info("Starting exporter container.")

	err := b.cli.ContainerStart(ctx, cid, types.ContainerStartOptions{})
	if err != nil {
		return errors.WithStack(err)
	}

	return nil
}

func (b Backend) StopExporter(ctx context.Context, exporter types.ContainerJSON) error {
	err := b.cli.ContainerStop(ctx, exporter.ID, nil)
	if err != nil {
		return errors.WithStack(err)
	}

	err = b.cli.ContainerRemove(ctx, exporter.ID, types.ContainerRemoveOptions{
		Force: true,
	})
	if err != nil {
		return errors.WithStack(err)
	}

	logrus.WithFields(logrus.Fields{
		"exporter.id":   exporter.ID,
		"exporter.name": exporter.Name,
		"exported.id":   exporter.Config.Labels[LABEL_EXPORTED_ID],
		"exported.name": exporter.Config.Labels[LABEL_EXPORTED_NAME],
	}).Info("Exporter container stopped.")

	return nil
}

func (b Backend) StartMissingExporters(ctx context.Context, promNetwrk string) error {
	containers, err := b.cli.ContainerList(ctx, types.ContainerListOptions{})
	if err != nil {
		return errors.WithStack(err)
	}

	containerNames := make(map[string]string, 0)
	for _, container := range containers {
		for _, name := range container.Names {
			containerNames[name] = name
		}
	}

	// Iterate over containers to find which one should have an associated
	// exporter running but does not have one
	for _, container := range containers {
		// Ignore exporters
		if _, ok := container.Labels[LABEL_EXPORTED_NAME]; ok {
			continue
		}

		exporterName := fmt.Sprintf("%s-exporter", container.Names[0])
		if _, ok := containerNames[exporterName]; ok {
			continue
		}

		err := b.handleContainerStart(ctx, container.ID, promNetwrk)
		if err != nil {
			return err
		}
	}

	return nil
}

func (b Backend) CleanupStaleExporters(ctx context.Context) error {
	exporters, err := b.cli.ContainerList(ctx, types.ContainerListOptions{
		Filters: filters.NewArgs(filters.KeyValuePair{
			Key:   "label",
			Value: LABEL_EXPORTED_ID,
		}),
	})

	if err != nil {
		return errors.WithStack(err)
	}

	logrus.Debugf("Found %d running exporters.", len(exporters))

	for _, container := range exporters {
		err := b.CleanupExporter(ctx, container.ID)
		if err != nil && !IsErrExportedStillRunning(err) {
			logrus.Errorf("%+v", err)
		}
	}

	return nil
}

func (b Backend) CleanupExporter(ctx context.Context, cid string) error {
	exporter, err := b.cli.ContainerInspect(ctx, cid)
	if err != nil {
		return errors.WithStack(err)
	}

	// If the exported container is still alive, cleanup process is aborted
	exported, err := b.cli.ContainerInspect(ctx, exporter.Config.Labels[LABEL_EXPORTED_ID])
	if err != nil && !client.IsErrNotFound(err) {
		return errors.WithStack(err)
	} else if err == nil && exported.State.Running {
		// @TODO: handle this case gracefully -displays an error message right now)
		return newErrExportedStillRunning(cid, exporter.Config.Labels[LABEL_EXPORTED_ID])
	}

	return b.StopExporter(ctx, exporter)
}

func (b Backend) FindAssociatedExporter(ctx context.Context, exportedId string) (types.Container, bool, error) {
	containers, err := b.cli.ContainerList(ctx, types.ContainerListOptions{
		Filters: filters.NewArgs(filters.KeyValuePair{
			Key:   "label",
			Value: LABEL_EXPORTED_ID + "=" + exportedId,
		}),
	})

	if err != nil {
		return types.Container{}, false, errors.WithStack(err)
	}

	if len(containers) == 0 {
		return types.Container{}, false, nil
	}

	return containers[0], true, nil
}
