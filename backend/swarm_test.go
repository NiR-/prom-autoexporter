package backend_test

import (
	"context"
	"testing"

	"github.com/NiR-/prom-autoexporter/backend"
	"github.com/NiR-/prom-autoexporter/models"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/swarm"
	"gotest.tools/assert"
)

func TestGetPromStaticConfig(t *testing.T) {
	testcases := map[string]struct {
		cli            *fakeClient
		finder         *fakeExporterFinder
		expectedConfig *models.StaticConfig
		expectedError  string
	}{
		"successfully generate static config for a bunch of services attached to prom network": {
			cli: &fakeClient{
				networkInspectFn: func(fc *fakeCall, ctx context.Context, networkID string, opts types.NetworkInspectOptions) (types.NetworkResource, error) {
					assert.Equal(fc.t, networkID, "promnetwork")
					return types.NetworkResource{
						Containers: map[string]types.EndpointResource{
							"59c129c96e05e237a4be5f13537f1403527408f74d8456acbb417a3aa1da8b85": {
								Name:        "api_redis.1.uisp4edp88ne3r1yzg4i93la0",
								IPv4Address: "10.0.6.8/24",
							},
							"cd8286850bd2a4a6f0c0eae85b04c5c6d7d7ee26f6aa6ad58ab7af3034459cbf": {
								Name:        "api_nginx.1.d74b1sq539our2wpqnwwewcbv",
								IPv4Address: "10.0.6.7/24",
							},
							"f8adec22d9174d1507e48267ae7d6fd9da7d139f7bcce05b9a71d83c0a09a388": {
								Name:        "monit_fluentd.1.tkp2g2iylq6gax5cqbhmgupfk",
								IPv4Address: "10.0.6.4/24",
							},
						},
					}, nil
				},
				taskListFn: func(fc *fakeCall, ctx context.Context, opts types.TaskListOptions) ([]swarm.Task, error) {
					assert.Assert(fc.t, opts.Filters.ExactMatch("desired-state", "running"))
					return []swarm.Task{
						{
							ID:        "uisp4edp88ne3r1yzg4i93la0",
							Slot:      1,
							ServiceID: "api_redis-service-id",
							Spec: swarm.TaskSpec{
								Runtime: swarm.RuntimeContainer,
							},
						},
						{
							ID:        "d74b1sq539our2wpqnwwewcbv",
							Slot:      1,
							ServiceID: "api_nginx-service-id",
							Spec: swarm.TaskSpec{
								Runtime: swarm.RuntimeContainer,
							},
						},
						{
							ID:        "tkp2g2iylq6gax5cqbhmgupfk",
							Slot:      1,
							ServiceID: "monit_fluentd-service-id",
							Spec: swarm.TaskSpec{
								Runtime: swarm.RuntimeContainer,
							},
						},
					}, nil
				},
				serviceInspectFn: func(fc *fakeCall, ctx context.Context, serviceID string, opts types.ServiceInspectOptions) (swarm.Service, []byte, error) {
					var service swarm.Service

					switch serviceID {
					case "api_redis-service-id":
						service = swarm.Service{
							Spec: swarm.ServiceSpec{
								Annotations: swarm.Annotations{
									Name:   "api_redis",
									Labels: map[string]string{},
								},
							},
						}
					case "api_nginx-service-id":
						service = swarm.Service{
							Spec: swarm.ServiceSpec{
								Annotations: swarm.Annotations{
									Name:   "api_nginx",
									Labels: map[string]string{},
								},
							},
						}
					case "monit_fluentd-service-id":
						service = swarm.Service{
							Spec: swarm.ServiceSpec{
								Annotations: swarm.Annotations{
									Name:   "monit_fluentd",
									Labels: map[string]string{},
								},
							},
						}
					default:
						t.Errorf("Unexpected service ID %q", serviceID)
					}

					return service, []byte{}, nil
				},
			},
			finder: &fakeExporterFinder{
				findMatchingExportersFn: func(t models.TaskToExport) map[string]models.Exporter {
					var name, exporterType, image, port string

					switch t.Name {
					case "api_redis.1.uisp4edp88ne3r1yzg4i93la0":
						name = "/exporter.redis.api_redis.1.uisp4edp88ne3r1yzg4i93la0"
						exporterType = "redis"
						image = "oliver006/redis_exporter:v0.25.0"
						port = "9121"
					case "api_nginx.1.d74b1sq539our2wpqnwwewcbv":
						name = "/exporter.nginx.api_nginx.1.d74b1sq539our2wpqnwwewcbv"
						exporterType = "nginx"
						image = "nginx/nginx-prometheus-exporter:0.2.0"
						port = "9113"
					case "monit_fluentd.1.tkp2g2iylq6gax5cqbhmgupfk":
						name = "/exporter.fluentd.monit_fluentd.1.tkp2g2iylq6gax5cqbhmgupfk"
						exporterType = "fluentd"
						image = "bitnami/fluentd-exporter:0.2.0"
						port = "9309"
					}

					exporter, _ := models.NewExporter(name, exporterType, image, []string{}, []string{}, port, t)
					return map[string]models.Exporter{name: exporter}
				},
			},
			expectedConfig: &models.StaticConfig{
				Targets: map[string]map[string]string{
					"10.0.6.8:9121": map[string]string{
						"job":                "autoexporter-redis",
						"swarm_service_name": "api_redis",
						"swarm_task_slot":    "1",
						"swarm_task_id":      "uisp4edp88ne3r1yzg4i93la0",
					},
					"10.0.6.7:9113": map[string]string{
						"job":                "autoexporter-nginx",
						"swarm_service_name": "api_nginx",
						"swarm_task_slot":    "1",
						"swarm_task_id":      "d74b1sq539our2wpqnwwewcbv",
					},
					"10.0.6.4:9309": map[string]string{
						"job":                "autoexporter-fluentd",
						"swarm_service_name": "monit_fluentd",
						"swarm_task_slot":    "1",
						"swarm_task_id":      "tkp2g2iylq6gax5cqbhmgupfk",
					},
				},
			},
			expectedError: "",
		},
		"generates empty config when no tasks matching endpoints": {
			cli: &fakeClient{
				networkInspectFn: func(*fakeCall, context.Context, string, types.NetworkInspectOptions) (types.NetworkResource, error) {
					return types.NetworkResource{
						Containers: map[string]types.EndpointResource{
							"59c129c96e05e237a4be5f13537f1403527408f74d8456acbb417a3aa1da8b85": {
								Name:        "api_redis.1.uisp4edp88ne3r1yzg4i93la0",
								IPv4Address: "10.0.6.8/24",
							},
						},
					}, nil
				},
				taskListFn: func(*fakeCall, context.Context, types.TaskListOptions) ([]swarm.Task, error) {
					return []swarm.Task{}, nil
				},
			},
			finder:         &fakeExporterFinder{},
			expectedConfig: &models.StaticConfig{Targets: map[string]map[string]string{}},
			expectedError:  "",
		},
		"generates multiple prometheus targets when a service has multiple tasks": {
			cli: &fakeClient{
				networkInspectFn: func(fc *fakeCall, ctx context.Context, networkID string, opts types.NetworkInspectOptions) (types.NetworkResource, error) {
					return types.NetworkResource{
						Containers: map[string]types.EndpointResource{
							"59c129c96e05e237a4be5f13537f1403527408f74d8456acbb417a3aa1da8b85": {
								Name:        "api_redis.1.uisp4edp88ne3r1yzg4i93la0",
								IPv4Address: "10.0.6.8/24",
							},
							"e7d90ad157efcc9d5db3359eb3863a983fe699c0e1eb03242b183b9dea72d64a": {
								Name:        "api_redis.2.tpfsipego2f2duqdbxg7oqc2k",
								IPv4Address: "10.0.6.9/24",
							},
						},
					}, nil
				},
				taskListFn: func(fc *fakeCall, ctx context.Context, opts types.TaskListOptions) ([]swarm.Task, error) {
					return []swarm.Task{
						{
							ID:        "uisp4edp88ne3r1yzg4i93la0",
							Slot:      1,
							ServiceID: "api_redis-service-id",
							Spec: swarm.TaskSpec{
								Runtime: swarm.RuntimeContainer,
							},
						},
						{
							ID:        "tpfsipego2f2duqdbxg7oqc2k",
							Slot:      2,
							ServiceID: "api_redis-service-id",
							Spec: swarm.TaskSpec{
								Runtime: swarm.RuntimeContainer,
							},
						},
					}, nil
				},
				serviceInspectFn: func(fc *fakeCall, ctx context.Context, serviceID string, opts types.ServiceInspectOptions) (swarm.Service, []byte, error) {
					return swarm.Service{
						Spec: swarm.ServiceSpec{
							Annotations: swarm.Annotations{
								Name:   "api_redis",
								Labels: map[string]string{},
							},
						},
					}, []byte{}, nil
				},
			},
			finder: &fakeExporterFinder{
				findMatchingExportersFn: func(t models.TaskToExport) map[string]models.Exporter {
					var name string

					switch t.Name {
					case "api_redis.1.uisp4edp88ne3r1yzg4i93la0":
						name = "/exporter.redis.api_redis.1.uisp4edp88ne3r1yzg4i93la0"
					case "api_redis.2.tpfsipego2f2duqdbxg7oqc2k":
						name = "/exporter.redis.api_redis.2.tpfsipego2f2duqdbxg7oqc2k"
					}

					exporter, _ := models.NewExporter(name, "redis", "oliver006/redis_exporter:v0.25.0", []string{}, []string{}, "9121", t)
					return map[string]models.Exporter{name: exporter}
				},
			},
			expectedConfig: &models.StaticConfig{
				Targets: map[string]map[string]string{
					"10.0.6.8:9121": map[string]string{
						"job":                "autoexporter-redis",
						"swarm_service_name": "api_redis",
						"swarm_task_slot":    "1",
						"swarm_task_id":      "uisp4edp88ne3r1yzg4i93la0",
					},
					"10.0.6.9:9121": map[string]string{
						"job":                "autoexporter-redis",
						"swarm_service_name": "api_redis",
						"swarm_task_slot":    "2",
						"swarm_task_id":      "tpfsipego2f2duqdbxg7oqc2k",
					},
				},
			},
			expectedError: "",
		},
	}

	for tcname, _ := range testcases {
		t.Run(tcname, func(t *testing.T) {
			tc := testcases[tcname]
			tc.cli.t = t
			t.Parallel()

			ctx := context.Background()
			b := backend.NewSwarmBackend(tc.cli, "promnetwork", tc.finder)
			config, err := b.GetPromStaticConfig(ctx)

			if tc.expectedError == "" {
				assert.NilError(t, err)
				assert.DeepEqual(t, config, tc.expectedConfig)
			} else {
				assert.Error(t, err, tc.expectedError)
			}
		})
	}
}
