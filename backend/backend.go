package backend

import (
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"strings"
	"strconv"

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

	stepPullImage = "pullImage"
	stepCreate    = "create"
	stepConnect   = "connect"
	stepStart     = "start"
	stepFinished  = "finished"
)

type Backend struct {
	cli *client.Client
}

func NewBackend(cli *client.Client) Backend {
	return Backend{cli}
}

type process struct {
	exporter    models.Exporter
	step        string
	exporterCID string
}

func (b Backend) RunExporter(ctx context.Context, exporter models.Exporter) {
	var err error

	logger := log.GetLogger(ctx).WithFields(logrus.Fields{
		"exported.name":  exporter.Exported.Name,
		"exporter.type":  exporter.PredefinedType,
		"exporter.name":  exporter.Name,
		"exporter.image": exporter.Image,
	})

	ctx = log.WithLogger(ctx, logger)

	p := process{exporter, stepPullImage, ""}

	for {
		select {
		case <-ctx.Done():
			return
		default:
			logFields := logrus.Fields{"step": p.step}
			if p.exporterCID != "" {
				logFields["exporter.cid"] = p.exporterCID
			}

			logger = logger.WithFields(logFields)
			ctx = log.WithLogger(ctx, logger)

			// The startup process is decomposed into several steps executed serially,
			// in order to cancel the startup as soon as possible
			switch p.step {
			case stepPullImage:
				err = b.pullImage(ctx, exporter.Image)
				p.step = stepCreate
			case stepCreate:
				cid, err := b.createContainer(ctx, p.exporter)

				if err == nil {
					p.exporterCID = cid
				}

				p.step = stepConnect
			case stepConnect:
				err = b.connectToNetwork(ctx, p.exporter, p.exporterCID)
				p.step = stepStart
			case stepStart:
				err = b.startContainer(ctx, p.exporter, p.exporterCID)
				p.step = stepFinished
			case stepFinished:
				return
			default:
				err = errors.New(fmt.Sprintf("undefined step %s", p.step))
			}

			if err != nil {
				logger.Errorf("%+v", err)
				return
			}
		}
	}
}

func (b Backend) pullImage(ctx context.Context, image string) error {
	logger := log.GetLogger(ctx)
	logger.Debugf("Pulling image %q", image)

	rc, err := b.cli.ImagePull(ctx, image, types.ImagePullOptions{})
	if err != nil {
		return errors.WithStack(err)
	}

	// Wait until image pulling ends (= when rc is closed)
	if _, err := ioutil.ReadAll(rc); err != nil {
		return errors.WithStack(err)
	}

	return nil
}

