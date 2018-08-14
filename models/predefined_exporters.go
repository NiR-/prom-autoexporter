package models

import (
	"fmt"
	"regexp"

	"github.com/docker/docker/api/types"
)

type predefinedExporter struct {
	matcher      exporterMatcher
	image        string
	cmd          []string
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

func FromPredefinedExporter(predefinedExporter, promNetwork string, exported types.ContainerJSON) (Exporter, error) {
	p, ok := predefinedExporters[predefinedExporter]
	if !ok {
		return Exporter{}, newErrPredefinedExporterNotFound(predefinedExporter)
	}

	cmd := make([]string, 0)

	for _, fragment := range p.cmd {
		val, err := renderTpl(fragment, exported)
		if err != nil {
			return Exporter{}, err
		}

		cmd = append(cmd, val)
	}

	return NewExporter(predefinedExporter, p.image, cmd, promNetwork, exported), nil
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
			image:   "oliver006/redis_exporter:v0.20.2",
			cmd: []string{
				"-redis.addr=redis://localhost:6379",
				"-redis.alias={{ index .Config.Labels \"com.docker.swarm.service.name\" }}",
				"-namespace={{ index .Config.Labels \"com.docker.swarm.service.name\" }}",
			},
			exporterPort: "9121",
		},
		"php": predefinedExporter{
			matcher: newRegexpMatcher("php"),
			image:   "bakins/php-fpm-exporter:v0.4.1",
			cmd: []string{
				"--addr", ":8080",
				"--fastcgi", "tcp://localhost:9000/_fpm_status",
			},
			exporterPort: "8080",
		},
		/* "blackbox": predefinedExporter{
			matcher: newBoolMatcher(false),
			image:   "prom/blackbox-exporter:v0.12.0",
			cmd:     ``,
		},
		"memcached": predefinedExporter{
			matcher: newRegexpMatcher("memcached?"),
			image:   "quay.io/prometheus/memcached-exporter:v0.4.1",
			cmd:     ``,
		},
		"fluentd": predefinedExporter{
			matcher: newRegexpMatcher("fluentd?"),
			image:   "bitnami/fluentd-exporter:0.2.0",
			cmd:     ``,
		},
		"nginx": predefinedExporter{
			matcher: newRegexpMatcher("nginx"),
			image:   "sophos/nginx-vts-exporter:v0.10.3",
			cmd:     ``,
		}, */
	}
)
