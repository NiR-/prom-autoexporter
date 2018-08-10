package models

import (
	"bufio"
	"bytes"
	"fmt"
	"html/template"

	"github.com/docker/docker/api/types"
	"github.com/pkg/errors"
)

type Exporter struct {
	Name        string
	Image       string
	Cmd         string
	PromNetwork string
	Exported    types.ContainerJSON
}

func NewExporter(name, image, cmd, promNetwrk string, exported types.ContainerJSON) Exporter {
	return Exporter{
		Name:        name,
		Image:       image,
		Cmd:         cmd,
		PromNetwork: promNetwrk,
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

type errPredefinedExporterNotFound struct {
	name string
}

func (e errPredefinedExporterNotFound) Error() string {
	return fmt.Sprintf("no predefined exporter named %q found", e.name)
}

func IsErrPredefinedExporterNotFound(e error) bool {
	_, ok := e.(errPredefinedExporterNotFound)
	return ok
}

func FromPredefinedExporter(predefinedExporter, promNetwrk string, exported types.ContainerJSON) (Exporter, error) {
	p, ok := predefinedExporters[predefinedExporter]
	if !ok {
		return Exporter{}, errPredefinedExporterNotFound{predefinedExporter}
	}

	cmd, err := renderTpl(p.cmd, exported)
	if err != nil {
		return Exporter{}, err
	}

	return NewExporter(predefinedExporter, p.image, cmd, promNetwrk, exported), nil
}
