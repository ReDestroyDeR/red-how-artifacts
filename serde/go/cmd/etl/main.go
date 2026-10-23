// ETL end-to-end, Go-зеркало rust/src/bin/etl.rs: эфемерный поток тиков,
// читать и писать по окнам. В таймер попадает только обработка: генерация
// окна (write-side стоимость) и подготовка (компрессия для zstd-read) — вне.
//
// Запуск: go run ./cmd/etl -windows 2500 [-approach all]
// (внутри tclaude-песочницы: env GOPATH="$HOME/go" ...)

package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
	"unsafe"

	"github.com/klauspost/compress/zstd"
	easyjson "github.com/mailru/easyjson"
	"google.golang.org/protobuf/proto"

	serdebench "redhow/researches/serde/go"
	tickpb "redhow/researches/serde/go/tickpb"
)

const winTicks = 32_768

// perfect — раскладка, зеркальная serdebench.tickWire (48 B, LE, без дыр).
type flatTick struct {
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

const flatSize = int(unsafe.Sizeof(flatTick{}))

func init() {
	if flatSize != 48 {
		panic("flatTick layout drifted")
	}
}

func castFlat(data []byte) []flatTick {
	p := unsafe.SliceData(data)
	return unsafe.Slice((*flatTick)(unsafe.Pointer(p)), len(data)/flatSize)
}


func main() {
	// 250 окон ≈ 1 GB NDJSON — рабочий дефолт: расхождение с 2500-оконным
	// прогоном < 3% на замеренных точках, короткие подходы шумят меньше.
	// 2500 окон — один демонстрационный прогон на деградацию.
	windows := flag.Uint64("windows", 250, "окон в потоке")
	approach := flag.String("approach", "all", "all | <имя подхода>")
	flag.Parse()

	type run struct {
		name string
		fn   func(w uint64) (timed time.Duration, fmtBytes int, ticks int)
	}
	// Каждый fn генерирует окно ВНЕ таймера и возвращает время только обработки.
	runs := []run{
		{"read/v1_decoder_stream", func(w uint64) (time.Duration, int, int) {
			window := serdebench.NDJSONWindow(w*winTicks, winTicks)
			t0 := time.Now()
			dec := json.NewDecoder(bytes.NewReader(window))
			n := 0
			for {
				// TickPlain: чистая рефлексия, без easyjson-методов.
				var t serdebench.TickPlain
				if err := dec.Decode(&t); err == io.EOF {
					break
				} else if err != nil {
					panic(err)
				}
				n++
			}
			return time.Since(t0), len(window), n
		}},
		{"read/easyjson_per_line", func(w uint64) (time.Duration, int, int) {
			window := serdebench.NDJSONWindow(w*winTicks, winTicks)
			t0 := time.Now()
			n := 0
			rest := window
			for len(rest) > 0 {
				end := bytes.IndexByte(rest, '\n')
				var t serdebench.Tick
				if err := easyjson.Unmarshal(rest[:end], &t); err != nil {
					panic(err)
				}
				rest = rest[end+1:]
				n++
			}
			return time.Since(t0), len(window), n
		}},
		{"read/perfect", func(w uint64) (time.Duration, int, int) {
			wire := serdebench.WireWindow(w*winTicks, winTicks)
			t0 := time.Now()
			ticks := castFlat(wire)
			// Слитый проход + валидация. Комбинированная ветка — ЛУЧШАЯ форма
			// для Go: компилятор не опускает bool->int в setcc, «безветвевой»
			// вариант b2u(...) даёт 37.5 GB/s против 55.4 (см. results).
			sum := uint64(0)
			valid := true
			for i := range ticks {
				t := &ticks[i]
				sum += t.Price
				if t.SymbolIdx >= 8 || t.Side > 1 || t.Flags >= 16 || t.TsMs <= 1_000_000_000_000 {
					valid = false
				}
			}
			if !valid {
				panic("invalid perfect window")
			}
			_ = sum
			return time.Since(t0), len(wire), len(ticks)
		}},
		{"read/proto", func(w uint64) (time.Duration, int, int) {
			batch := genBatchRange(w*winTicks, winTicks)
			protoBytes, err := proto.Marshal(batch)
			if err != nil {
				panic(err)
			}
			t0 := time.Now()
			out := &tickpb.Batch{}
			if err := proto.Unmarshal(protoBytes, out); err != nil {
				panic(err)
			}
			return time.Since(t0), len(protoBytes), len(out.Ticks)
		}},
		{"read/v1_zstd", func(w uint64) (time.Duration, int, int) {
			window := serdebench.NDJSONWindow(w*winTicks, winTicks)
			enc, _ := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
			compressed := enc.EncodeAll(window, nil)
			enc.Close()
			dec, _ := zstd.NewReader(nil)
			defer dec.Close()
			t0 := time.Now()
			raw, err := dec.DecodeAll(compressed, nil)
			if err != nil {
				panic(err)
			}
			d := json.NewDecoder(bytes.NewReader(raw))
			n := 0
			for {
				var t serdebench.TickPlain
				if err := d.Decode(&t); err == io.EOF {
					break
				} else if err != nil {
					panic(err)
				}
				n++
			}
			return time.Since(t0), len(window), n
		}},
		{"write/v1_ndjson", func(w uint64) (time.Duration, int, int) {
			// Чистая рефлексия: TickPlain без easyjson-методов.
			ticks := serdebench.GenTicksPlainRange(w*winTicks, winTicks)
			buf := make([]byte, 0, winTicks*128)
			t0 := time.Now()
			for i := range ticks {
				line, err := json.Marshal(&ticks[i])
				if err != nil {
					panic(err)
				}
				buf = append(buf, line...)
				buf = append(buf, '\n')
			}
			return time.Since(t0), len(buf), len(ticks)
		}},
		{"write/easyjson_ndjson", func(w uint64) (time.Duration, int, int) {
			ticks := serdebench.GenTicksRange(w*winTicks, winTicks)
			buf := make([]byte, 0, winTicks*128)
			t0 := time.Now()
			for i := range ticks {
				line, err := easyjson.Marshal(&ticks[i])
				if err != nil {
					panic(err)
				}
				buf = append(buf, line...)
				buf = append(buf, '\n')
			}
			return time.Since(t0), len(buf), len(ticks)
		}},
		{"write/perfect", func(w uint64) (time.Duration, int, int) {
			ticks := serdebench.GenTicksRange(w*winTicks, winTicks)
			buf := make([]byte, 0, winTicks*flatSize)
			t0 := time.Now()
			for i := range ticks {
				t := &ticks[i]
				var f flatTick
				f.TsMs = t.TsMs
				f.OrderID = t.OrderID
				f.Price = t.Price
				f.Qty = t.Qty
				f.SymbolIdx = symbolIndexOf(t.Symbol)
				f.Flags = t.Flags
				f.Side = sideCodeOf(t.Side)
				buf = binary.LittleEndian.AppendUint64(buf, uint64(f.TsMs))
				buf = binary.LittleEndian.AppendUint64(buf, f.OrderID)
				buf = binary.LittleEndian.AppendUint64(buf, f.Price)
				buf = binary.LittleEndian.AppendUint64(buf, f.Qty)
				buf = binary.LittleEndian.AppendUint32(buf, f.SymbolIdx)
				buf = binary.LittleEndian.AppendUint32(buf, f.VenueIdx)
				buf = binary.LittleEndian.AppendUint32(buf, f.Flags)
				buf = append(buf, f.Side, 0, 0, 0)
			}
			return time.Since(t0), len(buf), len(ticks)
		}},
		{"write/proto", func(w uint64) (time.Duration, int, int) {
			batch := genBatchRange(w*winTicks, winTicks)
			t0 := time.Now()
			out, err := proto.Marshal(batch)
			if err != nil {
				panic(err)
			}
			return time.Since(t0), len(out), len(batch.Ticks)
		}},
	}

	selected := runs
	if *approach != "all" {
		selected = nil
		for _, r := range runs {
			if r.name == *approach {
				selected = []run{r}
			}
		}
		if selected == nil {
			fmt.Fprintf(os.Stderr, "unknown approach %q\n", *approach)
			os.Exit(2)
		}
	}

	ndjsonGB := float64(*windows*winTicks) * 128 / (1 << 30)
	fmt.Printf("поток: %d окон × %d тиков ≈ %.1f GB NDJSON-эквивалента\n\n", *windows, winTicks, ndjsonGB)
	fmt.Println("| подход | время обработки | формат, GB | MB/s формата | тиков/с |")
	fmt.Println("|---|---:|---:|---:|---:|")

	for _, r := range selected {
		r.fn(0) // прогрев генератора/аллокатора
		var timed time.Duration
		var fmtBytes int
		var ticks int
		for w := uint64(0); w < *windows; w++ {
			t, b, n := r.fn(w)
			timed += t
			fmtBytes += b
			ticks += n
		}
		fmt.Printf("| %s | %s | %.2f | %.0f | %.1fM |\n",
			r.name, timed.Round(time.Millisecond),
			float64(fmtBytes)/(1<<30),
			float64(fmtBytes)/(1<<20)/timed.Seconds(),
			float64(ticks)/1e6/timed.Seconds())
	}
}

func genBatchRange(from, n uint64) *tickpb.Batch {
	ticks := serdebench.GenTicksRange(from, n)
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

func symbolIndexOf(s string) uint32 {
	for i, sym := range serdebench.Symbols() {
		if sym == s {
			return uint32(i)
		}
	}
	panic("unknown symbol " + s)
}

func sideCodeOf(s string) uint8 {
	if s == "B" {
		return 0
	}
	return 1
}
