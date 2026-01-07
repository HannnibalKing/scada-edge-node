package modbusadapter

import (
	"context"
	"encoding/binary"
	"errors"
	"log"
	"math"
	"sync"
	"time"

	"github.com/goburrow/modbus"

	"vanguardedge/pkg/model"
)

// AdapterConfig captures how the translator service should behave.
type AdapterConfig struct {
	Address       string
	UnitID        byte
	PollInterval  time.Duration
	DialTimeout   time.Duration
	BackoffMin    time.Duration
	BackoffMax    time.Duration
	Registers     []RegisterPoll
	ChannelBuffer int
}

// FieldType enumerates supported decode formats.
type FieldType string

const (
	Int16   FieldType = "int16"
	Uint16  FieldType = "uint16"
	Float32 FieldType = "float32"
)

// FieldSpec describes how to decode a single measurement field.
// Register is zero-based (e.g., Modbus holding 40001 -> Register 0).
// Scale is applied as raw/Scale.
type FieldSpec struct {
	Register uint16
	Type     FieldType
	Scale    float64
	ByteSwap bool
	WordSwap bool
}

// RegisterPoll maps Modbus register addresses for a sensor triplet.
type RegisterPoll struct {
	Pressure    FieldSpec
	Temperature FieldSpec
	Flow        FieldSpec
}

// Adapter reads Modbus registers and emits SensorData into a buffered channel.
type Adapter struct {
	cfg     AdapterConfig
	handler *modbus.TCPClientHandler
	client  modbus.Client

	out  chan model.SensorData
	stop chan struct{}
	wg   sync.WaitGroup
	mu   sync.Mutex
}

// NewAdapter builds a Modbus adapter with the provided config.
func NewAdapter(cfg AdapterConfig) *Adapter {
	if cfg.BackoffMin == 0 {
		cfg.BackoffMin = 100 * time.Millisecond
	}
	if cfg.BackoffMax == 0 {
		cfg.BackoffMax = 3 * time.Second
	}
	if cfg.ChannelBuffer == 0 {
		cfg.ChannelBuffer = 256
	}
	return &Adapter{
		cfg:  cfg,
		out:  make(chan model.SensorData, cfg.ChannelBuffer),
		stop: make(chan struct{}),
	}
}

// Out exposes the buffered sensor channel for downstream consumers.
func (a *Adapter) Out() <-chan model.SensorData {
	return a.out
}

// Start connects and spawns a polling goroutine per register.
func (a *Adapter) Start(ctx context.Context) {
	for _, poll := range a.cfg.Registers {
		poll := poll
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.pollLoop(ctx, poll)
		}()
	}
}

// Stop signals goroutines and closes resources.
func (a *Adapter) Stop() {
	close(a.stop)
	a.wg.Wait()
	a.mu.Lock()
	if a.handler != nil {
		_ = a.handler.Close()
	}
	close(a.out)
	a.mu.Unlock()
}

// WriteCoil synchronously writes a coil/register to trigger physical action.
func (a *Adapter) WriteCoil(ctx context.Context, register uint16, state bool) error {
	if err := a.ensureConnected(); err != nil {
		return err
	}

	req := make(chan error, 1)
	go func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		_, err := a.client.WriteSingleCoil(register, boolToCoil(state))
		req <- err
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-req:
		return err
	}
}

// ReadCoil reads a single coil/discrete output and returns its boolean state.
func (a *Adapter) ReadCoil(ctx context.Context, register uint16) (bool, error) {
	if err := a.ensureConnected(); err != nil {
		return false, err
	}

	req := make(chan struct {
		val bool
		err error
	}, 1)

	go func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		resp, err := a.client.ReadCoils(register, 1)
		var val bool
		if err == nil && len(resp) > 0 {
			val = resp[0]&0x01 == 0x01
		}
		req <- struct {
			val bool
			err error
		}{val: val, err: err}
	}()

	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case r := <-req:
		return r.val, r.err
	}
}

func (a *Adapter) pollLoop(ctx context.Context, poll RegisterPoll) {
	backoff := a.cfg.BackoffMin
	for {
		select {
		case <-a.stop:
			return
		case <-ctx.Done():
			return
		default:
		}

		if err := a.ensureConnected(); err != nil {
			backoff = a.backoffSleep(backoff, err)
			continue
		}

		if err := a.readAndEmit(poll); err != nil {
			backoff = a.backoffSleep(backoff, err)
			continue
		}

		backoff = a.cfg.BackoffMin
		time.Sleep(a.cfg.PollInterval)
	}
}

