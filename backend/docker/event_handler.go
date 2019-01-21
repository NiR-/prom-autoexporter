package docker

import (
	"context"
	"time"

	"github.com/NiR-/prom-autoexporter/log"
	"github.com/NiR-/prom-autoexporter/models"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/sirupsen/logrus"
)

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

			evtType := models.TaskStarted

			// Ignore actions not filtered by docker daemon
			// @TODO: check no actions missing
			if evt.Action != "start" && evt.Action != "die" {
				continue
			}
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

			evtCh <-models.TaskEvent{t, evtType, exporters}
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
