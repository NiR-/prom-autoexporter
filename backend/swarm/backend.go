package swarm

import (
	"context"
	"strconv"
	"fmt"
	"net"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/NiR-/prom-autoexporter/backend/docker"
	"github.com/NiR-/prom-autoexporter/log"
	"github.com/NiR-/prom-autoexporter/models"
	"github.com/sirupsen/logrus"
	"github.com/pkg/errors"
)

type SwarmBackend struct {
	*docker.DockerBackend

	cli         client.APIClient
	promNetwork string
	finder      models.ExporterFinder
}

func NewSwarmBackend(cli client.APIClient, promNetwork string, f models.ExporterFinder) SwarmBackend {
	b := docker.NewDockerBackend(cli, promNetwork, f)
	return SwarmBackend{&b, cli, promNetwork, f}
}

func (b SwarmBackend) GetPromStaticConfig(ctx context.Context) (*models.StaticConfig, error) {
	endpoints, err := b.listPromNetworkEndpoints(ctx)
	if err != nil {
		return nil, err
	}

	tasks, err := b.cli.TaskList(ctx, types.TaskListOptions{
		Filters: filters.NewArgs(filters.Arg("desired-state", "running")),
	})
	if err != nil {
		return nil, errors.WithStack(err)
	}

	staticConfig := models.NewStaticConfig()
	logger := log.GetLogger(ctx)
	// Use a service spec cache as multiple tasks might be linked
	// to the same service
	services := map[string]swarm.ServiceSpec{}

	for _, task := range tasks {
		if task.Spec.Runtime == swarm.RuntimePlugin ||
			task.Spec.Runtime == swarm.RuntimeNetworkAttachment {
			continue
		}

		serviceID := task.ServiceID
		service, err := b.findServiceSpec(ctx, serviceID, services)
		if err != nil {
			return nil, errors.WithStack(err)
		}

		// Check if there's an endpoint matching the task name to determine if
		// there's really an exporter connected on the promNetwork. If that's
		// not the case, prometheus won't be able to reach this exporter anyway
		// so no need to add it to its config.
		taskName := fmt.Sprintf("%s.%d.%s", service.Name, task.Slot, task.ID)
		if _, ok := endpoints[taskName]; !ok {
			continue
		}

		ip, _, err := net.ParseCIDR(endpoints[taskName])
		if err != nil {
			logger.Error(err)
			continue
		}

		t := models.TaskToExport{serviceID, taskName, service.Labels}

		for _, exporter := range b.resolveExporters(ctx, t) {
			target := fmt.Sprintf("%s:%s", ip.String(), exporter.Port)
			labels := map[string]string{
				"job": fmt.Sprintf("autoexporter-%s", exporter.ExporterType),
				"swarm_service_name": service.Name,
				"swarm_task_slot": strconv.Itoa(task.Slot),
				"swarm_task_id": task.ID,
			}

			staticConfig.AddTarget(target, labels)
			logger.WithFields(logrus.Fields{
				"labels": labels,
			}).Debugf("Add exporter %s for target %s", exporter.ExporterType, target)
		}
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

func (b SwarmBackend) findServiceSpec(ctx context.Context, serviceID string, cache map[string]swarm.ServiceSpec) (swarm.ServiceSpec, error) {
	if _, ok := cache[serviceID]; ok {
		return cache[serviceID], nil
	}

	service, _, err := b.cli.ServiceInspectWithRaw(ctx, serviceID, types.ServiceInspectOptions{})
	if err != nil {
		return swarm.ServiceSpec{}, errors.WithStack(err)
	}

	cache[serviceID] = service.Spec

	return cache[serviceID], nil
}

func (b SwarmBackend) resolveExporters(ctx context.Context, t models.TaskToExport) []models.Exporter {
	// We first check if an exporter name has been explicitly provided
	/* exporterType, err := readLabel(taskToExport, LABEL_EXPORTER)
	if err != nil {
		return []models.Exporter{}, err
	} */

	// @TODO: disable auto-resolve if label "autoexporter.auto=false" is present
	// @TODO: customize exporters with labels
	exporters := []models.Exporter{}
	matching, errors := b.finder.FindMatchingExporters(t)

	logger := log.GetLogger(ctx)
	logger.Debugf("Resolved %d exporters for %q.", len(matching), t.Name)

	for _, err := range errors {
		logger.Warning(err)
	}

	for pname, m := range matching {
		m.Name = getExporterName(pname, t.Name)
		exporters = append(exporters, m)
	}

	return exporters
}

func getExporterName(exporterType, tname string) string {
	return fmt.Sprintf("/exporter.%s.%s", exporterType, strings.TrimLeft(tname, "/"))
}
