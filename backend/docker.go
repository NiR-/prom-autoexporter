package backend

import (
	"context"
	"fmt"
	"io/ioutil"
	"regexp"
	"strings"
	"time"

	"github.com/NiR-/prom-autoexporter/log"
	"github.com/NiR-/prom-autoexporter/models"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	LABEL_EXPORTER      = "autoexporter.exporter"
	LABEL_EXPORTED_ID   = "autoexporter.exported.id"
	LABEL_EXPORTED_NAME = "autoexporter.exported.name"

	stepPullImage = "pullImage"
	stepCreate    = "create"
	stepConnect   = "connect"
	stepStart     = "start"
	stepFinished  = "finished"
)

type DockerBackend struct {
	cli         client.APIClient
	promNetwork string
	finder      models.ExporterFinder
}

func NewDockerBackend(cli client.APIClient, promNetwork string, finder models.ExporterFinder) DockerBackend {
	return DockerBackend{cli, promNetwork, finder}
}

type process struct {
	exporter    models.Exporter
	step        string
	exporterCID string
}

func (b DockerBackend) RunExporter(ctx context.Context, exporter models.Exporter) error {
	var err error

	logger := log.GetLogger(ctx).WithFields(logrus.Fields{
		"exported.name":  exporter.ExportedTask.Name,
		"exporter.type":  exporter.ExporterType,
		"exporter.name":  exporter.Name,
		"exporter.image": exporter.Image,
	})

	ctx = log.WithLogger(ctx, logger)
	p := process{exporter, stepPullImage, ""}

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			logger = logger.WithField("step", p.step)
			if p.exporterCID != "" {
				logger = logger.WithField("exporter.cid", p.exporterCID)
			}

			ctx = log.WithLogger(ctx, logger)
			exporter := p.exporter

			// The startup process is decomposed into several steps executed serially,
			// in order to cancel the startup as soon as possible
			switch p.step {
			case stepPullImage:
				err = b.pullImage(ctx, exporter.Image)
				p.step = stepCreate
			case stepCreate:
				var cid string
				cid, err = b.createContainer(ctx, exporter)

				p.exporterCID = cid
				p.step = stepConnect
			case stepConnect:
				err = b.connectToNetwork(ctx, exporter.ExportedTask.ID)
				p.step = stepStart
			case stepStart:
				err = b.startContainer(ctx, exporter, p.exporterCID)
				p.step = stepFinished
			case stepFinished:
				logger.Info("Exporter %q started.", exporter.Name)
				return nil
			default:
				err = errors.New(fmt.Sprintf("undefined step %s", p.step))
			}

			if err != nil {
				return err
			}
		}
	}
}

func isErrConflict(err error) bool {
	ok, err := regexp.MatchString("The container name \"[^\"]+\" is already in use", err.Error())
	if err != nil {
		panic(err)
	}

	return ok
}

func (b DockerBackend) pullImage(ctx context.Context, image string) error {
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

func (b DockerBackend) createContainer(ctx context.Context, exporter models.Exporter) (string, error) {
	config := container.Config{
		User:  "1000",
		Cmd:   exporter.Cmd,
		Image: exporter.Image,
		Env:   exporter.EnvVars,
		Labels: map[string]string{
			LABEL_EXPORTED_ID:   exporter.ExportedTask.ID,
			LABEL_EXPORTED_NAME: exporter.ExportedTask.Name,
		},
	}
	hostConfig := container.HostConfig{
		NetworkMode: container.NetworkMode(fmt.Sprintf("container:%s", exporter.ExportedTask.ID)),
		RestartPolicy: container.RestartPolicy{
			Name:              "on-failure",
			MaximumRetryCount: 10,
		},
	}
	networkingConfig := network.NetworkingConfig{}

	container, err := b.cli.ContainerCreate(ctx, &config, &hostConfig, &networkingConfig, exporter.Name)
	if err != nil {
		return "", errors.WithStack(err)
	}

	logger := log.GetLogger(ctx)
	logger.Debug("Exporter container created.")

	if len(container.Warnings) > 0 {
		logger.WithFields(logrus.Fields{
			"warnings": container.Warnings,
		}).Warning("Docker emitted warnings during container create.")
	}

	return container.ID, nil
}

func (b DockerBackend) connectToNetwork(ctx context.Context, cid string) error {
	endpointSettings := network.EndpointSettings{}
	err := b.cli.NetworkConnect(ctx, b.promNetwork, cid, &endpointSettings)

	if err != nil && strings.Contains(err.Error(), "endpoint with name") {
		return nil
	} else if err != nil {
		return errors.WithStack(err)
	}

	logger := log.GetLogger(ctx)
	logger.Debug("Exporter connected to prometheus network.")

	return nil
}

func (b DockerBackend) startContainer(ctx context.Context, exporter models.Exporter, cid string) error {
	logger := log.GetLogger(ctx)
	logger.Debug("Starting exporter container.")

	err := b.cli.ContainerStart(ctx, cid, types.ContainerStartOptions{})
	if err != nil {
		return errors.WithStack(err)
	}

	return nil
}

func (b DockerBackend) FindMissingExporters(ctx context.Context) ([]models.Exporter, error) {
	containers, err := b.cli.ContainerList(ctx, types.ContainerListOptions{})
	if err != nil {
		return []models.Exporter{}, errors.WithStack(err)
	}

	containerNames := make(map[string]string, 0)
	for _, container := range containers {
		for _, name := range container.Names {
			containerNames[name] = name
		}
	}

	missing := make([]models.Exporter, 0)

	// Iterate over containers to find which one should have an associated
	// exporter running but does not
	for _, container := range containers {
		// Ignore exporters
		if _, ok := container.Labels[LABEL_EXPORTED_NAME]; ok {
			continue
		}

		t := models.TaskToExport{container.ID, container.Names[0], container.Labels}
		exporters := b.resolveExporters(ctx, t)

		for _, exporter := range exporters {
			if _, ok := containerNames[exporter.Name]; !ok {
				missing = append(missing, exporter)
			}
		}
	}

	return missing, nil
}

func (b DockerBackend) resolveExporters(ctx context.Context, t models.TaskToExport) []models.Exporter {
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

/* func readLabel(taskToExport models.TaskToExport, label string) (string, error) {
	if _, ok := taskToExport.Labels[label]; !ok {
		return "", nil
	}
	return renderTpl(taskToExport.Labels[label], taskToExport)
} */

func (b DockerBackend) CleanupExporters(ctx context.Context, force bool) error {
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

	for _, exporter := range exporters {
		err := b.stopExporter(ctx, exporter, force)

		if err == nil {
			continue
		}
		if !IsErrExportedTaskStillRunning(err) {
			return err
		}

		logger.Infof("Exporter %q can't be cleaned up, exported task still running.", exporter.Names[0])
	}

	return nil
}

func (b DockerBackend) CleanupExporter(ctx context.Context, exporterName string, force bool) error {
	c, err := b.cli.ContainerList(ctx, types.ContainerListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("name", exporterName),
		),
	})
	if err != nil {
		return err
	}
	if len(c) == 0 {
		return errors.New("exporter not found")
	}
	if len(c) > 1 {
		return errors.New("more than one container match the provided exporter name")
	}

	return b.stopExporter(ctx, c[0], force)
}

