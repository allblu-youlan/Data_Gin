package job

import (
	"testing"
	"time"
)

func TestFeishuMentionPayloadContract(t *testing.T) {
	for _, tt := range []struct {
		name, payload string
		valid         bool
	}{{"valid", `{"job_id":12,"dispatch_generation":2}`, true}, {"missing generation", `{"job_id":12}`, false}, {"unknown field", `{"job_id":12,"dispatch_generation":2,"x":1}`, false}, {"trailing JSON", `{"job_id":12,"dispatch_generation":2}{}`, false}, {"zero", `{"job_id":0,"dispatch_generation":1}`, false}} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeFeishuMentionTaskPayload([]byte(tt.payload))
			if (err == nil) != tt.valid {
				t.Fatalf("error=%v", err)
			}
		})
	}
	if FeishuMentionTaskKey(12, 2) != "feishu-mention:12:g:2" {
		t.Fatal("unstable task key")
	}
	definitions := FeishuMentionOutboxTaskDefinitions(5, 30*time.Second)
	if definitions[0].MaxRetry != 4 || definitions[0].Queue != "delivery" {
		t.Fatalf("definitions=%#v", definitions)
	}
}
