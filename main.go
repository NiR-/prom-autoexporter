package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"text/template"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/strslice"
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"
	cli "gopkg.in/urfave/cli.v1"
)

type Exporter struct {
	Image    string
	Cmd      string
	Exported types.ContainerJSON
}

type config struct {
	autoConnect bool
	network     string
}

type PredefinedExporter struct {
	Regexp string
	Image  string
	Cmd    string
}

const (
	LABEL_EXPORTED_ID   = "autoexporter.exported.id"
	LABEL_EXPORTED_NAME = "autoexporter.exported.name"
	LABEL_EXPORTER_NAME = "autoexporter.exporter"
)

var (
	errNodeNotFound = errors.New("node not found")

	predefinedExporters = map[string]PredefinedExporter{
		"redis": PredefinedExporter{
			Regexp: "redis",
			Image:  "oliver006/redis_exporter:v0.20.2",
			Cmd: `-redis.addr=redis://localhost:6379
						-redis.alias=api_redis
						-namespace=api_redis`, //  ,{{.Config.Labels .Data "com.docker.swarm.task.name"}}
		},
		"memcached": PredefinedExporter{
			Regexp: "memcached?",
			Image:  "quay.io/prometheus/memcached-exporter:v0.4.1",
			Cmd:    ``,
		},
		"fluentd": PredefinedExporter{
			Regexp: "fluentd?",
			Image:  "bitnami/fluentd-exporter:0.2.0",
			Cmd:    ``,
		},
		"nginx": PredefinedExporter{
			Regexp: "nginx",
			Image:  "sophos/nginx-vts-exporter:v0.10.3",
			Cmd:    ``,
		},
		"blackbox": PredefinedExporter{
			Regexp: "", // This exporter is never started automatically
			Image:  "prom/blackbox-exporter:v0.12.0",
			Cmd:    ``,
		},
		"php": PredefinedExporter{
			Regexp: "php",
			Image:  "bakins/php-fpm-exporter:v0.4.1",
			Cmd:    `--fastcgi http://localhost:9000/_status`,
		},
	}
)

