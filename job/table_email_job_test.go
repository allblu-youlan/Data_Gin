package job

import "testing"

func TestTableEmailTaskPayloadHasDeliveryIdentity(t *testing.T) {
	task, err := NewTableEmailDeliveryTask([]byte(`{"delivery_id":19,"retry_version":2}`))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := DecodeTableEmailDeliveryPayload(task.Payload())
	if err != nil || payload.DeliveryID != 19 || payload.RetryVersion != 2 {
		t.Fatalf("payload: %#v %v", payload, err)
	}
	for _, invalid := range []string{`{}`, `{"delivery_id":0}`, `{"delivery_id":1,"email":"x@example.com"}`, `{"delivery_id":1} {}`} {
		if _, err := NewTableEmailDeliveryTask([]byte(invalid)); err == nil {
			t.Errorf("accepted %s", invalid)
		}
	}
}