func (b DockerBackend) stopExporter(ctx context.Context, exporter types.Container, force bool) error {
	exporterCID := exporter.ID
	exportedCID := exporter.Labels[LABEL_EXPORTED_ID]

	exported, err := b.cli.ContainerInspect(ctx, exportedCID)
	// @TODO: check what happens if the exported container has already been removed
	if err != nil && !client.IsErrNotFound(err) {
		return errors.WithStack(err)
	} else if err == nil && exported.State.Running && !force {
		return newErrExportedTaskStillRunning(exporter.ID, exportedCID)
	}

	// @TODO: find a way to disconnect the exported container only when no more exporter is attached to it (check how many containers share the same net cgroup?)
	/* err = b.cli.NetworkDisconnect(ctx, b.promNetwork, exporterCID, force)
	if err != nil && !strings.Contains(err.Error(), "is not connected to the network") {
		return errors.WithStack(err)
	} */

	// @TODO: add a timeout?
	// @TODO: what happens if the exporter has already been stopped but not removed?
	err = b.cli.ContainerStop(ctx, exporterCID, nil)
	if err != nil {
		return errors.WithStack(err)
	}

	opts := types.ContainerRemoveOptions{Force: force}
	err = b.cli.ContainerRemove(ctx, exporterCID, opts)
	if err != nil {
		return errors.WithStack(err)
	}

	logger := log.GetLogger(ctx).WithFields(logrus.Fields{
		"exporter.cid":  exporterCID,
		"exporter.name": exporter.Names[0],
		"exported.id":   exportedCID,
		"exported.name": exporter.Labels[LABEL_EXPORTED_NAME],
	})
	logger.Info("Exporter container stopped and removed.")

	return nil
}

func getExporterName(exporterType, tname string) string {
	return fmt.Sprintf("/exporter.%s.%s", exporterType, strings.TrimLeft(tname, "/"))
}

func (b DockerBackend) ListenForTasksToExport(ctx context.Context, evtCh chan<- models.TaskEvent) {
	dockEvtCh, dockErrCh := b.cli.Events(ctx, types.EventsOptions{
		Since: time.Now().Format(time.RFC3339),
		Filters: filters.NewArgs(
			filters.Arg("type", events.ContainerEventType),
			filters.Arg("action", "start,die"),
		),
	})

	for {
		select {
		case <-ctx.Done():
			return
		case err := <-dockErrCh:
			panic(err)
		case evt := <-dockEvtCh:
			// Ignore exporters
			if _, ok := evt.Actor.Attributes[LABEL_EXPORTED_NAME]; ok {
				continue
			}

			// Ignore actions not filtered by docker daemon
			// @TODO: check no actions missing
			if evt.Action != "start" && evt.Action != "die" {
				continue
			}

			evtType := models.TaskStarted
			if evt.Action == "die" {
				evtType = models.TaskStopped
			}

			logger := log.GetLogger(ctx).WithFields(logrus.Fields{
				"event.type":   evt.Type,
				"event.action": evt.Action,
				"exported.cid": evt.Actor.ID,
			})
			ctx = log.WithLogger(ctx, logger)
			logger.Debug("New container event received.")

			taskId := evt.Actor.ID
			taskName := evt.Actor.Attributes["name"]
			labels := extractActorLabels(evt.Actor)

			t := models.TaskToExport{taskId, taskName, labels}
			exporters := b.resolveExporters(ctx, t)

			evtCh <- models.TaskEvent{t, evtType, exporters}
		}
	}
}

func extractActorLabels(actor events.Actor) map[string]string {
	labels := make(map[string]string)

	for k, v := range actor.Attributes {
		if k == "name" || k == "image" || k == "exitCode" {
			continue
		}
		labels[k] = v
	}

	return labels
}

func (b DockerBackend) GetPromStaticConfig(context.Context) (*models.StaticConfig, error) {
	panic(errors.New("not implemented yet."))
}
