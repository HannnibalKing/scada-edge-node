package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"vanguardedge/pkg/localdb"
	"vanguardedge/pkg/modbusadapter"
	"vanguardedge/pkg/model"
	"vanguardedge/pkg/safetycontroller"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := log.Default()

	pressure := findRegister("Reactor_Inlet_Pressure")
	temperature := findRegister("Reactor_Temperature")
	flow := findRegister("Feed_Flow_Rate")

	adapterCfg := modbusadapter.AdapterConfig{
		Address:      "192.168.10.25:502", // Schneider / mixed-endian device
		UnitID:       1,                   // Modbus slave ID
		PollInterval: 200 * time.Millisecond,
		DialTimeout:  500 * time.Millisecond,
		BackoffMin:   100 * time.Millisecond,
		BackoffMax:   2 * time.Second,
		Registers: []modbusadapter.RegisterPoll{
			{
				Pressure:    toFieldSpec(pressure),
				Temperature: toFieldSpec(temperature),
				Flow:        toFieldSpec(flow),
			},
		},
		ChannelBuffer: 512,
	}

	adapter := modbusadapter.NewAdapter(adapterCfg)
	adapter.Start(ctx)

	ctrlCfg := safetycontroller.ControllerConfig{
		Limits: model.Limits{
			MaxPressurePSI:  pressure.Safety.High,
			MaxTemperatureC: temperature.Safety.High,
			MaxFlowLpm:      flow.Safety.High,
		},
		EmergencyRegister: 100, // coil 00101 (discrete output for pump emergency stop)
		EventSink:         localdb.NewFileEventSink("data/shutdown_events.jsonl"),
		Logger:            logger,
	}

	controller := safetycontroller.NewSafetyController(ctrlCfg, adapter)
	controller.Start(ctx, adapter.Out())

	pump := findRegister("Pump_Run_Status")
	go monitorPump(ctx, adapter, pump, logger)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs

	logger.Println("shutting down")
	controller.Stop()
	adapter.Stop()
}

func findRegister(name string) Register {
	for _, r := range Registers {
		if r.Name == name {
			return r
		}
	}
	return Register{}
}

func monitorPump(ctx context.Context, adapter *modbusadapter.Adapter, reg Register, logger *log.Logger) {
	if reg.Name == "" {
		return
	}
	interval := time.Duration(reg.ScanRateMs) * time.Millisecond
	if interval == 0 {
		interval = 200 * time.Millisecond
	}

	var last *bool

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		val, err := adapter.ReadCoil(ctx, reg.Address)
		if err != nil {
			logger.Printf("pump status read failed: %v", err)
			time.Sleep(interval)
			continue
		}

		if last == nil || *last != val {
			logger.Printf("pump status changed: %t", val)
			last = &val
		}

		time.Sleep(interval)
	}
}

func toFieldSpec(r Register) modbusadapter.FieldSpec {
	scaleDen := 1.0
	if r.Scale != 0 {
		scaleDen = 1.0 / r.Scale // adapter divides by Scale, so invert multiplier
	}

	ft := modbusadapter.FieldType("")
	switch r.DataType {
	case Uint16:
		ft = modbusadapter.Uint16
	case Int16:
		ft = modbusadapter.Int16
	case Float32:
		ft = modbusadapter.Float32
	default:
		ft = modbusadapter.Int16
	}

	return modbusadapter.FieldSpec{
		Register: r.Address,
		Type:     ft,
		Scale:    scaleDen,
		ByteSwap: r.ByteSwap,
		WordSwap: r.WordSwap,
	}
}
