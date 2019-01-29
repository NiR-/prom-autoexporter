package models

import (
	"bufio"
	"bytes"
	"html/template"
	"regexp"

	"github.com/pkg/errors"
)

type TaskToExport struct {
	ID     string
	Name   string
	Labels map[string]string
}

type Exporter struct {
	Name         string
	ExporterType string
	Image        string
	Cmd          []string
	EnvVars      []string
	Port         string
	ExportedTask TaskToExport
}

func NewExporter(name, exporterType, image string, cmd, envVars []string, port string, t TaskToExport) (Exporter, error) {
	cmd, err := renderTpls(cmd, t)
	if err != nil {
		return Exporter{}, err
	}

	envVars, err = renderTpls(envVars, t)
	if err != nil {
		return Exporter{}, err
	}

	return Exporter{
		Name:         name,
		ExporterType: exporterType,
		Image:        image,
		Cmd:          cmd,
		EnvVars:      envVars,
		Port:         port,
		ExportedTask: t,
	}, nil
}

// This function will render multiple templates with the same set of values each time
func renderTpls(tpls []string, values interface{}) ([]string, error) {
	res := []string{}

	for _, fragment := range tpls {
		val, err := renderTpl(fragment, values)
		if err != nil {
			return []string{}, err
		}

		res = append(res, val)
	}

	return res, nil
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

type ExporterFinder interface {
	FindMatchingExporters(TaskToExport) (map[string]Exporter, []error)
}

type PredefinedExporterFinder struct {
	predefinedExporters map[string]PredefinedExporter
}

func NewPredefinedExporterFinder() PredefinedExporterFinder {
	return PredefinedExporterFinder{defaultPredefinedExporters}
}

func (f PredefinedExporterFinder) FindMatchingExporters(t TaskToExport) (map[string]Exporter, []error) {
	exporters := map[string]Exporter{}
	errors := []error{}
	for name, p := range f.predefinedExporters {
		if !p.match(t) {
			continue
		}

		exporter, err := p.Exporter(t)
		if err != nil {
			errors = append(errors)
			continue
		}
		exporters[name] = exporter
	}
	return exporters, errors
}

type PredefinedExporter struct {
	name         string
	matcher      Matcher
	image        string
	cmd          []string
	envVars      []string
	exporterPort string
}

func (p PredefinedExporter) match(t TaskToExport) bool {
	return p.matcher(t)
}

func (p PredefinedExporter) Exporter(t TaskToExport) (Exporter, error) {
	return NewExporter("", p.name, p.image, p.cmd, p.envVars, p.exporterPort, t)
}

/* func PredefinedExporterExist(predefinedExporter string) bool {
	_, ok := predefinedExporters[predefinedExporter]
	return ok
} */

type Matcher func(TaskToExport) bool

func newRegexpNameMatcher(expr string) Matcher {
	return func(t TaskToExport) bool {
		return regexp.MustCompile(expr).FindStringIndex(t.Name) != nil
	}
}

func newBoolMatcher(val bool) Matcher {
	return func(TaskToExport) bool {
		return val
	}
}

var (
	defaultPredefinedExporters = map[string]PredefinedExporter{
		"redis": PredefinedExporter{
			name:    "redis",
			matcher: newRegexpNameMatcher("redis"),
			image:   "oliver006/redis_exporter:v0.25.0",
			cmd: []string{
				"-redis.addr=redis://localhost:6379",
				"-redis.alias={{ index .Labels \"com.docker.swarm.service.name\" }}",
				"-namespace={{ index .Labels \"com.docker.swarm.service.name\" }}",
			},
			envVars:      []string{},
			exporterPort: "9121",
		},
		"php": PredefinedExporter{
			name:    "php",
			matcher: newRegexpNameMatcher("php"),
			image:   "bakins/php-fpm-exporter:v0.5.0",
			cmd: []string{
				"--addr", ":8080",
				"--fastcgi", "tcp://localhost:9000/_fpm_status",
			},
			envVars:      []string{},
			exporterPort: "8080",
		},
		"elasticsearch": PredefinedExporter{
			name:    "elasticsearch",
			matcher: newRegexpNameMatcher("elasticsearch"),
			image:   "justwatch/elasticsearch_exporter:1.0.4rc1",
			cmd: []string{
				"-es.uri=http://localhost:9200",
				"-es.all=false",
			},
			envVars:      []string{},
			exporterPort: "9108",
		},
		"fluentd": PredefinedExporter{
			name:    "fluentd",
			matcher: newRegexpNameMatcher("fluentd?"),
			image:   "bitnami/fluentd-exporter:0.2.0",
			cmd: []string{
				"-scrape_uri", "http://localhost:24220/api/plugins.json",
			},
			envVars:      []string{},
			exporterPort: "9309",
		},
		"nginx": PredefinedExporter{
			name:    "nginx",
			matcher: newRegexpNameMatcher("nginx"),
			image:   "nginx/nginx-prometheus-exporter:0.2.0",
			cmd: []string{
				"-nginx.scrape-uri", "http://localhost/_status",
			},
			envVars:      []string{},
			exporterPort: "9113",
		},
		// @TODO: enable blackbox exporter
		/* "blackbox": PredefinedExporter{
			matcher: NewBoolMatcher(false),
			image:   "prom/blackbox-exporter:v0.13.0",
			cmd:     []string{},
		}, */
	}
)
