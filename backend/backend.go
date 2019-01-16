package backend

import (
	"context"
	"fmt"

	"github.com/NiR-/prom-autoexporter/models"
)

type Backend interface{
	RunExporter(context.Context, models.Exporter) error
	FindMissingExporters(context.Context) []models.Exporter
	CleanupExporters(context.Context, bool) error
	CleanupExporter(context.Context, string, bool) error
	GetPromStaticConfig(context.Context) (*models.StaticConfig, error)
	ListenForTasksToExport(context.Context) (chan models.Exporter, chan error)
}

type Event interface{
	Exporter() models.Exporter
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

type errTaskToExportNotFound struct{
	taskName string
}

func (e errTaskToExportNotFound) Error() string {
	return fmt.Sprintf("task to export %q not found", e.taskName)
}

func NewErrTaskToExportNotFound(taskName string) error {
	return errTaskToExportNotFound{taskName}
}

func IsErrTaskToExportNotFound(e error) bool {
	_, ok := e.(errTaskToExportNotFound)
	return ok
}
