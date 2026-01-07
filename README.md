# Vanguard-Edge (Real-Time Edge SCADA Node)

This project sketches a minimal real-time edge node for industrial safety. It contains:

- `modbus_adapter`: Polls Modbus TCP registers at high frequency and pushes normalized sensor data onto a buffered channel.
- `safety_controller`: Consumes the sensor channel, enforces hard safety thresholds, and triggers an emergency coil write with sub-5ms target latency. Also publishes shutdown events for durability.
- `cmd/node`: Example wiring that starts the adapter and controller.

> Note: The Modbus client uses the pure-Go `github.com/goburrow/modbus` library. Replace with a custom client if preferred.

## Quick start

```
go mod tidy

go run ./cmd/node
```

Configuration is centralized in `cmd/node/main.go` for now.
