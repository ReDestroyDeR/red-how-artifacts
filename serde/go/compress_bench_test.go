package serdebench

import (
	"encoding/json"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// Ось «сжатие» — зеркальна Rust compression.rs: compress на уровнях 1/3/9,
// полный конвейер decompress + parse против чистого parse.

func BenchmarkCompression(b *testing.B) {
	data := PayloadJSON(N)

	level := map[zstd.EncoderLevel]struct {
		name string
	}{
		zstd.SpeedFastest:           {"zstd_json/compress_1"},
		zstd.SpeedDefault:           {"zstd_json/compress_3"},
		zstd.SpeedBetterCompression: {"zstd_json/compress_9"},
	}
	for lv, spec := range level {
		enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(lv))
		if err != nil {
			b.Fatal(err)
		}
		var compressed []byte
		b.Run(spec.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				compressed = enc.EncodeAll(data, nil)
			}
		})
		enc.Close()
		_ = compressed
	}

	// Полный конвейер «сжатый транспорт»: decompress + наивный parse.
	enc, _ := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	compressed3 := enc.EncodeAll(data, nil)
	enc.Close()

	dec, _ := zstd.NewReader(nil)
	b.Run("zstd_json/decompress_3", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		for b.Loop() {
			_, err := dec.DecodeAll(compressed3, nil)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("zstd_json/decode_plus_parse_3", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		for b.Loop() {
			raw, err := dec.DecodeAll(compressed3, nil)
			if err != nil {
				b.Fatal(err)
			}
			var out []TickPlain
			if err := json.Unmarshal(raw, &out); err != nil {
				b.Fatal(err)
			}
			_ = out
		}
	})
	dec.Close()
}
