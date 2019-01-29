package models_test

import (
	"testing"

	"github.com/NiR-/prom-autoexporter/models"
	"gotest.tools/assert"
	is "gotest.tools/assert/cmp"
)

func TestNewExporter(t *testing.T) {
	testcases := []struct{
		name         string
		exporterType string
		image        string
		cmd          []string
		envVars      []string
		port         string
		taskToExport models.TaskToExport
		expected     models.Exporter
	}{
		{
			name:         "exporter.redis",
			exporterType: "redis",
			image:        "oliver006/redis_exporter:v0.25.0",
			cmd:          []string{"-namespace={{ index .Labels \"service.name\" }}"},
			envVars:      []string{"FOO={{ index .Labels \"service.name\" }}"},
			port:         "9121",
			taskToExport: models.TaskToExport{
				ID:   "exported-task-cid",
				Name: "exported-task",
				Labels: map[string]string{"service.name": "redis"},
			},
			expected: models.Exporter{
				Name:         "exporter.redis",
				ExporterType: "redis",
				Image:        "oliver006/redis_exporter:v0.25.0",
				Cmd:          []string{"-namespace=redis"},
				EnvVars:      []string{"FOO=redis"},
				Port:         "9121",
				ExportedTask: models.TaskToExport{
					ID:   "exported-task-cid",
					Name: "exported-task",
					Labels: map[string]string{"service.name": "redis"},
				},
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			// tc := testcases[tcname]
			// t.Parallel()

			exporter, err := models.NewExporter(tc.name, tc.exporterType, tc.image, tc.cmd, tc.envVars, tc.port, tc.taskToExport)
			assert.NilError(t, err)
			assert.DeepEqual(t, exporter, tc.expected)
		})
	}
}

func TestPredefinedExporterFinder(t *testing.T) {
	testcases := map[string]struct{
		taskToExport  models.TaskToExport
		expectedTypes []string
	}{
		"matches predefined exporter \"redis\" based on task name": {
			taskToExport: models.TaskToExport{
				Name:   "redis",
				Labels: map[string]string{},
			},
			expectedTypes: []string{"redis"},
		},
		"matches predefined exporter \"php\" based on task name": {
			taskToExport: models.TaskToExport{
				Name: "php",
				Labels: map[string]string{},
			},
			expectedTypes: []string{"php"},
		},
		"matches predefined exporter \"elasticsearch\" based on task name": {
			taskToExport: models.TaskToExport{
				Name: "elasticsearch",
				Labels: map[string]string{},
			},
			expectedTypes: []string{"elasticsearch"},
		},
		"matches predefined exporter \"fluentd\" when task name contains \"fluentd\"": {
			taskToExport: models.TaskToExport{
				Name: "fluentd",
				Labels: map[string]string{},
			},
			expectedTypes: []string{"fluentd"},
		},
		"matches predefined exporter \"fluentd\" when task name contains \"fluent\"": {
			taskToExport: models.TaskToExport{
				Name: "fluent",
				Labels: map[string]string{},
			},
			expectedTypes: []string{"fluentd"},
		},
		"matches predefined exporter \"nginx\" based on task name": {
			taskToExport: models.TaskToExport{
				Name: "nginx",
				Labels: map[string]string{},
			},
			expectedTypes: []string{"nginx"},
		},
	}

	for tcname, tc := range testcases {
		t.Run(tcname, func(t *testing.T) {
			// tc := testcases[tcname]
			// tc.Parallel()

			f := models.NewPredefinedExporterFinder()
			found, err := f.FindMatchingExporters(tc.taskToExport)
			assert.Assert(t, is.Len(err, 0))

			types := []string{}
			for _, exporter := range found {
				types = append(types, exporter.ExporterType)
			}

			assert.DeepEqual(t, types, tc.expectedTypes)
		})
	}
}
