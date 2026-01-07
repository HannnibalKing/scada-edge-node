package model

import "time"

// SensorData is the normalized payload produced by the Modbus adapter.
type SensorData struct {
	Timestamp    time.Time
	Register     uint16
	PressurePSI  float64
	TemperatureC float64
	FlowLpm      float64
}

// Limits defines hard safety thresholds.
type Limits struct {
	MaxPressurePSI  float64
	MaxTemperatureC float64
	MaxFlowLpm      float64
}

// ShutdownEvent is emitted before acknowledging a shutdown to persist intent.
type ShutdownEvent struct {
	Timestamp time.Time
	Reason    string
	Sensor    SensorData
}

// EventSink captures shutdown events for durability (e.g., write-to-disk or DB).
type EventSink interface {
	Persist(event ShutdownEvent) error
}
