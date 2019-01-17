package docker_test

import (
	"testing"
	"context"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/NiR-/prom-autoexporter/backend/docker"
	"github.com/NiR-/prom-autoexporter/log"
	"github.com/NiR-/prom-autoexporter/models"
	"gotest.tools/assert"
)

func TestListenForTasksToExport(t *testing.T) {
	log.ConfigureDefaultLogger("debug")

	testcases := map[string]struct{
		dockerEvent   events.Message
		expectedEvent models.TaskEvent
	}{
		"sends a task started event when a docker container starts": {
			dockerEvent: events.Message{
				Action: "start",
				Actor: events.Actor{
					ID: "/container-to-export-cid",
					Attributes: map[string]string{
						"name": "container-to-export",
						"image": "some/image",
						"foo": "bar",
						"some.label": "bzzzz",
					},
				},
			},
			expectedEvent: models.TaskEvent{
				Task: models.TaskToExport{
					ID:     "/container-to-export-cid",
					Name:   "container-to-export",
					Labels: map[string]string{
						"foo": "bar",
						"some.label": "bzzzz",
					},
				},
				Type: models.TaskStarted,
				Exporters: []models.Exporter{
					{
						Name:  "/exporter.redis.container-to-export",
						ExporterType: "redis",
					},
				},
			},
		},
		"sends a task stopped event when a constainer die": {
			dockerEvent: events.Message{
				Action: "die",
				Actor: events.Actor{
					ID: "/container-to-export-cid",
					Attributes: map[string]string{
						"name": "container-to-export",
						"image": "some/image",
						"exitCode": "137",
						"foo": "bar",
						"some.label": "bzzzz",
					},
				},
			},
			expectedEvent: models.TaskEvent{
				Task: models.TaskToExport{
					ID:   "/container-to-export-cid",
					Name: "container-to-export",
					Labels: map[string]string{
						"foo": "bar",
						"some.label": "bzzzz",
					},
				},
				Type: models.TaskStopped,
				Exporters: []models.Exporter{
					{
						Name:         "/exporter.redis.container-to-export",
						ExporterType: "redis",
					},
				},
			},
		},
	}

	for tcname, tc := range testcases {
		t.Run(tcname, func(t *testing.T) {
			cli := newFakeEventsListener([]events.Message{tc.dockerEvent})
			f := &fakeExporterFinder{
				findMatchingExportersFn: func(t models.TaskToExport) map[string]models.Exporter {
					return map[string]models.Exporter{
						"redis": {
							ExporterType: "redis",
						},
					}
				},
			}
			b := docker.NewDockerBackend(cli, "", f)

			ctx := context.Background()
			taskEvtCh := make(chan models.TaskEvent)
			go b.ListenForTasksToExport(ctx, taskEvtCh)

			timer := time.NewTimer(1 * time.Second)
			for {
				select {
				case <-timer.C:
					t.Error("Test timed out.")
					return
				case received := <-taskEvtCh:
					assert.DeepEqual(t, received, tc.expectedEvent)
					return
				}
			}
		})
	}
}

func newFakeEventsListener(evts []events.Message) *fakeClient {
	return &fakeClient{
		eventsFn: func(ctx context.Context, opts types.EventsOptions) (<-chan events.Message, <-chan error) {
			evtCh := make(chan events.Message)
			errCh := make(chan error)

			go func() {
				for _, evt := range evts {
					evtCh <-evt
				}
			}()

			return evtCh, errCh
		},
	}
}

func (c *fakeClient) Events(ctx context.Context, opts types.EventsOptions) (<-chan events.Message, <-chan error) {
	if c.eventsFn != nil {
		return c.eventsFn(ctx, opts)
	}
	evtCh := make(<-chan events.Message)
	errCh := make(<-chan error)
	return evtCh, errCh
}
