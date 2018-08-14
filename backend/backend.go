package backend

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"strings"

	"github.com/NiR-/prom-autoexporter/log"
	"github.com/NiR-/prom-autoexporter/models"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
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
			if ctx, err = b.runStartUpProcess(ctx, exporter); err != nil {
				logger := log.GetLogger(ctx)
				logger.Errorf("%+v", err)

				return
			}

			if ctx.Value("step") == stepFinished {
				return
			}
		}
	}
}

func (b Backend) runStartUpProcess(ctx context.Context, exporter models.Exporter) (context.Context, error) {
	logger := log.GetLogger(ctx).WithFields(logrus.Fields{
		"step":           ctx.Value("step"),
		"exported.name":  exporter.Exported.Name,
		"exporter.type":  exporter.Name,
		"exporter.image": exporter.Image,
	})
	ctx = log.WithLogger(ctx, logger)

	if ctx.Value("step") == stepCreate {
		cid, err := b.createContainer(ctx, exporter)
		if err != nil && isErrConflict(err) {
			logger.Warningf("Non-fatal error happened when creating exporter container")
		} else if err != nil {
			return ctx, err
		}

		ctx = context.WithValue(ctx, "exporter.id", cid)
		ctx = context.WithValue(ctx, "step", stepConnect)
		ctx = log.WithLogger(ctx, logger.WithFields(logrus.Fields{"exporter.cid": cid}))
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

func isErrConflict(err error) bool {
	ok, err := regexp.MatchString("The container name \"[^\"]+\" is already in use", err.Error())
	if err != nil {
		panic(err)
	}

	return ok
}

func (b Backend) createContainer(ctx context.Context, exporter models.Exporter) (string, error) {
	config := container.Config{
		User:  "1000",
		Cmd:   exporter.Cmd,
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
		return exporterName, errors.WithStack(err)
	}

	logger := log.GetLogger(ctx)
	logger.Debug("Exporter container created.")

	if len(container.Warnings) > 0 {
		logger.WithFields(logrus.Fields{
			"warnings": container.Warnings,
		}).Warning("Docker emitted warnings during container create.")
	}

	return exporterName, nil
}

func (b Backend) connectToNetwork(ctx context.Context, exporter models.Exporter, cid string) error {
	endpointSettings := network.EndpointSettings{}
	err := b.cli.NetworkConnect(ctx, exporter.PromNetwork, exporter.Exported.Name, &endpointSettings)

	if err != nil && strings.Contains(err.Error(), "endpoint with name") {
		return nil
	} else if err != nil {
		return errors.WithStack(err)
	}

	logger := log.GetLogger(ctx)
	logger.Debug("Exporter connected to prometheus network.")

	return nil
}

func (b Backend) startContainer(ctx context.Context, exporter models.Exporter, cid string) error {
	logger := log.GetLogger(ctx)
	logger.Debug("Starting exporter container.")

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

	logger := log.GetLogger(ctx)
	logger.WithFields(logrus.Fields{
		"exported.id":   exporter.Config.Labels[LABEL_EXPORTED_ID],
		"exported.name": exporter.Config.Labels[LABEL_EXPORTED_NAME],
	}).Info("Exporter container stopped.")

	return nil
}

func (b Backend) StartMissingExporters(ctx context.Context, promNetwork string) error {
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

		logger := log.GetLogger(ctx).WithFields(logrus.Fields{
			"exported.id":   container.ID,
			"exported.name": container.Names[0],
		})
		ctx := log.WithLogger(ctx, logger)

		err := b.handleContainerStart(ctx, container.ID, promNetwork)
		if err != nil {
			logger.Errorf("%+v", err)
		}
	}

	return nil
}

func (b Backend) CleanupStaleExporters(ctx context.Context) error {
	exporters, err := b.cli.ContainerList(ctx, types.ContainerListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", LABEL_EXPORTED_ID),
		),
	})

	if err != nil {
		return errors.WithStack(err)
	}

	logger := log.GetLogger(ctx)
	logger.Debugf("Found %d running exporters.", len(exporters))

	for _, container := range exporters {
		ctx := log.WithLogger(ctx, logger.WithFields(logrus.Fields{
			"exporter.cid":  container.ID,
			"exporter.name": container.Names[0],
		}))

		err := b.CleanupExporter(ctx, container.ID, false)
		if err != nil && !IsErrExportedStillRunning(err) {
			logger.Errorf("%+v", err)
		}
	}

	return nil
}

