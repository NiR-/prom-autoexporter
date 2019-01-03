package models

import (
	"github.com/docker/docker/api/types"
)

type Exporter struct {
	Name        string
	Image       string
	Cmd         []string
	EnvVars     []string
	PromNetwork string
	Exported    types.ContainerJSON
}

func NewExporter(name, image string, cmd, envVars []string, exported types.ContainerJSON) Exporter {
	return Exporter{
		Name:        name,
		Image:       image,
		Cmd:         cmd,
		EnvVars:     envVars,
		PromNetwork: "",
		Exported:    exported,
	}
}
