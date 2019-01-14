package swarm

import (
  "context"
  "strconv"

  "github.com/docker/docker/client"
  "github.com/NiR-/prom-autoexporter/backend/docker"
  "github.com/NiR-/prom-autoexporter/models"
)

type SwarmBackend struct {
  cli *client.Client
  docker docker.DockerBackend
  promNetwork string
}

func NewSwarmBackend(cli *client.Client, promNetwork string) SwarmBackend {
  return SwarmBackend{
    cli,
    NewDockerBackend(cli, promNetwork),
    promNetwork,
  }
}

func (b SwarmBackend) RunExporter(ctx context.Context, exporter models.Exporter) error {
  return b.docker.RunExporter(ctx, exporter)
}

func (b SwarmBackend) StopExporter(ctx context.Context, exporterName string) error {
  return b.docker.StopExporter(ctx, exporterName)
}

func (b SwarmBackend) FindMissingExporters(ctx context.Context) error {
  return b.docker.FindMissingExporters(ctx)
}

func (b SwarmBackend) GetPromStaticConfig(ctx context.Context) (*models.StaticConfig, error) {
	endpoints, err := b.listPromNetworkEndpoints(ctx)
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

func (b SwarmBackend) listPromNetworkEndpoints(ctx context.Context) (map[string]string, error) {
	network, err := b.cli.NetworkInspect(ctx, b.promNetwork, types.NetworkInspectOptions{})

	if err != nil {
		return nil, errors.WithStack(err)
	}

	endpoints := map[string]string{}
	for _, c := range network.Containers {
		endpoints[c.Name] = c.IPv4Address
	}

	return endpoints, nil
}
