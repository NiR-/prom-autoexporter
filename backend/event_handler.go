package backend

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"html/template"
	"sync"
	"time"

	"github.com/NiR-/prom-autoexporter/log"
	"github.com/NiR-/prom-autoexporter/models"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// Thread-safe collection of context.CancelFunc
type cancelCollection struct {
	mutex sync.RWMutex
	funcs map[string]context.CancelFunc
}

func newCancelCollection() cancelCollection {
	return cancelCollection{
		mutex: sync.RWMutex{},
		funcs: make(map[string]context.CancelFunc, 0),
	}
}

func (c cancelCollection) cancel(k string) bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	var f func()
	var ok bool

	if f, ok = c.funcs[k]; ok {
		f()
		delete(c.funcs, k)
	}

	return ok
}

func (c cancelCollection) add(k string, ctx context.Context) context.Context {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	ctx, c.funcs[k] = context.WithCancel(ctx)
	return ctx
}

func (c cancelCollection) remove(k string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if _, ok := c.funcs[k]; ok {
		delete(c.funcs, k)
	}
}

func (b Backend) ListenEventsForExported(ctx context.Context, promNetwork string) {
	evtCh, errCh := b.cli.Events(ctx, types.EventsOptions{
		Since: time.Now().Format(time.RFC3339),
		Filters: filters.NewArgs(
			filters.Arg("type", events.ContainerEventType),
			filters.Arg("action", "start,die"),
		),
	})

	cancellables := newCancelCollection()

	for {
		select {
		case err := <-errCh:
			panic(err)
		case evt := <-evtCh:
			// Ignore exporters
			if _, ok := evt.Actor.Attributes[LABEL_EXPORTED_NAME]; ok {
				continue
			}

			// Ignore actions not filtered by docker daemon
			if evt.Action != "start" && evt.Action != "die" {
				continue
			}

			logger := log.GetLogger(ctx).WithFields(logrus.Fields{
				"event.type":   evt.Type,
				"event.action": evt.Action,
				"exported.cid": evt.Actor.ID,
			})
			ctx = log.WithLogger(ctx, logger)

			logger.Debug("New container event received.")

			if evt.Action == "start" {
				cancellables.add(evt.Actor.ID, ctx)
			} else if evt.Action == "die" {
				if cancelled := cancellables.cancel(evt.Actor.ID); cancelled {
					logger.Debug("Set up process was running and has been cancelled.")
				}
			}

			go func(ctx context.Context, evt events.Message) {
				handler := func() error {
					switch evt.Action {
					case "start":
						return b.handleContainerStart(ctx, evt.Actor.ID, promNetwork)
					case "die":
						return b.handleContainerStop(ctx, evt.Actor.ID)
					default:
						return fmt.Errorf("Action %q for %s %q is not supported.", evt.Action, evt.Type, evt.Actor.ID)
					}
				}

				logger := log.GetLogger(ctx)

				if err := retry(3, 5, handler); err != nil {
					logger.Errorf("%+v", err)
				}

				cancellables.remove(evt.Actor.ID)
			}(ctx, evt)
		}
	}
}

// @TODO: implement true back-off retry
func retry(times uint, interval time.Duration, f func() error) error {
	err := f()

	if err != nil {
		times = times - 1
	}
	if times != 0 && err != nil {
		time.Sleep(interval)

		err = retry(times, interval, f)
	}

	return err
}

func (b Backend) handleContainerStart(ctx context.Context, containerId, promNetwork string) error {
	logger := log.GetLogger(ctx)
	container, err := b.cli.ContainerInspect(ctx, containerId)

	if client.IsErrNotFound(err) {
		logger.Info("Contained died prematurly, exporter won't start.")
		return nil
	} else if err != nil {
		return errors.WithStack(err)
	}

	// We first check if an exporter name has been explicitly provided
	exporterType, err := readLabel(container, LABEL_EXPORTER_NAME)
	if err != nil {
		return err
	}

	// Then we try to find a predefined exporter matching container metadata
	if exporterType == "" {
		exporterType = models.FindMatchingExporter(container.Name)
	}

	logger = logger.WithFields(logrus.Fields{
		"exported.name": container.Name,
	})
	ctx = log.WithLogger(ctx, logger)

	// At this point, if no exporter has been found, we abort start up process
	if exporterType == "" {
		logger.Debug("No exporter name provided and no matching exporter found.")

		return nil
	}

	exporterName := getExporterName(container.Name)
	exporter, err := models.FromPredefinedExporter(exporterName, exporterType, container)
	if models.IsErrPredefinedExporterNotFound(err) {
		logger.Warnf("No predefined exporter named %q found.", exporterType)
		return nil
	} else if err != nil {
		return err
	}

	logger.WithFields(logrus.Fields{
		"exporter.image": exporter.Image,
	}).Info("Starting exporter...")

	exporter.PromNetwork = promNetwork
	b.RunExporter(ctx, exporter)

	return nil
}

func readLabel(container types.ContainerJSON, label string) (string, error) {
	return renderTpl(container.Config.Labels[label], container)
}

func renderTpl(tplStr string, values interface{}) (string, error) {
	tpl, err := template.New("").Parse(tplStr)
	if err != nil {
		return "", errors.WithStack(err)
	}

	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)
	err = tpl.Execute(writer, values)
	if err != nil {
		return "", errors.WithStack(err)
	}

	writer.Flush()
	val := buf.String()

	return val, nil
}

func (b Backend) handleContainerStop(ctx context.Context, containerId string) error {
	exporter, found, err := b.FindAssociatedExporter(ctx, containerId)

	if err != nil {
		return err
	} else if !found {
		return nil
	}

	return b.CleanupExporter(ctx, exporter.ID, false)
}
