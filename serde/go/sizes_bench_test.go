package serdebench

import (
	"encoding/json"
	"testing"

	"google.golang.org/protobuf/proto"

	tickpb "redhow/researches/serde/go/tickpb"
)

// Кейс «CRUD/WS»: лестница размеров — зеркало Rust benches/sizes.rs.
// 1 / 4 / 8 / 16 / 80 тиков ≈ 120 B / 500 B / 1 KB / 2 KB / 10 KB.

var crudSizes = []int{1, 4, 8, 16, 80}

func BenchmarkCrud(b *testing.B) {
	for _, n := range crudSizes {
		data := PayloadJSON(n)
		// Чистая рефлексия: TickPlain без easyjson-методов.
		plain := GenTicksPlain(n)
		// Типизированные тики — для proto и perfect.
		ticks := GenTicks(n)
		batch := genBatch(n)
		protoBytes, err := proto.Marshal(batch)
		if err != nil {
			b.Fatal(err)
		}

		b.Run("json/parse_typed", func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				var out []TickPlain
				if err := json.Unmarshal(data, &out); err != nil {
					b.Fatal(err)
				}
				_ = out
			}
		})
		b.Run("json/write_typed", func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				out, err := json.Marshal(plain)
				if err != nil {
					b.Fatal(err)
				}
				_ = out
			}
		})
		b.Run("easyjson/parse_typed", func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				var out Ticks
				if err := easyjsonUnmarshal(data, &out); err != nil {
					b.Fatal(err)
				}
				_ = out
			}
		})
		b.Run("proto/decode_full", func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(protoBytes)))
			for b.Loop() {
				out := &tickpb.Batch{}
				if err := proto.Unmarshal(protoBytes, out); err != nil {
					b.Fatal(err)
				}
				_ = out
			}
		})
		b.Run("proto/marshal", func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(protoBytes)))
			for b.Loop() {
				out, err := proto.Marshal(batch)
				if err != nil {
					b.Fatal(err)
				}
				_ = out
			}
		})
		// zerocopy-точка лестницы: тот же perfect, 48 B/тик.
		wire := make([]byte, 0, len(ticks)*wireSize)
		for _, t := range ticks {
			wire = appendWireTick(wire, t)
		}
		b.Run("perfect/cast_validated", func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(wire)))
			for b.Loop() {
				if !validateWire(castTicks(wire)) {
					b.Fatal("invalid")
				}
			}
		})
	}
}
