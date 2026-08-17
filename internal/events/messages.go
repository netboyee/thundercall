package events

import "encoding/json"

const (
	EventTypeMessageAccepted = "message.accepted"
)

type MessageAccepted struct {
	MessageID int64 `json:"messageId"`
}

func EncodeMessageAccepted(messageID int64) (string, error) {
	payload, err := json.Marshal(MessageAccepted{MessageID: messageID})
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func DecodeMessageAccepted(payload string) (MessageAccepted, error) {
	var event MessageAccepted
	err := json.Unmarshal([]byte(payload), &event)
	return event, err
}
