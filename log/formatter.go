package log

import (
	"github.com/NiR-/prom-autoexporter/models"
)

func FormatExporterField(exporter models.Exporter) map[string]string {
	return map[string]string{
		"name": exporter.Name,
		"type": exporter.ExporterType,
	}
}

func FormatTaskField(task models.TaskToExport) map[string]string {
	return map[string]string{
		"id":   task.ID,
		"name": task.Name,
	}
}
