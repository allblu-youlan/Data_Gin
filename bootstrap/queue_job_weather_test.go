package bootstrap

import (
	"testing"

	"gin-biz-web-api/job"
)

func TestQueueJobServerSpecsIsolatesWeatherQueue(t *testing.T) {
	configured := map[string]int{
		"default": 3, job.MallWeatherQueueName: 5,
		job.MallExportQueueName: 2, job.MallDeliveryQueueName: 2,
	}
	specs := queueJobServerSpecs(configured, 10, false, 2, 4, 2, 1, true, 20)
	if len(specs) != 2 {
		t.Fatalf("server specs = %#v, want separate general and weather workers", specs)
	}
	general, weather := specs[0], specs[1]
	if general.name != "default" || general.concurrency != 10 || len(general.queues) != 3 {
		t.Fatalf("general worker = %#v", general)
	}
	if _, exists := general.queues[job.MallWeatherQueueName]; exists {
		t.Fatal("general worker still consumes weather tasks")
	}
	for queue, weight := range map[string]int{"default": 3, job.MallExportQueueName: 2, job.MallDeliveryQueueName: 2} {
		if general.queues[queue] != weight {
			t.Fatalf("general queue %q weight = %d, want %d", queue, general.queues[queue], weight)
		}
	}
	if weather.name != "weather" || weather.concurrency != 20 || len(weather.queues) != 1 || weather.queues[job.MallWeatherQueueName] != 5 {
		t.Fatalf("weather worker = %#v", weather)
	}
	if configured[job.MallWeatherQueueName] != 5 {
		t.Fatal("worker setup mutated configured queues")
	}
}

func TestQueueJobServerSpecsOnlyIsolatesEnabledConfiguredWeather(t *testing.T) {
	tests := []struct {
		name           string
		weatherEnabled bool
		queues         map[string]int
	}{
		{name: "feature disabled", queues: map[string]int{"default": 3, job.MallWeatherQueueName: 5}},
		{name: "weather queue omitted", weatherEnabled: true, queues: map[string]int{"default": 3}},
		{name: "weather queue weight disabled", weatherEnabled: true, queues: map[string]int{"default": 3, job.MallWeatherQueueName: 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			specs := queueJobServerSpecs(tt.queues, 10, false, 2, 4, 2, 1, tt.weatherEnabled, 20)
			if len(specs) != 1 || specs[0].name != "default" || len(specs[0].queues) != len(tt.queues) {
				t.Fatalf("server specs = %#v, want the configured general queues", specs)
			}
			for queue, weight := range tt.queues {
				if specs[0].queues[queue] != weight {
					t.Fatalf("queue %q weight = %d, want %d", queue, specs[0].queues[queue], weight)
				}
			}
		})
	}
}

func TestQueueJobServerSpecsWeatherOnlyUsesConfiguredConcurrency(t *testing.T) {
	tests := []struct {
		name               string
		weatherConcurrency int
		defaultConcurrency int
		wantConcurrency    int
	}{
		{name: "configured", weatherConcurrency: 13, defaultConcurrency: 7, wantConcurrency: 13},
		{name: "zero defaults to sequential", defaultConcurrency: 7, wantConcurrency: 1},
		{name: "negative defaults to sequential", weatherConcurrency: -1, defaultConcurrency: 7, wantConcurrency: 1},
		{name: "invalid general still defaults to sequential", weatherConcurrency: -1, defaultConcurrency: 0, wantConcurrency: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			specs := queueJobServerSpecs(map[string]int{job.MallWeatherQueueName: 5}, tt.defaultConcurrency,
				false, 2, 4, 2, 1, true, tt.weatherConcurrency)
			if len(specs) != 1 || specs[0].name != "weather" || specs[0].concurrency != tt.wantConcurrency ||
				len(specs[0].queues) != 1 || specs[0].queues[job.MallWeatherQueueName] != 5 {
				t.Fatalf("server specs = %#v, want weather concurrency %d", specs, tt.wantConcurrency)
			}
		})
	}
}
