# Vanguard-Edge Configuration

This guide explains how to customize Vanguard-Edge for your PLC and hardware.

## Quick Start

```bash
go run ./cmd/node
```

## Tag Configuration

All PLC data points are defined in [cmd/node/registers.go](cmd/node/registers.go).

### Register Structure

```go
Register{
  Name:       "Tag_Name",
  Address:    122,              // zero-based Modbus address (40123 -> 122)
  RegType:    HoldingRegister,  // or InputRegister, Coil, DiscreteInput
  DataType:   Uint16,           // or Int16, Float32, Uint32, Int32, Bool
  Scale:      0.1,              // applied as: value = raw / Scale
  Offset:     0.0,              // applied as: value = (raw / Scale) + Offset
  Units:      "PSI",
  ByteSwap:   false,            // byte order within register
  WordSwap:   true,             // word order (for float32)
  ScanRateMs: 200,              // polling interval
  Safety: SafetyLimits{
    High:     250.0,            // warning threshold
    HighHigh: 275.0,            // trip / hard alarm
    Low:      10.0,             // minimum safe value
  },
}
```

## Address Translation (Critical)

The adapter uses **zero-based** Modbus register addresses:

| Human-readable | Zero-based | Example |
|---|---|---|
| 40001 | 0 | pressure register |
| 40123 | 122 | reactor inlet pressure |
| 40125–40126 | 124 | flow (float32, occupies 2 regs) |
| 00017 (coil) | 16 | pump run status |

## Data Type & Encoding

| Type | Size | Endian | Comment |
|---|---|---|---|
| Uint16 | 1 reg | big-endian | unsigned, e.g., pressure, flow |
| Int16 | 1 reg | big-endian | signed, e.g., temperature, torque |
| Uint32 | 2 regs | big-endian | 32-bit unsigned |
| Int32 | 2 regs | big-endian | 32-bit signed |
| Float32 | 2 regs | big-endian | IEEE 754 (set WordSwap for swapped) |
| Bool | 1 coil | N/A | discrete input or coil |

## Swap Flags

Apply if values look wrong (but stable):

| Vendor | ByteSwap | WordSwap | Example |
|---|---|---|---|
| Allen-Bradley | ❌ | ❌ | standard big-endian |
| Siemens S7 | ❌ | ❌ | standard big-endian |
| Schneider | ❌ | ✅ | float32 word-swapped |
| Generic RTU | ✅ | ✅ | mixed byte/word order |

## Safety Limits

The edge node enforces hard thresholds. If a reading exceeds `High`, it logs an event. If it exceeds `HighHigh`, the safety controller triggers an emergency shutdown to the specified coil/register.

Update limits in [cmd/node/registers.go](cmd/node/registers.go):

```go
Safety: SafetyLimits{
  High:     250.0,  // log warning
  HighHigh: 275.0,  // trigger shutdown
  Low:      10.0,   // minimum acceptable
}
```

## Emergency Response

When a safety threshold is exceeded:

1. **Event Persisted**: Shutdown event logged to `data/shutdown_events.jsonl`
2. **Coil Write**: Emergency register (default: 2000) set to TRUE within 5 ms
3. **Controller Stops**: Graceful shutdown of monitoring loops

Adjust `EmergencyRegister` in [cmd/node/main.go](cmd/node/main.go):

```go
EmergencyRegister: 2000,  // e.g., coil 02001 (zero-based: 2000)
```

## Scaling Examples

### PSI Pressure (0.1 PSI/count)

```go
Scale: 0.1,      // raw 1234 → 123.4 PSI
```

### Temperature (0.5°C/count, offset +20)

```go
Scale:  0.5,
Offset: 20.0,    // raw 100 → 100/0.5 + 20 = 220°C
```

### Flow (no scaling, raw counts = GPM)

```go
Scale: 1.0,
```

## PLC Connection

Configure in [cmd/node/main.go](cmd/node/main.go):

```go
Address:     "192.168.1.100:502",  // PLC IP:port
UnitID:      1,                    // Modbus slave ID
PollInterval: 200 * time.Millisecond,
DialTimeout: 500 * time.Millisecond,
```

## Building & Running

### Build

```bash
go build -o ./bin/vanguard-edge ./cmd/node
```

### Run

```bash
./bin/vanguard-edge
```

### Test

```bash
go test ./...
```

## Event Log

Shutdown events are saved as JSONL to `data/shutdown_events.jsonl`:

```json
{"timestamp":"2026-01-05T12:34:56.789Z","reason":"safety threshold exceeded","sensor":{"timestamp":"...","register":122,"pressure_psi":275.5,"temperature_c":98.3,"flow_lpm":115.2}}
```

## Best Practices

✅ **Do**

- Scan at 100–500 ms for safety critical loops
- Timestamp at read, not post-process
- Batch contiguous registers (reduces round-trips)
- Validate scaling against PLC docs
- Test emergency shutdown before deployment

❌ **Don't**

- Scan faster than 100 ms unless proven necessary
- Apply safety logic downstream (do it at the edge)
- Trust UI/historian for safety decisions
- Ignore swap flags if values are wildly off

## Troubleshooting

### Values look wrong

1. Check address mapping (40001 → 0, etc.)
2. Try `ByteSwap: true` and/or `WordSwap: true`
3. Verify `Scale` matches PLC tag scaling
4. Compare raw register read vs. PLC HMI

### Connection drops

Increase `DialTimeout` and check network. Adapter will backoff and retry automatically.

### Shutdown triggered unexpectedly

1. Review `Safety.HighHigh` limits
2. Check sensor calibration
3. Verify register addresses
4. Inspect `data/shutdown_events.jsonl` for cause
