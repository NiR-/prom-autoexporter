package cmd

import (
	"context"
	"sync"
	"fmt"
	"time"

	"github.com/NiR-/prom-autoexporter/backend"
	"github.com/NiR-/prom-autoexporter/backend/docker"
	"github.com/NiR-/prom-autoexporter/log"
	"github.com/NiR-/prom-autoexporter/models"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	cli "gopkg.in/urfave/cli.v1"
)

const (
	cancellableIDKey = "cancellableID"
)

func AutoExport(c *cli.Context) {
	promNetwork := c.String("network")
	forceRecreate := c.Bool("force-recreate")

	ctx := log.WithDefaultLogger(context.Background())
	log.ConfigureDefaultLogger(c.String("level"))

	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		logrus.Errorf("%+v", errors.WithStack(err))
		return
	}

	defer cli.Close()
	cli.NegotiateAPIVersion(ctx)

	b := docker.NewDockerBackend(cli, promNetwork, models.NewPredefinedExporterFinder())
	cancellables := newCancellableCollection()
	logger := log.GetLogger(ctx)

	logger.Info("Removing stale exporters...")
	if err := b.CleanupExporters(ctx, forceRecreate); err != nil {
		logrus.Errorf("%+v", err)
	}

	logger.Info("Start listening for new backend events...")
	taskEvtCh := make(chan models.TaskEvent)
	go b.ListenForTasksToExport(ctx, evtCh)

	logger.Info("Starting missing exporters...")
	missingExporters, err := b.FindMissingExporters(ctx)
	if err != nil {
		logrus.Errorf("%+v", err)
	}

	for _, exporter := range missingExporters {
		cancellableID := fmt.Sprintf("%s.%s", exporter.ExportedTask.Name, exporter.ExporterType)
		ctx = cancellables.add(cancellableID, ctx)
		handler := startHandlerFactory(cancellables, ctx, b, exporter)

		go retry(ctx, 3, 30 * time.Second, handler)
	}

	// Start ingesting backend events
	for evt := range taskEvtCh {
		for _, exporter := range evt.Exporters {
			cancellableID := fmt.Sprintf("%s.%s", evt.Task.Name, exporter.ExporterType)
			if cancelled := cancellables.cancel(cancellableID); cancelled {
				logger.Debugf("A previous start/stop process with ID %q has been cancelled.", cancellableID)
			}

			var handler func() error
			ctx = cancellables.add(cancellableID, ctx)

			if evt.Type == models.TaskStarted {
				handler = startHandlerFactory(cancellables, ctx, b, exporter)
			} else {
				handler = stopHandlerFactory(ctx, b, exporter)
			}

			go retry(ctx, 3, 30 * time.Second, handler)
		}
	}

	// @TODO: setup signal handler to properly stop listening
}

// This function returns an anonymous function to start exporters.
// This is especially useful for easily retrying the handler if it fails
func startHandlerFactory(cancellables cancellableCollection, ctx context.Context, b backend.Backend, exporter models.Exporter) (func() error) {
	return func() error {
		err := b.RunExporter(ctx, exporter)
		// When the exporter has sucessfully started, the cancellable associated
		if err == nil {
			cancellables.remove(ctx.Value(cancellableIDKey).(string))
		}
		return err
	}
}

func stopHandlerFactory(ctx context.Context, b backend.Backend, exporter models.Exporter) func() error {
	return func() error {
		return b.CleanupExporter(ctx, exporter.Name, false)
	}
}

// It's used to store a thread-safe set of context cancellation functions called
// when some exporters are being created whereas the exported container dies.
type cancellableCollection struct {
	mutex sync.RWMutex
	funcs map[string]context.CancelFunc
}

func newCancellableCollection() cancellableCollection {
	return cancellableCollection{
		mutex: sync.RWMutex{},
		funcs: make(map[string]context.CancelFunc, 0),
	}
}

func (c cancellableCollection) cancel(id string) bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	cancelfn, ok := c.funcs[id]
	if ok {
		cancelfn()
		delete(c.funcs, id)
	}

	return ok
}

func (c cancellableCollection) add(id string, ctx context.Context) context.Context {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	ctx, c.funcs[id] = context.WithCancel(ctx)
	ctx = context.WithValue(ctx, cancellableIDKey, id)

	return ctx
}

func (c cancellableCollection) remove(id string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if _, ok := c.funcs[id]; ok {
		delete(c.funcs, id)
	}
}

// @TODO: implement true back-off retry
func retry(ctx context.Context, times uint, interval time.Duration, fn func() error) error {
	err := fn()
	if err != nil {
		times--
	}
	if (times == 0 && err != nil) || err == nil {
		return err
	}

	// Interrupt the retry as soon as the context got cancelled
	// or wait for the given interval and then retry
	timer := time.NewTimer(interval)
	select {
	case <-timer.C:
		err = retry(ctx, times, interval, fn)
	case <-ctx.Done():
	}

	return err
}
