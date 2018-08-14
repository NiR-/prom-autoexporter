package cmd

import (
	"time"

	cli "gopkg.in/urfave/cli.v1"
)

func BuildCommands() []cli.Command {
	return []cli.Command{
		{
			Name:        "autoexport",
			Description: "start daemon in autoexport mode: react to local docker events to start exporters",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "level",
					Usage: "Set the level of the logger",
				},
				cli.StringFlag{
					Name:  "network",
					Usage: "Network used to automatically connect Prometheus and exporters",
				},
				cli.BoolFlag{
					Name:  "force-recreate",
					Usage: "Cleanup all exporters created by prom-autoexporter and recreate them at start up",
				},
			},
			Action: AutoExport,
		},
		{
			Name:        "autoconfig",
			Description: "start daemon in autoconfigure mode: react to docker events to reconfigure Prometheus",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "level",
					Usage: "Set the level of the logger",
				},
				cli.StringFlag{
					Name:  "network",
					Usage: "Network used to interconnect exported containers and Prometheus",
				},
				cli.StringFlag{
					Name:  "filepath",
					Usage: "Path of the generated SD file",
				},
				cli.DurationFlag{
					Name:  "interval",
					Usage: "Interval in seconds between two reconfiguration",
					Value: time.Duration(10 * time.Second),
				},
			},
			Action: AutoConfig,
		},
		{
			Name:        "cleanup",
			Description: "clean up all exporters created",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "level",
					Usage: "Set the level of the logger",
				},
			},
			Action: Cleanup,
		},
	}
}
