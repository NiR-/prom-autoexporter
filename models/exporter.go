package models

import (
	"github.com/docker/docker/api/types"
)

type Exporter struct {
	Name           string
	PredefinedType string
	Image          string
	Cmd            []string
	EnvVars        []string
	Exported       types.ContainerJSON
}

func NewExporter(name, predefinedType, image string, cmd, envVars []string, exported types.ContainerJSON) Exporter {
	return Exporter{
		Name:           name,
		PredefinedType: predefinedType,
		Image:          image,
		Cmd:            cmd,
		EnvVars:        envVars,
		Exported:       exported,
	}
}
