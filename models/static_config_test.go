package models_test

import (
	"testing"

	"github.com/NiR-/prom-autoexporter/models"
	"gotest.tools/assert"
)

func TestStaticConfigToJSON(t *testing.T) {
	config := models.NewStaticConfig()
	config.AddTarget("10.0.0.1", map[string]string{
		"some.label": "bzzzz",
		"foo": "bar",
	})
	config.AddTarget("10.0.0.2", map[string]string{
		"another.label": "some.value",
		"bar":           "baz",
	})
	json, err := config.ToJSON()

	assert.NilError(t, err)
	assert.Equal(t, string(json), "[" +
		"{\"labels\":{\"foo\":\"bar\",\"some.label\":\"bzzzz\"},\"targets\":[\"10.0.0.1\"]}," +
		"{\"labels\":{\"another.label\":\"some.value\",\"bar\":\"baz\"},\"targets\":[\"10.0.0.2\"]}" +
	"]")
}
