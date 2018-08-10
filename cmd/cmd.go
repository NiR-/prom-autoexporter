package cmd

import (
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
			},
			Action: AutoExport,
		},
		/* {
			Name:        "autoconfigure",
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
				cli.UintFlag{
					Name:  "interval",
					Usage: "Interval in seconds between two reconfiguration",
					Value: 10,
				},
			},
			Action: AutoConfigure,
		}, */
	}
}
