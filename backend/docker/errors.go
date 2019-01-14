package docker

import "fmt"

type errExportedStilRunning struct {
	exporterID string
	exportedID string
}

func newErrExportedTaskStillRunning(exporterID, exportedID string) errExportedStilRunning {
	return errExportedStilRunning{exporterID, exportedID}
}

func (e errExportedStilRunning) Error() string {
	return fmt.Sprintf("Exporter %q can't be stopped, exported container %q still running.", e.exporterID, e.exportedID)
}

func IsErrExportedTaskStillRunning(e error) bool {
	_, ok := e.(errExportedStilRunning)
	return ok
}
