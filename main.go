package main

import (
	"os"

	"github.com/NiR-/prom-autoexporter/cmd"
	"github.com/sirupsen/logrus"
	cli "gopkg.in/urfave/cli.v1"
)

func main() {
	app := cli.NewApp()
	app.Name = "prom-autoexporter"
	app.Version = "0.2.0"
	app.Commands = cmd.BuildCommands()

	logrus.Fatal(app.Run(os.Args))
}
