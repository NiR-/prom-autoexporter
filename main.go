package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"text/template"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	cli "gopkg.in/urfave/cli.v1"
)

type config struct {
	network string
}

const (
	LABEL_EXPORTED_ID   = "autoexporter.exported.id"
	LABEL_EXPORTED_NAME = "autoexporter.exported.name"
	LABEL_EXPORTER_NAME = "autoexporter.exporter"
)

func run(cfg config) error {
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return err
	}
	defer cli.Close()

	ctx := context.Background()

	if err := cleanup(ctx, cli); err != nil {
		logrus.Error(err)
	}

	if err := startMissingExporters(ctx, cli, &cfg); err != nil {
		logrus.Error(err)
	}

	evtCh, errCh := cli.Events(ctx, types.EventsOptions{
		Since: time.Now().Format(time.RFC3339),
	})

	logrus.Info("Start listening for new Docker events...")

	for {
		select {
		case err := <-errCh:
			// @TODO: should auto-reconnect instead
			panic(err)
		case evt := <-evtCh:
			go func(evt events.Message) {
				if err := handleEvent(ctx, cli, evt, &cfg); err != nil {
					logrus.Error(err)
				}
			}(evt)
		}
	}

	return nil
}

func cleanup(ctx context.Context, cli *client.Client) error {
	containers, err := cli.ContainerList(ctx, types.ContainerListOptions{
		Filters: filters.NewArgs(filters.KeyValuePair{
			Key:   "label",
			Value: LABEL_EXPORTED_ID,
		}),
	})

	if err != nil {
		return err
	}

	for _, container := range containers {
		go func(container types.Container) {
			if err := cleanupExporter(ctx, cli, container); err != nil {
				logrus.Error(err)
			}
		}(container)
	}

	return nil
}

func startMissingExporters(ctx context.Context, cli *client.Client, cfg *config) error {
	containers, err := cli.ContainerList(ctx, types.ContainerListOptions{})
	if err != nil {
		return err
	}

	containerNames := make(map[string]string, 0)
	for _, container := range containers {
		for _, name := range container.Names {
			containerNames[name] = name
		}
	}

	for _, container := range containers {
		// Ignore exporters
		if _, ok := container.Labels[LABEL_EXPORTED_NAME]; ok {
			continue
		}

		exported := container.Labels[LABEL_EXPORTED_NAME]
		exporter := fmt.Sprintf("%s-exporter", exported)
		if _, ok := containerNames[exporter]; ok {
			continue
		}

		err := handleContainerStart(ctx, cli, container.ID, cfg)
		if err != nil {
			return err
		}
	}

	return nil
}

func handleEvent(ctx context.Context, cli *client.Client, evt events.Message, cfg *config) error {
	if evt.Type == "container" && evt.Action == "start" {
		return handleContainerStart(ctx, cli, evt.Actor.ID, cfg)
	} else if evt.Type == "container" && evt.Action == "stop" {
		return handleContainerStop(ctx, cli, evt.Actor.ID)
	}

	return nil
}

func handleContainerStart(ctx context.Context, cli *client.Client, containerId string, cfg *config) error {
	container, err := cli.ContainerInspect(ctx, containerId)
	if err != nil {
		return err
	}

	exporterName, err := inferExporter(ctx, cli, cfg, container)
	if exporterName == "" {
		logrus.WithFields(logrus.Fields{
			"container.name":   container.Name,
			"container.labels": container.Config.Labels,
		}).Info("No exporter name provided and no matching exporter found.")

		return err
	}

	predefinedExporter, ok := predefinedExporters[exporterName]
	if !ok {
		return errors.Errorf("Exporter %q not found.", exporterName)
	}

	exporter, err := NewExporter(predefinedExporter, container)
	if err != nil {
		return err
	}

	logrus.WithFields(logrus.Fields{
		"exported.name":  exporter.Exported.Name,
		"exporter.image": exporter.Image,
		"exporer.cmd":    exporter.Cmd,
	}).Info("Starting exporter...")

	return runExporter(ctx, cli, exporter, cfg)
}

func inferExporter(ctx context.Context, cli *client.Client, cfg *config, container types.ContainerJSON) (string, error) {
	_, isExporter := container.Config.Labels[LABEL_EXPORTED_ID]
	if isExporter {
		return "", nil
	}

	exporterName, err := readLabel(container, LABEL_EXPORTER_NAME)
	if err != nil {
		return "", err
	}
	if exporterName != "" {
		return exporterName, nil
	}

	for name, predefinedExporter := range predefinedExporters {
		if predefinedExporter.Matcher.Match(container.Name) {
			return name, nil
		}
	}

	return "", nil
}

func readLabel(container types.ContainerJSON, label string) (string, error) {
	return tplToStr(container.Config.Labels[label], container)
}

func tplToStr(tplStr string, values interface{}) (string, error) {
	tpl, err := template.New("").Parse(tplStr)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)
	err = tpl.Execute(writer, values)
	if err != nil {
		return "", err
	}

	writer.Flush()
	val := buf.String()

	return val, nil
}

func handleContainerStop(ctx context.Context, cli *client.Client, containerId string) error {
	container, err := cli.ContainerInspect(ctx, containerId)
	if err != nil {
		return err
	}

	_, isExporter := container.Config.Labels[LABEL_EXPORTED_ID]
	if !isExporter {
		return nil
	}

	return stopExporter(ctx, cli, container.Name)
}

func main() {
	app := cli.NewApp()
	app.Name = "prom-autoexporter"
	app.Version = "0.1.0"

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "network",
			Usage: "Network used to automatically connect Prometheus and exporters",
		},
	}

	app.Action = func(c *cli.Context) error {
		return run(config{
			network: c.String("network"),
		})
	}

	logrus.Fatal(app.Run(os.Args))
}
