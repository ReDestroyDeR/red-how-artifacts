package serdebench

import (
	"encoding/binary"
	"testing"
)

// Ось «zerocopy» для Go: unsafe-каст фиксированных структур.
// tickWire, castTicks, validateWire, wireSize — в wire.go: общие с ETL
// и генератором эталонного потока.

func BenchmarkFlat48(b *testing.B) {
	ticks := GenTicks(N)

	// Эталон раскладки — WireWindow из wire.go: байт-в-байт совпадает с
	// rust wire_window (сверено sha256, см. sanity-прогон 2026-10-23).
	wire := make([]byte, 0, len(ticks)*wireSize)
	tmp := tickWire{}
	for _, t := range ticks {
		tmp = tickWire{
			TsMs:      t.TsMs,
			OrderID:   t.OrderID,
			Price:     t.Price,
			Qty:       t.Qty,
			SymbolIdx: symbolIndex(t.Symbol),
			VenueIdx:  0,
			Flags:     t.Flags,
			Side:      sideCode(t.Side),
		}
		wire = binary.LittleEndian.AppendUint64(wire, uint64(tmp.TsMs))
		wire = binary.LittleEndian.AppendUint64(wire, tmp.OrderID)
		wire = binary.LittleEndian.AppendUint64(wire, tmp.Price)
		wire = binary.LittleEndian.AppendUint64(wire, tmp.Qty)
		wire = binary.LittleEndian.AppendUint32(wire, tmp.SymbolIdx)
		wire = binary.LittleEndian.AppendUint32(wire, tmp.VenueIdx)
		wire = binary.LittleEndian.AppendUint32(wire, tmp.Flags)
		wire = append(wire, tmp.Side, 0, 0, 0)
	}

	b.Run("perfect/cast", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(wire)))
		for b.Loop() {
			_ = castTicks(wire)
		}
	})
	b.Run("perfect/cast_validated", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(wire)))
		for b.Loop() {
			if !validateWire(castTicks(wire)) {
				b.Fatal("invalid")
			}
		}
	})
	// Конверсия словарный int -> string без аллокаций: срез интернированных строк.
	strs := symbols[:]
	b.Run("perfect/symbol_lookup", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(wire)))
		for b.Loop() {
			total := 0
			for _, t := range castTicks(wire) {
				total += len(strs[t.SymbolIdx])
			}
			_ = total
		}
	})
	// Копирующий декод без unsafe: бинарные чтения по смещениям —
	// «zerocopy руками» против «zerocopy по-настоящему».
	b.Run("perfect/decode_manual_copy", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(wire)))
		out := make([]Tick, len(ticks))
		b.ResetTimer()
		for b.Loop() {
			for i := range out {
				off := i * wireSize
				out[i] = Tick{
					Venue:  "MOEX",
					TsMs:    int64(binary.LittleEndian.Uint64(wire[off:])),
					OrderID: binary.LittleEndian.Uint64(wire[off+8:]),
					Price:   binary.LittleEndian.Uint64(wire[off+16:]),
					Qty:     binary.LittleEndian.Uint64(wire[off+24:]),
					Symbol:  strs[binary.LittleEndian.Uint32(wire[off+32:])],
					Flags:   binary.LittleEndian.Uint32(wire[off+40:]),
					Side:    []string{"B", "S"}[wire[off+44]],
				}
			}
		}
	})
}
