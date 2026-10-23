package serdebench

import (
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"

	fbgen "redhow/researches/serde/go/serdebench"
)

// FlatBuffers (библиотека, переносимость) против perfect (своя раскладка).
// Код сгенерирован flatc из schema/tick.fbs (serdebench/Tick.go) —
// тот же safe-API, что в проде: никаких ручных unsafe.

type fbTick struct {
	Symbol  string
	Venue   string
	Price   uint64
	Qty     uint64
	TsMs    int64
	Side    string
	OrderID uint64
	Flags   uint32
}

// buildFBTick — запись через сгенерированные хелперы.
func buildFBTick(b *flatbuffers.Builder, t fbTick) []byte {
	b.Reset()
	sym := b.CreateString(t.Symbol)
	venue := b.CreateString(t.Venue)
	side := b.CreateString(t.Side)
	fbgen.TickStart(b)
	// порядок Add зеркален Rust Tick::create (сортировка по выравниванию) —
	// так буферы Go и Rust байт-идентичны
	fbgen.TickAddOrderId(b, t.OrderID)
	fbgen.TickAddTsMs(b, t.TsMs)
	fbgen.TickAddQty(b, t.Qty)
	fbgen.TickAddPrice(b, t.Price)
	fbgen.TickAddFlags(b, t.Flags)
	fbgen.TickAddSide(b, side)
	fbgen.TickAddVenue(b, venue)
	fbgen.TickAddSymbol(b, sym)
	end := fbgen.TickEnd(b)
	b.Finish(end)
	return b.FinishedBytes()
}

// readFBTick — чтение через сгенерированные акцессоры: представления []byte
// поверх буфера без конверсии в string (так библиотеку используют в проде).
func readFBTick(buf []byte) fbTick {
	tick := fbgen.GetRootAsTick(buf, 0)
	out := fbTick{
		Price:   tick.Price(),
		Qty:     tick.Qty(),
		TsMs:    tick.TsMs(),
		OrderID: tick.OrderId(),
		Flags:   tick.Flags(),
	}
	if s := tick.Symbol(); s != nil {
		out.Symbol = string(s)
	}
	if v := tick.Venue(); v != nil {
		out.Venue = string(v)
	}
	if sd := tick.Side(); sd != nil {
		out.Side = string(sd)
	}
	return out
}

// TestFBRoundTrip — корректность: прочитанное обязано совпасть с записанным,
// иначе бенч мерит мусор.
func TestFBRoundTrip(t *testing.T) {
	want := fbTick{
		Symbol: "SBER", Venue: "MOEX", Price: 277_350, Qty: 15,
		TsMs: 1_729_600_000_123, Side: "B", OrderID: 882_374, Flags: 3,
	}
	b := flatbuffers.NewBuilder(1024)
	buf := buildFBTick(b, want)
	got := readFBTick(buf)
	if got != want {
		t.Fatalf("round-trip разошёлся:\nwant %+v\ngot  %+v", want, got)
	}
	t.Logf("flatbuffers tick buffer: %d bytes\nhex: %x", len(buf), buf)
}

func BenchmarkFlatBuffers(b *testing.B) {
	want := fbTick{
		Symbol: "SBER", Venue: "MOEX", Price: 277_350, Qty: 15,
		TsMs: 1_729_600_000_123, Side: "B", OrderID: 882_374, Flags: 3,
	}
	bldr := flatbuffers.NewBuilder(1024)
	buf := buildFBTick(bldr, want)

	b.Run("fb/read_tick", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			t := readFBTick(buf)
			_ = t.Price
		}
	})
	b.Run("fb/write_tick", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = buildFBTick(bldr, want)
		}
	})

	// bigdoc: 100 000 тиков одним документом (кейс bigdoc).
	bigTicks := GenTicksRange(0, 100_000)
	fbBig := make([]fbTick, len(bigTicks))
	for i, t := range bigTicks {
		fbBig[i] = fbTick{Symbol: t.Symbol, Venue: t.Venue, Price: t.Price,
			Qty: t.Qty, TsMs: t.TsMs, Side: t.Side, OrderID: t.OrderID, Flags: t.Flags}
	}
	bigDoc := buildFBBigDoc(bldr, fbBig)

	b.Run("fb/read_bigdoc", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(bigDoc)))
		for b.Loop() {
			_ = readFBBigDoc(bigDoc)
		}
	})
	b.Run("fb/write_bigdoc", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(bigDoc)))
		for b.Loop() {
			_ = buildFBBigDoc(bldr, fbBig)
		}
	})
}

// buildFBBigDoc — 100 000 тиков одним документом: вектор таблиц под общим
// корнем BigDoc. Таблицы создаются в прямом порядке, CreateVectorOfTables
// сам разворачивает.
func buildFBBigDoc(b *flatbuffers.Builder, ticks []fbTick) []byte {
	b.Reset()
	offsets := make([]flatbuffers.UOffsetT, len(ticks))
	for i := range ticks {
		t := ticks[i]
		sym := b.CreateString(t.Symbol)
		venue := b.CreateString(t.Venue)
		side := b.CreateString(t.Side)
		fbgen.TickStart(b)
		fbgen.TickAddOrderId(b, t.OrderID)
		fbgen.TickAddTsMs(b, t.TsMs)
		fbgen.TickAddQty(b, t.Qty)
		fbgen.TickAddPrice(b, t.Price)
		fbgen.TickAddFlags(b, t.Flags)
		fbgen.TickAddSide(b, side)
		fbgen.TickAddVenue(b, venue)
		fbgen.TickAddSymbol(b, sym)
		offsets[i] = fbgen.TickEnd(b)
	}
	vec := b.CreateVectorOfTables(offsets)
	fbgen.BigDocStart(b)
	fbgen.BigDocAddTicks(b, vec)
	doc := fbgen.BigDocEnd(b)
	b.Finish(doc)
	return b.FinishedBytes()
}

// readFBBigDoc — итерация вектора: все поля каждого тика.
func readFBBigDoc(buf []byte) uint64 {
	doc := fbgen.GetRootAsBigDoc(buf, 0)
	n := doc.TicksLength()
	sum := uint64(0)
	var t fbgen.Tick
	for i := 0; i < n; i++ {
		doc.Ticks(&t, i)
		sum += t.Price()
		if s := t.Symbol(); len(s) == 0 {
			panic("empty symbol")
		}
	}
	return sum
}
