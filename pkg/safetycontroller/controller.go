package safetycontroller

import (
	"context"
	"log"
	"sync"
	"time"

	"vanguardedge/pkg/modbusadapter"
	"vanguardedge/pkg/model"
)

// ControllerConfig captures Guardian behavior.
type ControllerConfig struct {
	Limits            model.Limits
	EmergencyRegister uint16
	EventSink         model.EventSink
	Logger            *log.Logger
}

// SafetyController consumes sensor data and triggers shutdowns.
type SafetyController struct {
	cfg     ControllerConfig
	adapter *modbusadapter.Adapter
	stop    chan struct{}
	wg      sync.WaitGroup
}

// NewSafetyController builds a new guardian.
func NewSafetyController(cfg ControllerConfig, adapter *modbusadapter.Adapter) *SafetyController {
	if cfg.Logger == nil {
		cfg.Logger = log.Default()
	}
	return &SafetyController{
		cfg:     cfg,
		adapter: adapter,
		stop:    make(chan struct{}),
	}
}

// Start begins consuming the sensor channel with priority on incoming data.
func (c *SafetyController) Start(ctx context.Context, in <-chan model.SensorData) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.consume(ctx, in)
	}()
}

// Stop signals termination and waits for completion.
func (c *SafetyController) Stop() {
	close(c.stop)
	c.wg.Wait()
}

func (c *SafetyController) consume(ctx context.Context, in <-chan model.SensorData) {
	for {
		select {
		case sample, ok := <-in:
			if !ok {
				return
			}
			c.handleSample(ctx, sample)
		case <-c.stop:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (c *SafetyController) handleSample(parent context.Context, sample model.SensorData) {
	if c.exceeds(sample) {
		if err := c.publishShutdown(sample); err != nil {
			c.cfg.Logger.Printf("shutdown event persistence failed: %v", err)
		}

		ctx, cancel := context.WithTimeout(parent, 5*time.Millisecond)
		defer cancel()

		if err := c.adapter.WriteCoil(ctx, c.cfg.EmergencyRegister, true); err != nil {
			c.cfg.Logger.Printf("emergency coil write failed: %v", err)
		}
	}
}

func (c *SafetyController) exceeds(s model.SensorData) bool {
	if c.cfg.Limits.MaxPressurePSI > 0 && s.PressurePSI > c.cfg.Limits.MaxPressurePSI {
		return true
	}
	if c.cfg.Limits.MaxTemperatureC > 0 && s.TemperatureC > c.cfg.Limits.MaxTemperatureC {
		return true
	}
	if c.cfg.Limits.MaxFlowLpm > 0 && s.FlowLpm > c.cfg.Limits.MaxFlowLpm {
		return true
	}
	return false
}

func (c *SafetyController) publishShutdown(sample model.SensorData) error {
	if c.cfg.EventSink == nil {
		return nil
	}
	event := model.ShutdownEvent{
		Timestamp: time.Now(),
		Reason:    "safety threshold exceeded",
		Sensor:    sample,
	}
	return c.cfg.EventSink.Persist(event)
}