func run(cfg config) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithVersion("1.37"))
	if err != nil {
		return err
	}
	defer cli.Close()

	ctx := context.Background()

	hostname, err := os.Hostname()
	if err != nil {
		return err
	}

	// @TODO: re-enable
	/* if err := updateAutoConnectMode(ctx, cli, &cfg, hostname); err != nil {
		return err
	} */
	autoConnectAll(ctx, cli)

	// @TODO: increase duration
	go every(3*time.Second, func() {
		if err := updateAutoConnectMode(ctx, cli, &cfg, hostname); err != nil {
			logrus.Error(err)
		}
	})

	if err := cleanup(ctx, cli); err != nil {
		return err
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

func every(duration time.Duration, fn func()) {
	for _ = range time.Tick(duration) {
		fn()
	}
}

func updateAutoConnectMode(ctx context.Context, cli *client.Client, cfg *config, hostname string) error {
	swarmMember, err := isSwarmMember(ctx, cli)
	if err != nil {
		return err
	}

	if !swarmMember {
		if cfg.autoConnect {
			cfg.autoConnect = false
			logrus.Info("Auto connect mode has been disabled because this node is not a member of a swarm cluster.")
		}

		return nil
	}

	isLeader, err := isClusterLeader(ctx, cli, hostname)
	if err != nil {
		return err
	}

	if isLeader && !cfg.autoConnect {
		cfg.autoConnect = true
		logrus.Info("Auto connect mode has been enabled because this daemon is now running on the cluster leader.")

		go autoConnectAll(ctx, cli)
	} else if !isLeader && cfg.autoConnect {
		cfg.autoConnect = false
		logrus.Info("Auto connect mode has been disabled because this daemon is not running on the cluster leader.")
	}

	return nil
}

func isSwarmMember(ctx context.Context, cli *client.Client) (bool, error) {
	_, err := cli.SwarmInspect(ctx)

	if err != nil &&
		!client.IsErrConnectionFailed(err) &&
		!client.IsErrNotImplemented(err) &&
		!client.IsErrUnauthorized(err) {
		// If the error is of unknown type, we consider this node as
		// outside of any swarm cluster
		return false, nil
	} else if err != nil {
		return false, err
	}

	return true, nil
}

func isClusterLeader(ctx context.Context, cli *client.Client, hostname string) (bool, error) {
	nodes, err := cli.NodeList(ctx, types.NodeListOptions{})

	if err != nil &&
		!client.IsErrConnectionFailed(err) &&
		!client.IsErrNotImplemented(err) &&
		!client.IsErrUnauthorized(err) {
		// If the error is of unknown type, we consider the node as a worker
		return false, nil
	} else if err != nil {
		return false, err
	}

	for _, node := range nodes {
		if node.Description.Hostname != hostname {
			continue
		}

		if node.ManagerStatus.Leader {
			return true, nil
		} else {
			return false, nil
		}
	}

	return false, errNodeNotFound
}

// @TODO
func autoConnectAll(ctx context.Context, cli *client.Client) error {
	services, err := cli.ServiceList(ctx, types.ServiceListOptions{})
	if err != nil {
		return err
	}

	for service := range services {
		_, ok := service.Spec.TaskTemplate.ContainerSpec.Labels[LABEL_EXPORTER_NAME]
		if !ok {
			continue
		}

		tasks := cli.TaskList(ctx, types.TaskListOptions{
			Filters: filters.NewArgs(filters.KeyValuePair{
				Key:   "service",
				Value: service.Spec.Name,
			}),
		})
	}

	logrus.Infof("Services found: %d", len(services))

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

func cleanupExporter(ctx context.Context, cli *client.Client, exporter types.Container) error {
	_, err := cli.ContainerInspect(ctx, exporter.Labels[LABEL_EXPORTED_NAME])

	// If the exported service is still alive, we stop cleanup process
	if err == nil {
		return nil
	}
	if !client.IsErrNotFound(err) {
		return err
	}

	stopExporter(ctx, cli, exporter.Labels[LABEL_EXPORTED_NAME])

	return nil
}

func handleEvent(ctx context.Context, cli *client.Client, evt events.Message, cfg *config) error {
	if evt.Type == "container" && evt.Action == "start" {
		return handleContainerStart(ctx, cli, evt, cfg)
	} else if evt.Type == "container" && evt.Action == "stop" {
		return handleContainerStop(ctx, cli, evt)
	}

	return nil
}

func handleContainerStart(ctx context.Context, cli *client.Client, evt events.Message, cfg *config) error {
	container, err := cli.ContainerInspect(ctx, evt.Actor.ID)
	if err != nil {
		return err
	}

	_, isExporter := container.Config.Labels[LABEL_EXPORTED_ID]
	if isExporter {
		return nil
	}

	exporterName, err := readLabel(container, LABEL_EXPORTER_NAME)
	if err != nil {
		return err
	}

	if exporterName == "" {
		exporterName = detectExporter(container.Name)
	}

	if exporterName == "" {
		logrus.WithFields(logrus.Fields{
			"container.name":   container.Name,
			"container.labels": container.Config.Labels,
		}).Infof("No exporter name provided and no matching exporter found.")

		return nil
	}

	predefinedExporter, ok := predefinedExporters[exporterName]
	if !ok {
		logrus.Warningf("Exporter %q not found.", exporterName)
	}

	cmd, err := tplToStr(predefinedExporter.Cmd, container)
	if err != nil {
		return err
	}

	exporter := Exporter{
		Image:    predefinedExporter.Image,
		Cmd:      cmd,
		Exported: container,
	}

	logrus.WithFields(logrus.Fields{
		"exported.name":  exporter.Exported.Name,
		"exporter.image": exporter.Image,
		"exporer.cmd":    exporter.Cmd,
	}).Infof("Exported container started.")

	return runExporter(ctx, cli, exporter, cfg)
}

func readLabel(container types.ContainerJSON, label string) (string, error) {
	labelVal := container.Config.Labels[label]

	return tplToStr(labelVal, container)
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

func detectExporter(containerName string) string {
	for exporterName, predefinedExporter := range predefinedExporters {
		// If a predefined exporter does not have any regexp, it won't be
		// started automatically
		if predefinedExporter.Regexp == "" {
			continue
		}

		re := regexp.MustCompile(predefinedExporter.Regexp)
		if re.FindStringIndex(containerName) != nil {
			return exporterName
		}
	}

	return ""
}

func runExporter(ctx context.Context, cli *client.Client, exporter Exporter, cfg *config) error {
	config := container.Config{
		User:  "1000",
		Cmd:   strslice.StrSlice{exporter.Cmd},
		Image: exporter.Image,
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

	exporterName := fmt.Sprintf("%s-exporter", exporter.Exported.Name)
	container, err := cli.ContainerCreate(ctx, &config, &hostConfig, &networkingConfig, exporterName)
	if err != nil {
		return err
	}

	logrus.WithFields(logrus.Fields{
		"exporter.id":   container.ID,
		"exported.name": exporter.Exported.Name,
		"image":         exporter.Image,
		"cmd":           exporter.Cmd,
	}).Infof("Exporter container created.", exporter.Exported.Name)

	if len(container.Warnings) > 0 {
		logrus.WithFields(logrus.Fields{
			"warnings": container.Warnings,
		}).Warningf("Docker emitted warnings during container create.")
	}

	endpointSettings := network.EndpointSettings{}
	err = cli.NetworkConnect(ctx, cfg.network, exporter.Exported.ID, &endpointSettings)
	if err != nil {
		return err
	}

	logrus.WithFields(logrus.Fields{
		"exporter.id":   container.ID,
		"exporter.name": exporter,
		"exported.name": exporter.Exported.Name,
		"network":       cfg.network,
	}).Infof("Exporter connected to prometheus network.", exporter.Exported.Name)

	err = cli.ContainerStart(ctx, container.ID, types.ContainerStartOptions{})
	if err != nil {
		return err
	}

	logrus.WithFields(logrus.Fields{
		"exporter.id":   container.ID,
		"exporter.name": exporter,
		"image":         exporter.Image,
		"cmd":           exporter.Cmd,
	}).Infof("Exporter container started.", exporter.Exported.Name)

	return nil
}

func handleContainerStop(ctx context.Context, cli *client.Client, evt events.Message) error {
	container, err := cli.ContainerInspect(ctx, evt.Actor.ID)
	if err != nil {
		return err
	}

	_, isExporter := container.Config.Labels[LABEL_EXPORTED_ID]
	if !isExporter {
		return nil
	}

	return stopExporter(ctx, cli, container.Name)
}

func stopExporter(ctx context.Context, cli *client.Client, exported string) error {
	exporter := fmt.Sprintf("%s-exporter", exported)
	err := cli.ContainerStop(ctx, exporter, nil)
	if err != nil {
		return err
	}

	err = cli.ContainerRemove(ctx, exporter, types.ContainerRemoveOptions{
		Force: true,
	})
	if err != nil {
		return err
	}

	logrus.WithFields(logrus.Fields{
		"exporter.name": exporter,
		"exported.name": exported,
	}).Infof("Exporter container stopped.", exported)

	return nil
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
		cfg := config{
			autoConnect: false,
			network:     c.String("network"),
		}

		if cfg.network != "" {
			cfg.autoConnect = true
		}

		return run(cfg)
	}

	err := app.Run(os.Args)
	if err != nil {
		logrus.Fatal(err)
		os.Exit(1)
	}
}