func (a *Adapter) ensureConnected() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.handler != nil && a.client != nil {
		return nil
	}

	handler := modbus.NewTCPClientHandler(a.cfg.Address)
	handler.Timeout = a.cfg.DialTimeout
	handler.SlaveId = a.cfg.UnitID
	handler.Logger = log.New(log.Writer(), "", log.LstdFlags)

	if err := handler.Connect(); err != nil {
		return err
	}

	a.handler = handler
	a.client = modbus.NewClient(handler)
	return nil
}

func (a *Adapter) readAndEmit(poll RegisterPoll) error {
	a.mu.Lock()
	client := a.client
	a.mu.Unlock()

	if client == nil {
		return errors.New("modbus client not connected")
	}

	minReg, maxReg := registerRange(poll)
	count := maxReg - minReg + 1

	data, err := client.ReadHoldingRegisters(minReg, count)
	if err != nil {
		a.resetConnection()
		return err
	}

	pressure, err := decodeField(data, poll.Pressure, minReg)
	if err != nil {
		return err
	}
	temp, err := decodeField(data, poll.Temperature, minReg)
	if err != nil {
		return err
	}
	flow, err := decodeField(data, poll.Flow, minReg)
	if err != nil {
		return err
	}

	sample := model.SensorData{
		Timestamp:    time.Now(),
		Register:     poll.Pressure.Register,
		PressurePSI:  pressure,
		TemperatureC: temp,
		FlowLpm:      flow,
	}

	select {
	case a.out <- sample:
	default:
		// If buffer is full, drop oldest to avoid blocking critical path.
		select {
		case <-a.out:
		default:
		}
		a.out <- sample
	}
	return nil
}

func (a *Adapter) backoffSleep(current time.Duration, _ error) time.Duration {
	next := time.Duration(math.Min(float64(a.cfg.BackoffMax), float64(current)*2))
	time.Sleep(current)
	return next
}

func (a *Adapter) resetConnection() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.handler != nil {
		_ = a.handler.Close()
	}
	a.handler = nil
	a.client = nil
}

func boolToCoil(v bool) uint16 {
	if v {
		return 0xFF00
	}
	return 0x0000
}

func registerRange(poll RegisterPoll) (uint16, uint16) {
	fields := []FieldSpec{poll.Pressure, poll.Temperature, poll.Flow}
	minReg := ^uint16(0)
	maxReg := uint16(0)
	for _, f := range fields {
		width := fieldWidth(f)
		if f.Register < minReg {
			minReg = f.Register
		}
		end := f.Register + width - 1
		if end > maxReg {
			maxReg = end
		}
	}
	return minReg, maxReg
}

func decodeField(data []byte, spec FieldSpec, base uint16) (float64, error) {
	width := fieldWidth(spec)
	offset := int(spec.Register-base) * 2
	need := offset + int(width*2)
	if need > len(data) {
		return 0, errors.New("unexpected register payload length")
	}

	scale := spec.Scale
	if scale == 0 {
		scale = 10.0
	}
	ft := spec.Type
	if ft == "" {
		ft = Int16
	}

	switch ft {
	case Int16:
		b := data[offset : offset+2]
		if spec.ByteSwap {
			b = []byte{b[1], b[0]}
		}
		raw := int16(binary.BigEndian.Uint16(b))
		return float64(raw) / scale, nil
	case Uint16:
		b := data[offset : offset+2]
		if spec.ByteSwap {
			b = []byte{b[1], b[0]}
		}
		raw := binary.BigEndian.Uint16(b)
		return float64(raw) / scale, nil
	case Float32:
		b := make([]byte, 4)
		copy(b, data[offset:offset+4])
		if spec.WordSwap {
			b = []byte{b[2], b[3], b[0], b[1]}
		}
		if spec.ByteSwap {
			b = []byte{b[1], b[0], b[3], b[2]}
		}
		bits := binary.BigEndian.Uint32(b)
		f := math.Float32frombits(bits)
		return float64(f) / scale, nil
	default:
		return 0, errors.New("unsupported field type")
	}
}

func fieldWidth(spec FieldSpec) uint16 {
	switch spec.Type {
	case Float32:
		return 2
	default:
		return 1
	}
}
