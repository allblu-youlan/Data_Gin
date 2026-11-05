package shanghaimall

import (
	"encoding/json"
	"io"
)

func decodeAndDrainJSON(body io.Reader, destination any) error {
	if err := json.NewDecoder(body).Decode(destination); err != nil {
		return err
	}
	// Consume the remaining response while preserving Decode's tolerance of trailing content.
	_, err := io.Copy(io.Discard, body)
	return err
}
