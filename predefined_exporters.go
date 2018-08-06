package main

import "regexp"

type PredefinedExporter struct {
	Matcher ExporterMatcher
	Image   string
	Cmd     string
}

type ExporterMatcher interface {
	Match(containerName string) bool
}

type RegexpMatcher struct {
	regexp *regexp.Regexp
}

func NewRegexpMatcher(exp string) RegexpMatcher {
	return RegexpMatcher{
		regexp: regexp.MustCompile(exp),
	}
}

func (m RegexpMatcher) Match(containerName string) bool {
	return m.regexp.FindStringIndex(containerName) != nil
}

type BoolMatcher struct {
	value bool
}

func NewBoolMatcher(val bool) BoolMatcher {
	return BoolMatcher{val}
}

func (m BoolMatcher) Match(containerName string) bool {
	return m.value
}

var (
	predefinedExporters = map[string]PredefinedExporter{
		"redis": PredefinedExporter{
			Matcher: NewRegexpMatcher("redis"),
			Image:   "oliver006/redis_exporter:v0.20.2",
			Cmd: `redis_exporter
						-redis.addr=redis://localhost:6379
						-redis.alias=api_redis
						-namespace=api_redis`, //  ,{{.Config.Labels .Data "com.docker.swarm.task.name"}}
		},
		"php": PredefinedExporter{
			Matcher: NewRegexpMatcher("php"),
			Image:   "bakins/php-fpm-exporter:v0.4.1",
			Cmd: `php-fpm-exporter
						--fastcgi http://localhost:9000/_status`,
		},
		"blackbox": PredefinedExporter{
			Matcher: NewBoolMatcher(false), // This exporter is never started automatically
			Image:   "prom/blackbox-exporter:v0.12.0",
			Cmd:     ``,
		},
		"memcached": PredefinedExporter{
			Matcher: NewRegexpMatcher("memcached?"),
			Image:   "quay.io/prometheus/memcached-exporter:v0.4.1",
			Cmd:     ``,
		},
		"fluentd": PredefinedExporter{
			Matcher: NewRegexpMatcher("fluentd?"),
			Image:   "bitnami/fluentd-exporter:0.2.0",
			Cmd:     ``,
		},
		"nginx": PredefinedExporter{
			Matcher: NewRegexpMatcher("nginx"),
			Image:   "sophos/nginx-vts-exporter:v0.10.3",
			Cmd:     ``,
		},
	}
)
