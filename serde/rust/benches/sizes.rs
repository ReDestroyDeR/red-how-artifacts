//! Кейс «CRUD/WS»: лестница размеров запроса и ответа.
//! 1 / 4 / 8 / 16 / 80 тиков ≈ 120 B / 500 B / 1 KB / 2 KB / 10 KB.
//! На малых размерах выигрывает не скорость алгоритма, а отсутствие
//! fixed overhead — это и есть измеряемая гипотеза.
//!
//! Группа bigdoc (~12 MB одним документом) — точка, где SIMD-парсер
//! разворачивается: в CRUD-лестнице он проигрывает setup'ом.

use std::hint::black_box;

use criterion::{criterion_group, criterion_main, BenchmarkId, Criterion, Throughput};
use prost::Message as _;
use serde_bench::*;

/// Пять точек лестницы: тиков на батч.
const SIZES: [usize; 5] = [1, 4, 8, 16, 80];
/// Один большой документ: 100 000 тиков ≈ 12 MB JSON.
const BIGDOC: usize = 100_000;

fn bench_sizes(c: &mut Criterion) {
    let mut g = c.benchmark_group("crud");
    for n in SIZES {
        let payload = payload_json(n);
        let bytes = payload.as_bytes();
        let ticks = gen_ticks(n);
        let archived = rkyv_bytes(&ticks);
        let table = symbol_table();
        let wire = encode_wire(&ticks, &table);
        let batch = gen_batch_msg(n);
        let proto = batch.encode_to_vec();

        g.throughput(Throughput::Bytes(bytes.len() as u64));
        g.bench_function(BenchmarkId::new("json/parse_typed", bytes.len()), |b| {
            b.iter(|| {
                let t: Vec<Tick> = serde_json::from_slice(black_box(bytes)).unwrap();
                black_box(t)
            })
        });
        g.bench_function(BenchmarkId::new("json/write_typed", bytes.len()), |b| {
            b.iter(|| black_box(serde_json::to_vec(black_box(&ticks)).unwrap()))
        });
        g.bench_function(BenchmarkId::new("simd/parse_typed", bytes.len()), |b| {
            b.iter(|| {
                let mut s = payload.clone().into_bytes();
                let t: Vec<Tick> = simd_json::from_slice(&mut s).unwrap();
                black_box(t)
            })
        });
        g.bench_function(BenchmarkId::new("rkyv/access_validated", bytes.len()), |b| {
            b.iter(|| {
                let a: &ArchivedTicks =
                    rkyv::access::<ArchivedTicks, RkyvError>(black_box(&archived)).unwrap();
                black_box(a.len())
            })
        });
        g.bench_function(BenchmarkId::new("perfect/cast_validated", bytes.len()), |b| {
            b.iter(|| {
                let w = cast_wire(black_box(&wire)).unwrap();
                black_box(validate_wire(w))
            })
        });
        g.throughput(Throughput::Bytes(proto.len() as u64));
        g.bench_function(BenchmarkId::new("proto/decode_full", bytes.len()), |b| {
            b.iter(|| black_box(BatchMsg::decode(black_box(proto.as_slice())).unwrap()))
        });
        g.throughput(Throughput::Bytes(bytes.len() as u64));
    }
    g.finish();

    let mut g = c.benchmark_group("bigdoc");
    let payload = payload_json(BIGDOC);
    let bytes = payload.as_bytes();
    let ticks = gen_ticks(BIGDOC);
    let archived = rkyv_bytes(&ticks);
    let table = symbol_table();
    let wire = encode_wire(&ticks, &table);
    g.throughput(Throughput::Bytes(bytes.len() as u64));
    g.bench_function(BenchmarkId::new("json/parse_typed", bytes.len()), |b| {
        b.iter(|| {
            let t: Vec<Tick> = serde_json::from_slice(black_box(bytes)).unwrap();
            black_box(t)
        })
    });
    g.bench_function(BenchmarkId::new("json/parse_value_dom", bytes.len()), |b| {
        b.iter(|| {
            let v: serde_json::Value = serde_json::from_slice(black_box(bytes)).unwrap();
            black_box(v)
        })
    });
    g.bench_function(BenchmarkId::new("simd/parse_typed", bytes.len()), |b| {
        b.iter(|| {
            let mut s = payload.clone().into_bytes();
            let t: Vec<Tick> = simd_json::from_slice(&mut s).unwrap();
            black_box(t)
        })
    });
    g.bench_function(BenchmarkId::new("rkyv/access_validated", bytes.len()), |b| {
        b.iter(|| {
            let a: &ArchivedTicks =
                rkyv::access::<ArchivedTicks, RkyvError>(black_box(&archived)).unwrap();
            black_box(a.len())
        })
    });
    g.bench_function(BenchmarkId::new("perfect/cast_validated", bytes.len()), |b| {
        b.iter(|| {
            let w = cast_wire(black_box(&wire)).unwrap();
            black_box(validate_wire(w))
        })
    });
    g.finish();
}

criterion_group!(benches, bench_sizes);
criterion_main!(benches);
