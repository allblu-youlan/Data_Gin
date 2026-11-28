package job

import (
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"
)

// DecodeTaskPayload preserves the JSON cause and marks malformed tasks terminal.
// The payload itself is never included in the returned diagnostic.
func DecodeTaskPayload(task *asynq.Task, destination any) error {
	if err := json.Unmarshal(task.Payload(), destination); err != nil {
		return fmt.Errorf("%s: invalid task payload: %w: %w", task.Type(), asynq.SkipRetry, err)
	}
	return nil
}
