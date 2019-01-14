package cmd

import (
	"context"

	"github.com/NiR-/prom-autoexporter/backend"
	"github.com/NiR-/prom-autoexporter/log"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	cli "gopkg.in/urfave/cli.v1"
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

	b := backend.NewBackend(cli)
	cancellables := newCancellableCollection()

	logrus.Info("Removing stale exporters...")

	if err := b.CleanupAllExporters(ctx, forceRecreate); err != nil {
		logrus.Errorf("%+v", err)
	}

	logrus.Info("Starting missing exporters...")

	if err := b.StartMissingExporters(ctx, promNetwork); err != nil {
		logrus.Errorf("%+v", err)
	}

	logrus.Info("Start listening for new Docker events...")
	b.ListenEventsForExported(ctx, promNetwork)
}

// Thread-safe collection of context.CancelFunc
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

func (c cancellableCollection) cancel(k string) bool {
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

func (c cancellableCollection) add(k string, ctx context.Context) context.Context {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	ctx, c.funcs[k] = context.WithCancel(ctx)
	return ctx
}

func (c cancellableCollection) remove(k string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if _, ok := c.funcs[k]; ok {
		delete(c.funcs, k)
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
