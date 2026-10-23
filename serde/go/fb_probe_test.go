package serdebench

import (
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/proto"

	tickpb "redhow/researches/serde/go/tickpb"
)

// Одноразовые замеры для статьи: protobuf на 100k тиков (bigdoc-масштаб)
// и сжатие конкатенации FB-сообщений. Не бенч — источник чисел.
func TestProbeBigNumbers(t *testing.T) {
	big := make([]fbTick, 100_000)
	src := GenTicksRange(0, 100_000)
	for i, s := range src {
		big[i] = fbTick{Symbol: s.Symbol, Venue: s.Venue, Price: s.Price,
			Qty: s.Qty, TsMs: s.TsMs, Side: s.Side, OrderID: s.OrderID, Flags: s.Flags}
	}
	pbTicks := make([]*tickpb.Tick, len(big))
	for i, t := range big {
		pbTicks[i] = &tickpb.Tick{Symbol: t.Symbol, Venue: t.Venue, Price: t.Price,
			Qty: t.Qty, TsMs: t.TsMs, Side: t.Side, OrderId: t.OrderID, Flags: t.Flags}
	}
	batch := &tickpb.Batch{Ticks: pbTicks}

	enc, _ := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	defer enc.Close()

	// protobuf 100k
	pbBytes, err := proto.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("protobuf 100k: wire=%d bytes", len(pbBytes))
	r := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			if err := proto.Unmarshal(pbBytes, &tickpb.Batch{}); err != nil {
				b.Fatal(err)
			}
		}
	})
	t.Logf("protobuf 100k read: %s (%.0f ns/op)", r.T, float64(r.T.Nanoseconds())/float64(r.N))
	w := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			if _, err := proto.Marshal(batch); err != nil {
				b.Fatal(err)
			}
		}
	})
	t.Logf("protobuf 100k write: %s", w.T)

	// FB: конкатенация 10k сообщений + zstd-3
	bldr := flatbuffers.NewBuilder(1024)
	var concat []byte
	for i := 0; i < 10_000; i++ {
		concat = append(concat, buildFBTick(bldr, fbTick{
			Symbol: big[i].Symbol, Venue: big[i].Venue, Price: big[i].Price,
			Qty: big[i].Qty, TsMs: big[i].TsMs, Side: big[i].Side,
			OrderID: big[i].OrderID, Flags: big[i].Flags,
		})...)
	}
	compressed := enc.EncodeAll(concat, nil)
	t.Logf("FB concat 10k messages: raw=%d bytes, zstd-3=%d bytes (%.2f×JSON-size ratio vs 1227558)",
		len(concat), len(compressed), float64(len(compressed))/1_227_558)
}
