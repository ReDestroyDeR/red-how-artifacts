//! Кейс «ETL»: окно потока 32 768 тиков ≈ 4 MB NDJSON.
//! Чтение: typed-стрим, per-line SIMD, zerocopy, proto, zstd-конвейер.
//! Запись: то же в обратную сторону. Полный 10 GB прогон — bin/etl,
//! здесь микробенч одного окна для статистики criterion.

use std::hint::black_box;

use criterion::{criterion_group, criterion_main, BenchmarkId, Criterion, Throughput};
use prost::Message as _;
use serde_bench::*;
use zerocopy::IntoBytes as _;

/// Тиков в окне: 32 768 × ~128 B ≈ 4.2 MB NDJSON.
pub const WIN_TICKS: usize = 32_768;

fn bench_etl(c: &mut Criterion) {
    let window = ndjson_window(0, WIN_TICKS);
    let ticks = gen_ticks_range(0, WIN_TICKS);
    let archived = rkyv_bytes(&ticks);
    let table = symbol_table();
    let wire = encode_wire(&ticks, &table);
    let batch = gen_batch_msg(WIN_TICKS);
    let proto = batch.encode_to_vec();
    let zstd_window = zstd::bulk::compress(&window, 3).unwrap();

    let mut g = c.benchmark_group("etl");
    g.throughput(Throughput::Bytes(window.len() as u64));

    // Typed-стрим: serde_json идёт по конкатенированным значениям без DOM.
    g.bench_function(BenchmarkId::new("read/serde_stream", WIN_TICKS), |b| {
        b.iter(|| {
            let n = serde_json::Deserializer::from_slice(black_box(&window))
                .into_iter::<Tick>()
                .filter_map(|r| r.ok())
                .count();
            black_box(n)
        })
    });
    // Per-line SIMD: копия окна включена (simd-json требует &mut) — цена подхода.
    g.bench_function(BenchmarkId::new("read/simd_lines", WIN_TICKS), |b| {
        b.iter(|| {
            let mut buf = window.clone();
            let mut n = 0usize;
            for line in buf.split_inclusive_mut(|c| *c == b'\n') {
                let end = line.len() - if line.last() == Some(&b'\n') { 1 } else { 0 };
                let t: Tick = simd_json::from_slice(&mut line[..end]).unwrap();
                black_box(&t);
                n += 1;
            }
            black_box(n)
        })
    });
    g.throughput(Throughput::Bytes(wire.len() as u64));
    // zerocopy: окно perfect — один слитый проход, безветвевая валидация
    // (лучшая форма для LLVM: setcc + векторизация; зеркало Go — ветвящаяся).
    g.bench_function(BenchmarkId::new("read/flat48_cast", WIN_TICKS), |b| {
        b.iter(|| {
            let w = cast_wire(black_box(&wire)).unwrap();
            let mut sum = 0u64;
            let mut bad = 0u64;
            for t in w {
                sum += t.price;
                bad |= u64::from(t.symbol_idx as u64 >= SYMBOLS.len() as u64)
                    | u64::from(t.side > 1)
                    | u64::from(t.flags >= 16)
                    | u64::from(t.ts_ms <= 1_000_000_000_000);
            }
            black_box(bad == 0);
            black_box(sum)
        })
    });
    g.throughput(Throughput::Bytes(archived.len() as u64));
    g.bench_function(BenchmarkId::new("read/rkyv_access", WIN_TICKS), |b| {
        b.iter(|| {
            let a: &ArchivedTicks =
                rkyv::access::<ArchivedTicks, RkyvError>(black_box(&archived)).unwrap();
            let sum: u64 = a.iter().map(|t| t.price.to_native()).sum();
            black_box(sum)
        })
    });
    g.throughput(Throughput::Bytes(proto.len() as u64));
    g.bench_function(BenchmarkId::new("read/proto_decode", WIN_TICKS), |b| {
        b.iter(|| black_box(BatchMsg::decode(black_box(proto.as_slice())).unwrap()))
    });
    g.throughput(Throughput::Bytes(window.len() as u64));
    // Сжатый транспорт: decompress + typed-стрим. Отдельно в gen — цена сжатия.
    g.bench_function(BenchmarkId::new("read/serde_zstd", WIN_TICKS), |b| {
        b.iter(|| {
            let raw =
                zstd::bulk::decompress(black_box(&zstd_window), window.len()).unwrap();
            let n = serde_json::Deserializer::from_slice(black_box(&raw))
                .into_iter::<Tick>()
                .filter_map(|r| r.ok())
                .count();
            black_box(n)
        })
    });

    // Запись: поток тиков -> байты окна, буфер переиспользуется.
    // Тики «пришли» из источника (предгенерированы) — в таймере только
    // сериализация, зеркально Go write/*-бенчам.
    g.bench_function(BenchmarkId::new("write/serde_ndjson", WIN_TICKS), |b| {
        b.iter(|| {
            let mut buf = Vec::with_capacity(window.len());
            for t in &ticks {
                serde_json::to_writer(&mut buf, t).unwrap();
                buf.push(b'\n');
            }
            black_box(buf)
        })
    });
    g.throughput(Throughput::Bytes(wire.len() as u64));
    g.bench_function(BenchmarkId::new("write/perfect", WIN_TICKS), |b| {
        b.iter(|| {
            let mut buf = Vec::with_capacity(wire.len());
            for t in &ticks {
                let w = TickWire {
                    ts_ms: t.ts_ms,
                    order_id: t.order_id,
                    price: t.price,
                    qty: t.qty,
                    symbol_idx: symbol_index(&t.symbol) as u32,
                    venue_idx: 0,
                    flags: t.flags,
                    side: if t.side == "B" { 0 } else { 1 },
                    _pad: [0; 3],
                };
                buf.extend_from_slice(w.as_bytes());
            }
            black_box(buf)
        })
    });
    g.throughput(Throughput::Bytes(archived.len() as u64));
    g.bench_function(BenchmarkId::new("write/rkyv", WIN_TICKS), |b| {
        b.iter(|| black_box(rkyv_bytes(black_box(&ticks))))
    });
    g.throughput(Throughput::Bytes(proto.len() as u64));
    g.bench_function(BenchmarkId::new("write/proto", WIN_TICKS), |b| {
        b.iter(|| black_box(black_box(&batch).encode_to_vec()))
    });

    g.finish();
}

criterion_group!(benches, bench_etl);
criterion_main!(benches);
