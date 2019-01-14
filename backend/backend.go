package backend

import (
  "context"

  "github.com/NiR-/prom-autoexporter/models"
)

interface Backend {
  RunExporter(context.Context, models.Exporter) error
  FindMissingExporters(context.Context) error
  CleanupExporters(context.Context, bool) error
  CleanupExporter(context.Context, string, bool) error
  GetPromStaticConfig(context.Context) (*models.StaticConfig, error)
  ListenForTasksToExport(context.Context) (chan models.Exporter, chan error)
}

interface Event {
  Exporter() models.Exporer
}

type TaskToExportStarted struct {
  exporter models.Exporter
}

func (evt TaskToExportStarted) Exporter() models.Exporter {
  return evt.exporter
}

type TaskToExportStopped struct {
  exporter models.Exporter
}

func (evt TaskToExportStopped) Exporter() models.Exporter {
  return evt.exporter
}
