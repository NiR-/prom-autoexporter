package models

import (
	"bufio"
	"bytes"
	"html/template"

	"github.com/docker/docker/api/types"
	"github.com/pkg/errors"
)

type Exporter struct {
	Name        string
	Image       string
	Cmd         []string
	PromNetwork string
	Exported    types.ContainerJSON
}

func NewExporter(name, image string, cmd []string, promNetwork string, exported types.ContainerJSON) Exporter {
	return Exporter{
		Name:        name,
		Image:       image,
		Cmd:         cmd,
		PromNetwork: promNetwork,
		Exported:    exported,
	}
}

func renderTpl(tplStr string, values interface{}) (string, error) {
	tpl, err := template.New("").Parse(tplStr)
	if err != nil {
		return "", errors.WithStack(err)
	}

	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)
	err = tpl.Execute(writer, values)
	if err != nil {
		return "", errors.WithStack(err)
	}

	writer.Flush()
	val := buf.String()

	return val, nil
}
