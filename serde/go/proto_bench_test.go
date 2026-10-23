package serdebench

import (
	"testing"

	"google.golang.org/protobuf/proto"

	tickpb "redhow/researches/serde/go/tickpb"
)

// Ось «protobuf: наивные encode/decode» — та же, что prost в Rust.

func genBatch(n int) *tickpb.Batch {
	ticks := GenTicks(n)
	msgs := make([]*tickpb.Tick, n)
	for i, t := range ticks {
		msgs[i] = &tickpb.Tick{
			Symbol:  t.Symbol,
			Venue:   t.Venue,
			Price:   t.Price,
			Qty:     t.Qty,
			TsMs:    t.TsMs,
			Side:    t.Side,
			OrderId: t.OrderID,
			Flags:   t.Flags,
		}
	}
	return &tickpb.Batch{Ticks: msgs}
}

func BenchmarkProto(b *testing.B) {
	batch := genBatch(N)
	encoded, err := proto.Marshal(batch)
	if err != nil {
		b.Fatal(err)
	}

	b.Run("proto/marshal", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(encoded)))
		for b.Loop() {
			out, err := proto.Marshal(batch)
			if err != nil {
				b.Fatal(err)
			}
			_ = out
		}
	})
	// Полная материализация: каждый тик — отдельная аллокация.
	b.Run("proto/unmarshal_full", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(encoded)))
		for b.Loop() {
			out := &tickpb.Batch{}
			if err := proto.Unmarshal(encoded, out); err != nil {
				b.Fatal(err)
			}
			_ = out
		}
	})
}
