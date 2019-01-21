package cmd

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/NiR-/prom-autoexporter/backend"
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

	b := backend.NewDockerBackend(cli, promNetwork, models.NewPredefinedExporterFinder())
	cancellables := newCancellableCollection()
	logger := log.GetLogger(ctx)

	logger.Info("Removing stale exporters...")
	if err = b.CleanupExporters(ctx, forceRecreate); err != nil {
		logger.Error("Cleanup failed: ", err)
	}

	logger.Info("Start listening for new backend events...")
	taskEvtCh := make(chan models.TaskEvent)
	go b.ListenForTasksToExport(ctx, taskEvtCh)

	logger.Info("Starting missing exporters...")
	missingExporters, err := b.FindMissingExporters(ctx)
	if err != nil {
		logrus.Errorf("%+v", err)
	}

	for _, exporter := range missingExporters {
		cancellableID := fmt.Sprintf("%s.%s", exporter.ExportedTask.Name, exporter.ExporterType)
		ctx := cancellables.add(cancellableID, ctx)
		handler := startHandlerFactory(b, exporter)

		logger := logger.WithFields(logrus.Fields{
			"operation":   "start",
			"exporter":    log.FormatExporterField(exporter),
			"task":        log.FormatTaskField(exporter.ExportedTask),
			"retry_count": 0,
		})
		ctx = log.WithLogger(ctx, logger)

		go retry(cancellables, ctx, 3, 30*time.Second, handler)
	}

	// Start ingesting backend events
	for evt := range taskEvtCh {
		for _, exporter := range evt.Exporters {
			cancellableID := fmt.Sprintf("%s.%s", evt.Task.Name, exporter.ExporterType)
			if cancelled := cancellables.cancel(cancellableID); cancelled {
				logger.Debugf("A previous start-up/clean-up operation for %q has been cancelled.", cancellableID)
			}

			var handler func(context.Context) error
			var op string
			ctx := cancellables.add(cancellableID, ctx)

			if evt.Type == models.TaskStarted {
				handler = startHandlerFactory(b, exporter)
				op = "start"
			} else {
				handler = stopHandlerFactory(b, exporter)
				op = "stop"
			}

			logger := logger.WithFields(logrus.Fields{
				"operation":   op,
				"exporter":    log.FormatExporterField(exporter),
				"task":        log.FormatTaskField(exporter.ExportedTask),
				"retry_count": 0,
			})
			ctx = log.WithLogger(ctx, logger)

			go retry(cancellables, ctx, 3, 30*time.Second, handler)
		}
	}

	// @TODO: setup signal handler to properly stop listening
	// @TODO: refactor log fields everywhere
}

// This function returns a closure to start an exporter. It's used as an argument
// to retry().
func startHandlerFactory(b backend.Backend, exporter models.Exporter) func(context.Context) error {
	return func(ctx context.Context) error {
		err := b.RunExporter(ctx, exporter)

		// We need to cleanup resources created previously, in order to retry a failed startup
		if err != nil {
			cleanErr := b.CleanupExporter(ctx, exporter.Name, true)
			if cleanErr != nil {
				logger := log.GetLogger(ctx)
				logger.Error("Clean-up of failed exporter start-up failed too: ", cleanErr.Error())
			}
		}

		return err
	}
}

// This function returns a closure to cleanup an exporter. It's used as an argument
// to retry().
func stopHandlerFactory(b backend.Backend, exporter models.Exporter) func(context.Context) error {
	return func(ctx context.Context) error {
		return b.CleanupExporter(ctx, exporter.Name, false)
	}
}

// cancellableCollection stores a thread-safe set of context cancellation functions.
// These functions are called to cancel running start/stop operation whereas the
// exported task dies/restarts. Because concurrent routines need to add/remove
// cancel functions to the set, a global mutex managed by add(), cancel() and
// remove() methods is used.
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
// This function runs and retries a provided closure for a given number of attempts
// while waiting for a given interval between each attempts. The closure takes
// the context as argument as it carries out the logger.
func retry(cancellables cancellableCollection, ctx context.Context, attempts uint, interval time.Duration, fn func(context.Context) error) error {
	err := fn(ctx)
	attempts--
	if err == nil {
		// When the exporter sucessfully starts or stops, the cancellable associated
		// with this operation is removed as there's nothing to cancel anymore
		cancellables.remove(ctx.Value(cancellableIDKey).(string))
		return nil
	}

	logger := log.GetLogger(ctx)
	if attempts == 0 {
		logger.Errorf("Last operation retry failed with error: %s", err.Error())
		return err
	}

	retryCount := logger.Data["retry_count"].(int)
	logger.Warningf("Operation failed with error: %s. Waiting %.0fs before retrying...",
		err.Error(),
		interval.Seconds())

	// Interrupt the retry as soon as the context is cancelled
	// or wait for the given interval and then retry
	timer := time.NewTimer(interval)
	select {
	case <-timer.C:
		retryCount++
		logger = logger.WithField("retry_count", retryCount)
		ctx = log.WithLogger(ctx, logger)

		err = retry(cancellables, ctx, attempts, interval, fn)
	case <-ctx.Done():
	}

	return err
}
