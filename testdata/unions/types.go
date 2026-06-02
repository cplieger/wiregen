// Package unions provides test types for wiregen union tests.
package unions

// EventType is a typed string for event types.
type EventType string

// EventData is a sealed interface for event payloads.
//
//wiregen:union discriminator=type variants=CoverageEvent,NotifyEvent,ScanEvent
type EventData interface{ eventData() }

// CoverageEvent is the payload for coverage updates.
type CoverageEvent struct {
	MediaID  string `json:"media_id"`
	Language string `json:"language"`
}

func (CoverageEvent) eventData() {}

// NotifyEvent is the payload for notifications.
type NotifyEvent struct {
	Level string `json:"level"`
	Text  string `json:"text"`
}

func (NotifyEvent) eventData() {}

// ScanEvent is the payload for scan events.
type ScanEvent struct {
	Action    string `json:"action"`
	Succeeded bool   `json:"succeeded,omitempty"`
}

func (ScanEvent) eventData() {}
