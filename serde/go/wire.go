package serdebench

import (
	"encoding/binary"
	"encoding/json"
	"unsafe"

	easyjson "github.com/mailru/easyjson"
)

// Общие помощники бенчей и ETL: perfect-тип, кодирование, алиасы.

// tickWire — плоская 48-байтная запись, раскладка зеркальна Rust TickWire
// (LE, естественное выравнивание 8, без дыр). Живёт здесь, чтобы и бенчи,
// и генератор эталонного потока использовали один тип.
type tickWire struct {
	TsMs      int64
	OrderID   uint64
	Price     uint64
	Qty       uint64
	SymbolIdx uint32
	VenueIdx  uint32
	Flags     uint32
	Side      uint8
	_         [3]byte
}

const wireSize = int(unsafe.Sizeof(tickWire{}))

func init() {
	// Контракт с Rust-стороной: раскладка обязана совпадать.
	if wireSize != 48 {
		panic("tickWire layout drifted from Rust TickWire")
	}
}

// castTicks — голый каст. Предусловия zerocopy, которые легко забыть:
// длина кратна размеру записи, буфер выровнен на 8.
func castTicks(data []byte) []tickWire {
	if len(data)%wireSize != 0 {
		panic("misaligned payload length")
	}
	p := unsafe.SliceData(data)
	if uintptr(unsafe.Pointer(p))%8 != 0 {
		panic("payload buffer not 8-byte aligned")
	}
	return unsafe.Slice((*tickWire)(unsafe.Pointer(p)), len(data)/wireSize)
}

// validateWire — семантическая валидация поверх каста; зеркальна Rust-версии.
func validateWire(w []tickWire) bool {
	for _, t := range w {
		if t.SymbolIdx >= uint32(len(symbols)) || t.Side > 1 || t.Flags >= 16 || t.TsMs <= 1_000_000_000_000 {
			return false
		}
	}
	return true
}

func binaryAppendU64(buf []byte, v uint64) []byte {
	return binary.LittleEndian.AppendUint64(buf, v)
}

func binaryAppendU32(buf []byte, v uint32) []byte {
	return binary.LittleEndian.AppendUint32(buf, v)
}

// appendWireTick — perfect-запись из типизированного тика (словарь -> int).
func appendWireTick(buf []byte, t Tick) []byte {
	w := tickWire{
		TsMs:      t.TsMs,
		OrderID:   t.OrderID,
		Price:     t.Price,
		Qty:       t.Qty,
		SymbolIdx: symbolIndex(t.Symbol),
		VenueIdx:  0,
		Flags:     t.Flags,
		Side:      sideCode(t.Side),
	}
	buf = binaryAppendU64(buf, uint64(w.TsMs))
	buf = binaryAppendU64(buf, w.OrderID)
	buf = binaryAppendU64(buf, w.Price)
	buf = binaryAppendU64(buf, w.Qty)
	buf = binaryAppendU32(buf, w.SymbolIdx)
	buf = binaryAppendU32(buf, w.VenueIdx)
	buf = binaryAppendU32(buf, w.Flags)
	return append(buf, w.Side, 0, 0, 0)
}

// WireWindow — окно perfect [from, from+ticks) из эталонного генератора.
func WireWindow(from uint64, ticks int) []byte {
	buf := make([]byte, 0, ticks*wireSize)
	for i := from; i < from+uint64(ticks); i++ {
		buf = appendWireTick(buf, GenTick(i))
	}
	return buf
}

func easyjsonUnmarshal(data []byte, out *Ticks) error {
	return easyjson.Unmarshal(data, out)
}

// jsonPlainMarshal — v1-маршалинг без easyjson-методов (для эквивалентности TickPlain).
func jsonPlainMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}