func (b Backend) createContainer(ctx context.Context, exporter models.Exporter) (string, error) {
	config := container.Config{
		User:   "1000",
		Cmd:    exporter.Cmd,
		Image:  exporter.Image,
		Env:    exporter.EnvVars,
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

	container, err := b.cli.ContainerCreate(ctx, &config, &hostConfig, &networkingConfig, exporter.Name)
	if err != nil {
		return exporter.Name, errors.WithStack(err)
	}

	logger := log.GetLogger(ctx)
	logger.Debug("Exporter container created.")

	if len(container.Warnings) > 0 {
		logger.WithFields(logrus.Fields{
			"warnings": container.Warnings,
		}).Warning("Docker emitted warnings during container create.")
	}

	return exporter.Name, nil
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
	logger.Info("Exporter container stopped.")

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
	// exporter running but does not
	for _, container := range containers {
		// Ignore exporters
		if _, ok := container.Labels[LABEL_EXPORTED_NAME]; ok {
			continue
		}

		exporterName := getExporterName(container.Names[0])
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
		logger = logger.WithFields(logrus.Fields{
			"exporter.cid":  container.ID,
			"exporter.name": container.Names[0],
		})
		ctx := log.WithLogger(ctx, logger)

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
	logger.Debugf("Found %d exporters to clean up...", len(exporters))

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

	exportedTaskId := exporter.Config.Labels[LABEL_EXPORTED_ID]
	exported, err := b.cli.ContainerInspect(ctx, exportedTaskId)

	if err != nil && !client.IsErrNotFound(err) {
		return errors.WithStack(err)
	} else if err == nil && exported.State.Running && !force {
		return newErrExportedTaskStillRunning(cid, exportedTaskId)
	}

	logger := log.GetLogger(ctx).WithFields(logrus.Fields{
		"exported.id":   exportedTaskId,
		"exported.name": exporter.Config.Labels[LABEL_EXPORTED_NAME],
	})
	ctx = log.WithLogger(ctx, logger)

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

func (b Backend) GetPromStaticConfig(ctx context.Context, promNetwork string) (*models.StaticConfig, error) {
	endpoints, err := b.listNetworkEndpoints(ctx, promNetwork)
	if err != nil {
		return nil, err
	}

	tasks, err := b.cli.TaskList(ctx, types.TaskListOptions{
		Filters: filters.NewArgs(
			filters.Arg("desired-state", "running"),
		),
	})
	if err != nil {
		return nil, errors.WithStack(err)
	}

	services := map[string]string{}
	staticConfig := models.NewStaticConfig()
	logger := log.GetLogger(ctx)

	for _, task := range tasks {
		if task.Spec.Runtime == swarm.RuntimePlugin ||
			task.Spec.Runtime == swarm.RuntimeNetworkAttachment {
			continue
		}

		// Cache the service name associated to the task ServiceID,
		// as multiple tasks might come from the same service
		if _, ok := services[task.ServiceID]; !ok {
			service, _, err := b.cli.ServiceInspectWithRaw(ctx, task.ServiceID, types.ServiceInspectOptions{})
			if err != nil {
				return nil, errors.WithStack(err)
			}

			services[task.ServiceID] = service.Spec.Name
		}

		taskName := fmt.Sprintf("%s.%d.%s", services[task.ServiceID], task.Slot, task.ID)
		if _, ok := endpoints[taskName]; !ok {
			continue
		}

		// We first check if an exporter name has been explicitly provided
		// Then we try to find a predefined exporter matching service name
		exporterType := task.Spec.ContainerSpec.Labels[LABEL_EXPORTER_NAME]
		if exporterType == "" {
			exporterType = models.FindMatchingExporter(services[task.ServiceID])
		}

		// Finally, this task is ignored if no exporter has been infered
		if exporterType == "" {
			continue
		}

		// Ignore this task, if the associated exporter does not exist
		if !models.PredefinedExporterExist(exporterType) {
			continue
		}

		ip, _, err := net.ParseCIDR(endpoints[taskName])
		if err != nil {
			logger.Error(err)
			continue
		}

		port, err := models.GetExporterPort(exporterType)
		if err != nil {
			logger.Error(err)
			continue
		}

		target := fmt.Sprintf("%s:%s", ip.String(), port)
		labels := map[string]string{
			"job": fmt.Sprintf("autoexporter-%s", exporterType),
			"swarm_service_name": services[task.ServiceID],
			"swarm_task_slot": strconv.Itoa(task.Slot),
			"swarm_task_id": task.ID,
		}

		staticConfig.AddTarget(target, labels)
		logger.WithFields(logrus.Fields{
			"labels": labels,
		}).Debugf("Add exporter %s for target %s", exporterType, target)
	}

	return staticConfig, nil
}

func (b Backend) listNetworkEndpoints(ctx context.Context, networkName string) (map[string]string, error) {
	network, err := b.cli.NetworkInspect(ctx, networkName, types.NetworkInspectOptions{})

	if err != nil {
		return nil, errors.WithStack(err)
	}

	endpoints := map[string]string{}
	for _, c := range network.Containers {
		endpoints[c.Name] = c.IPv4Address
	}

	return endpoints, nil
}

func getExporterName(containerName string) string {
	return fmt.Sprintf("/exporter.%s", strings.TrimLeft(containerName, "/"))
}
