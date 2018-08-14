package models

// This struct matches the structure defined for the static_config config format
// defined by Prometheus, see: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#static_config
type StaticConfig struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels"`
}

func NewStaticConfig() *StaticConfig {
	return &StaticConfig{
		Targets: make([]string, 0),
		Labels:  make(map[string]string),
	}
}

func (c *StaticConfig) AddTarget(target string) {
	c.Targets = append(c.Targets, target)
}
