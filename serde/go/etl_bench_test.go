package serdebench

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	"github.com/klauspost/compress/zstd"
	easyjson "github.com/mailru/easyjson"
	"google.golang.org/protobuf/proto"

	tickpb "redhow/researches/serde/go/tickpb"
)

// Кейс «ETL»: микробенч окна 32 768 тиков ≈ 4.2 MB NDJSON — зеркало
// Rust benches/etl.rs. Полный 10 GB прогон — cmd/etl.

const winTicks = 32_768

func BenchmarkEtlWindow(b *testing.B) {
	window := NDJSONWindow(0, winTicks)
	ticks := GenTicksRange(0, winTicks)
	// Чистая рефлексия для v1-путей: TickPlain без easyjson-методов,
	// иначе encoding/json зовёт UnmarshalJSON/MarshalJSON (см. tick.go).
	plain := GenTicksPlainRange(0, winTicks)
	wire := WireWindow(0, winTicks)
	batch := genBatchRange(0, winTicks)
	protoBytes, err := proto.Marshal(batch)
	if err != nil {
		b.Fatal(err)
	}
	enc, _ := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	zstdWindow := enc.EncodeAll(window, nil)
	enc.Close()

	// read/typed-stream: v1 Decoder ест конкатенированные значения.
	b.Run("read/v1_decoder_stream", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(window)))
		for b.Loop() {
			dec := json.NewDecoder(bytes.NewReader(window))
			n := 0
			for {
				var t TickPlain
				if err := dec.Decode(&t); err == io.EOF {
					break
				} else if err != nil {
					b.Fatal(err)
				}
				n++
			}
			_ = n
		}
	})
	// read/per-line easyjson: codegen на каждой 128-байтной строке —
	// точка сравнения «setup против скорости алгоритма».
	b.Run("read/easyjson_per_line", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(window)))
		for b.Loop() {
			n := 0
			rest := window
			for len(rest) > 0 {
				end := bytes.IndexByte(rest, '\n')
				var t Tick
				if err := easyjson.Unmarshal(rest[:end], &t); err != nil {
					b.Fatal(err)
				}
				rest = rest[end+1:]
				n++
			}
			_ = n
		}
	})
	// read/perfect: один слитый проход — сумма + валидация
	// (два прохода = два замера разных работ; зеркало Rust-версии).
	b.Run("read/perfect_cast", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(wire)))
		for b.Loop() {
			w := castTicks(wire)
			sum := uint64(0)
			valid := true
			for i := range w {
				t := &w[i]
				sum += t.Price
				if t.SymbolIdx >= uint32(len(symbols)) || t.Side > 1 || t.Flags >= 16 || t.TsMs <= 1_000_000_000_000 {
					valid = false
				}
			}
			if !valid {
				b.Fatal("invalid")
			}
			_ = sum
		}
	})
	// read/proto: полная материализация батча.
	b.Run("read/proto_decode", func(b *testing.B) {
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
	// read/zstd-конвейер: decompress + typed-stream.
	dec, _ := zstd.NewReader(nil)
	b.Run("read/v1_zstd", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(window)))
		for b.Loop() {
			raw, err := dec.DecodeAll(zstdWindow, nil)
			if err != nil {
				b.Fatal(err)
					}
			d := json.NewDecoder(bytes.NewReader(raw))
			n := 0
			for {
				var t TickPlain
				if err := d.Decode(&t); err == io.EOF {
					break
				} else if err != nil {
					b.Fatal(err)
				}
				n++
			}
			_ = n
		}
	})
	dec.Close()

	// write: тики «пришли», таймерует сериализация.
	b.Run("write/v1_ndjson", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(window)))
		for b.Loop() {
			buf := make([]byte, 0, len(window))
			for i := range plain {
				line, err := json.Marshal(&plain[i])
				if err != nil {
					b.Fatal(err)
				}
				buf = append(buf, line...)
				buf = append(buf, '\n')
			}
			_ = buf
		}
	})
	b.Run("write/easyjson_ndjson", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(window)))
		for b.Loop() {
			buf := make([]byte, 0, len(window))
			for i := range ticks {
				line, err := easyjson.Marshal(&ticks[i])
				if err != nil {
					b.Fatal(err)
				}
				buf = append(buf, line...)
				buf = append(buf, '\n')
			}
			_ = buf
		}
	})
	b.Run("write/perfect", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(wire)))
		for b.Loop() {
			buf := make([]byte, 0, len(wire))
			for i := range ticks {
				buf = appendWireTick(buf, ticks[i])
			}
			_ = buf
		}
	})
	b.Run("write/proto_marshal", func(b *testing.B) {
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
}

// genBatchRange — protobuf-батч окна потока.
func genBatchRange(from, n uint64) *tickpb.Batch {
	ticks := GenTicksRange(from, n)
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
