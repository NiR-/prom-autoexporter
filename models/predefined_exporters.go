package models

import (
	"regexp"
)

type predefinedExporter struct {
	matcher exporterMatcher
	image   string
	cmd     string
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

var (
	predefinedExporters = map[string]predefinedExporter{
		"redis": predefinedExporter{
			matcher: newRegexpMatcher("redis"),
			image:   "oliver006/redis_exporter:v0.20.2",
			cmd: "redis_exporter " +
				"-redis.addr=redis://localhost:6379 " +
				"-redis.alias={{ index .Config.Labels \"com.docker.swarm.service.name\" }} " +
				"-namespace={{ index .Config.Labels \"com.docker.swarm.service.name\" }}",
		},
		"php": predefinedExporter{
			matcher: newRegexpMatcher("php"),
			image:   "bakins/php-fpm-exporter:v0.4.1",
			cmd: "php-fpm-exporter " +
				"--fastcgi http://localhost:9000/_status",
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
