package models

type EventType int

const (
	TaskStarted EventType = iota
	TaskStopped
)

type TaskEvent struct {
	Task      TaskToExport
	Type      EventType
	Exporters []Exporter
}
