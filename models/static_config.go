package models

import (
	"encoding/json"
)

// The StaticConfig holds a set of targets with their associated labels
type StaticConfig struct {
	Targets map[string]map[string]string
}

func NewStaticConfig() *StaticConfig {
	return &StaticConfig{
		Targets: make(map[string]map[string]string),
	}
}

func (c *StaticConfig) AddTarget(target string, labels map[string]string) {
	c.Targets[target] = labels
}

func (c *StaticConfig) ToJSON() ([]byte, error) {
	config := make([]map[string]interface{}, 0)

	for target, labels := range c.Targets {
		config = append(config, map[string]interface{}{
			"targets": []string{target},
			"labels": labels,
		})
	}

	content, err := json.Marshal(config)
	if err != nil {
		return []byte{}, err
	}

	return content, nil
}
