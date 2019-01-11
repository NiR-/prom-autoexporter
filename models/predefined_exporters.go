package models

import (
	"fmt"
	"regexp"
	"bufio"
	"bytes"
	"html/template"

	"github.com/docker/docker/api/types"
	"github.com/pkg/errors"
)

type predefinedExporter struct {
	matcher      exporterMatcher
	image        string
	cmd          []string
	envVars      []string
	exporterPort string
}

type exporterMatcher interface {
	match(containerName string) bool
}

type regexpMatcher struct {
	regexp *regexp.Regexp
}

func newRegexpMatcher(exp string) regexpMatcher {
	return regexpMatcher{
		regexp: regexp.MustCompile(exp),
	}
}

func (m regexpMatcher) match(containerName string) bool {
	return m.regexp.FindStringIndex(containerName) != nil
}

type boolMatcher struct {
	value bool
}

func newBoolMatcher(val bool) boolMatcher {
	return boolMatcher{val}
}

func (m boolMatcher) match(containerName string) bool {
	return m.value
}

func FindMatchingExporter(name string) string {
	for exporterName, predefinedExporter := range predefinedExporters {
		if predefinedExporter.matcher.match(name) {
			return exporterName
		}
	}

	return ""
}

type errPredefinedExporterNotFound struct {
	name string
}

func (e errPredefinedExporterNotFound) Error() string {
	return fmt.Sprintf("no predefined exporter named %q found", e.name)
}

func newErrPredefinedExporterNotFound(exporterName string) error {
	return errPredefinedExporterNotFound{exporterName}
}

func IsErrPredefinedExporterNotFound(e error) bool {
	_, ok := e.(errPredefinedExporterNotFound)
	return ok
}

func FromPredefinedExporter(predefinedExporter string, exported types.ContainerJSON) (Exporter, error) {
	p, ok := predefinedExporters[predefinedExporter]
	if !ok {
		return Exporter{}, newErrPredefinedExporterNotFound(predefinedExporter)
	}

	cmd, err := renderSliceOfTpls(p.cmd, exported)
	if err != nil {
		return Exporter{}, err
	}

	envVars, err := renderSliceOfTpls(p.envVars, exported)
	if err != nil {
		return Exporter{}, err
	}

	return NewExporter(predefinedExporter, p.image, cmd, envVars, exported), nil
}

func renderSliceOfTpls(tpls []string, values interface{}) ([]string, error) {
	res := make([]string, len(tpls))

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

func PredefinedExporterExist(predefinedExporter string) bool {
	_, ok := predefinedExporters[predefinedExporter]
	return ok
}

func GetExporterPort(predefinedExporter string) (string, error) {
	if _, ok := predefinedExporters[predefinedExporter]; !ok {
		return "", newErrPredefinedExporterNotFound(predefinedExporter)
	}

	return predefinedExporters[predefinedExporter].exporterPort, nil
}

var (
	predefinedExporters = map[string]predefinedExporter{
		"redis": predefinedExporter{
			matcher: newRegexpMatcher("redis"),
			image:   "oliver006/redis_exporter:v0.25.0",
			cmd: []string{
				"-redis.addr=redis://localhost:6379",
				"-redis.alias={{ index .Config.Labels \"com.docker.swarm.service.name\" }}",
				"-namespace={{ index .Config.Labels \"com.docker.swarm.service.name\" }}",
			},
			envVars: []string{},
			exporterPort: "9121",
		},
		"php": predefinedExporter{
			matcher: newRegexpMatcher("php"),
			image:   "bakins/php-fpm-exporter:v0.5.0",
			cmd: []string{
				"--addr", ":8080",
				"--fastcgi", "tcp://localhost:9000/_fpm_status",
			},
			envVars: []string{},
			exporterPort: "8080",
		},
		"elasticsearch": predefinedExporter{
			matcher: newRegexpMatcher("elasticsearch"),
			image:   "justwatch/elasticsearch_exporter:1.0.4rc1",
			cmd: []string{
				"-es.uri=http://localhost:9200",
				"-es.all=false",
			},
			envVars: []string{},
			exporterPort: "9108",
		},
		/* "blackbox": predefinedExporter{
			matcher: newBoolMatcher(false),
			image:   "prom/blackbox-exporter:v0.13.0",
			cmd:     []string{},
		}, */
		"fluentd": predefinedExporter{
			matcher: newRegexpMatcher("fluentd?"),
			image:   "bitnami/fluentd-exporter:0.2.0",
			cmd:     []string{
				"-scrape_uri", "http://localhost:24220/api/plugins.json",
			},
			envVars: []string{},
			exporterPort: "9309",
		},
		"nginx": predefinedExporter{
			matcher: newRegexpMatcher("nginx"),
			image:   "sophos/nginx-vts-exporter:v0.10.3",
			cmd:     []string{},
			envVars: []string{
				"METRICS_NS={{ index .Config.Labels \"com.docker.swarm.service.name\" }}",
			},
			exporterPort: "9913",
		},
	}
)