func (b Backend) CleanupAllExporters(ctx context.Context) error {
	exporters, err := b.cli.ContainerList(ctx, types.ContainerListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", LABEL_EXPORTED_ID),
		),
	})

	if err != nil {
		return errors.WithStack(err)
	}

	logger := log.GetLogger(ctx)
	logger.Debugf("Found %d exporters.", len(exporters))

	for _, container := range exporters {
		logger := logger.WithFields(logrus.Fields{
			"exporter.cid":  container.ID,
			"exporter.name": container.Names[0],
		})
		ctx := log.WithLogger(ctx, logger)

		err := b.CleanupExporter(ctx, container.ID, true)
		if err != nil {
			logger.Errorf("%+v", err)
		}
	}

	return nil
}

func (b Backend) CleanupExporter(ctx context.Context, cid string, force bool) error {
	exporter, err := b.cli.ContainerInspect(ctx, cid)
	if err != nil {
		return errors.WithStack(err)
	}

	// If the exported container is still alive and force is false, cleanup process is aborted
	exported, err := b.cli.ContainerInspect(ctx, exporter.Config.Labels[LABEL_EXPORTED_ID])
	if err != nil && !client.IsErrNotFound(err) {
		return errors.WithStack(err)
	} else if err == nil && exported.State.Running && !force {
		return newErrExportedStillRunning(cid, exporter.Config.Labels[LABEL_EXPORTED_ID])
	}

	return b.StopExporter(ctx, exporter)
}

func (b Backend) FindAssociatedExporter(ctx context.Context, exportedId string) (types.Container, bool, error) {
	containers, err := b.cli.ContainerList(ctx, types.ContainerListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", LABEL_EXPORTED_ID+"="+exportedId),
		),
	})

	if err != nil {
		return types.Container{}, false, errors.WithStack(err)
	}

	if len(containers) == 0 {
		return types.Container{}, false, nil
	}

	return containers[0], true, nil
}

func (b Backend) GetPromStaticConfigs(ctx context.Context, promNetwork string) ([]*models.StaticConfig, error) {
	var res []*models.StaticConfig

	exported, err := b.cli.TaskList(ctx, types.TaskListOptions{
		Filters: filters.NewArgs(
			filters.Arg("desired-state", "running"),
		),
	})
	if err != nil {
		return res, errors.WithStack(err)
	}

	exportedTasks := make(map[string]map[string]string)
	servicesName := make(map[string]string)
	staticConfigs := make(map[string]*models.StaticConfig, 0)

	for _, task := range exported {
		// When the task is a container, task.Spec.Runtime is empty
		// and thus does not match swarm.RuntimeContainer
		if task.Spec.Runtime == swarm.RuntimePlugin ||
			task.Spec.Runtime == swarm.RuntimeNetworkAttachment {
			continue
		}

		// We first check if an exporter name has been explicitly provided
		// Then we try to find a predefined exporter matching service name
		exporterName := task.Spec.ContainerSpec.Labels[LABEL_EXPORTER_NAME]
		if exporterName == "" {
			// Cache the service name, as multiple tasks might depend upon the same service
			if _, ok := servicesName[task.ServiceID]; !ok {
				service, _, err := b.cli.ServiceInspectWithRaw(ctx, task.ServiceID, types.ServiceInspectOptions{})
				if err != nil {
					return res, err
				}

				servicesName[task.ServiceID] = service.Spec.Name
			}

			exporterName = models.FindMatchingExporter(servicesName[task.ServiceID])
		}

		// Finally, this task is ignored if no exporter has been infered
		if exporterName == "" {
			continue
		}

		// Ignore this task too if the associated exporter does not exist
		if !models.PredefinedExporterExist(exporterName) {
			continue
		}

		exportedTasks[task.ID] = map[string]string{
			"exporter": exporterName,
			"service":  task.ServiceID,
		}
	}

	network, err := b.cli.NetworkInspect(ctx, promNetwork, types.NetworkInspectOptions{})
	if err != nil {
		return res, errors.WithStack(err)
	}

	for _, container := range network.Containers {
		for taskID, exported := range exportedTasks {
			exporterName := exported["exporter"]
			serviceID := exported["serviceID"]

			match, err := regexp.MatchString("\\."+taskID+"$", container.Name)
			if err != nil {
				return res, err
			} else if !match {
				continue
			}

			ip, _, err := net.ParseCIDR(container.IPv4Address)
			if err != nil {
				return res, err
			}

			if _, ok := staticConfigs[serviceID]; !ok {
				staticConfigs[serviceID] = models.NewStaticConfig()
			}

			port, err := models.GetExporterPort(exporterName)
			if err != nil {
				return res, err
			}

			// @TODO: add labels
			addr := fmt.Sprintf("%s:%s", ip.String(), port)
			staticConfigs[serviceID].AddTarget(addr)

			logger := log.GetLogger(ctx)
			logger.Debugf("Add exporter %s for %s", exporterName, addr)
		}
	}

	for _, conf := range staticConfigs {
		res = append(res, conf)
	}

	return res, nil
}
