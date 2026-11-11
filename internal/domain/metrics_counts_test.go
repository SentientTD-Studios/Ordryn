package domain

import (
	"testing"

	"GoTodo/internal/storage"
)

func TestMetricsCountsQueries(t *testing.T) {
	setupCompanySite(t, "gotodo_metrics_test")
	c, err := storage.GetMetricsCounts()
	if err != nil {
		t.Fatal(err)
	}
	if c.ActiveUsers < 1 {
		t.Fatalf("the site owner should count as active: %+v", c)
	}
}
