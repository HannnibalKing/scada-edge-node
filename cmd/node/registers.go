package main

// RegType denotes the Modbus register type.
type RegType int

const (
	HoldingRegister RegType = iota
	InputRegister
	Coil
	DiscreteInput
)

// DataType denotes how the raw register value should be decoded.
type DataType int

const (
	Uint16 DataType = iota
	Int16
	Uint32
	Int32
	Float32
	Bool
)

// SafetyLimits captures optional edge-enforced bounds.
type SafetyLimits struct {
	High     float64
	HighHigh float64
	Low      float64
	LowLow   float64
}

// Register defines a single PLC tag for ingestion.
type Register struct {
	Name       string
	Address    uint16 // zero-based; e.g., 40001 -> 0, 40123 -> 122
	RegType    RegType
	DataType   DataType
	Scale      float64
	Offset     float64
	Units      string
	ByteSwap   bool
	WordSwap   bool
	ScanRateMs int
	Safety     SafetyLimits
}

// Registers defines all PLC data points ingested by the Edge node.
// Production example: Schneider / mixed-endian Modbus TCP device
var Registers = []Register{
	{
		Name:       "Inlet_Pressure",
		Address:    100, // 40101 zero-based (0–300 PSI, 0.1 PSI/count)
		RegType:    HoldingRegister,
		DataType:   Uint16,
		Scale:      0.1,
		Offset:     0.0,
		Units:      "PSI",
		ByteSwap:   false,
		WordSwap:   false,
		ScanRateMs: 200,
		Safety: SafetyLimits{
			High:     250.0,
			HighHigh: 275.0,
			Low:      10.0,
		},
	},

	{
		Name:       "Feed_Flow",
		Address:    102, // 40103 zero-based; consumes 40103–40104 (IEEE 754, word-swapped)
		RegType:    HoldingRegister,
		DataType:   Float32,
		Scale:      1.0,
		Offset:     0.0,
		Units:      "GPM",
		ByteSwap:   false,
		WordSwap:   true, // Schneider / mixed-endian style
		ScanRateMs: 250,
		Safety: SafetyLimits{
			High: 120.0,
			Low:  5.0,
		},
	},

	{
		Name:       "Reactor_Temperature",
		Address:    104, // 40105 zero-based (signed, 0.01 °C/count)
		RegType:    HoldingRegister,
		DataType:   Int16,
		Scale:      0.01,
		Offset:     0.0,
		Units:      "C",
		ByteSwap:   false,
		WordSwap:   false,
		ScanRateMs: 500,
		Safety: SafetyLimits{
			High: 180.0,
			Low:  40.0,
		},
	},

	{
		Name:       "Pump_Run",
		Address:    16, // coil 00017 zero-based (discrete run status)
		RegType:    Coil,
		DataType:   Bool,
		Scale:      1.0,
		Offset:     0.0,
		Units:      "",
		ByteSwap:   false,
		WordSwap:   false,
		ScanRateMs: 100,
	},
}
