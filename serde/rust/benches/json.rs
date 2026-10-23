//! Ось «JSON: наивная десериализация, DOM, SIMD».
//! simd-json мутирует входной буфер — копия пейлоада включена в измерение
//! и помечена в методологии как часть цены подхода.

use std::hint::black_box;

use criterion::{criterion_group, criterion_main, BenchmarkId, Criterion, Throughput};
use serde_bench::*;

fn bench_json(c: &mut Criterion) {
    let payload = payload_json(N);
    let bytes = payload.as_bytes();
    let ticks = gen_ticks(N);

    let mut g = c.benchmark_group("json");
    g.throughput(Throughput::Bytes(bytes.len() as u64));
    g.bench_with_input(
        BenchmarkId::new("serde/parse_typed", N),
        bytes,
        |b, bytes| {
            b.iter(|| {
                let t: Vec<Tick> = serde_json::from_slice(black_box(bytes)).unwrap();
                black_box(t)
            })
        },
    );
    // DOM: полный AST в памяти.
    g.bench_with_input(
        BenchmarkId::new("serde/parse_value_dom", N),
        bytes,
        |b, bytes| {
            b.iter(|| {
                let v: serde_json::Value = serde_json::from_slice(black_box(bytes)).unwrap();
                black_box(v)
            })
        },
    );
    g.bench_function(BenchmarkId::new("serde/write_typed", N), |b| {
        b.iter(|| black_box(serde_json::to_string(black_box(&ticks)).unwrap()))
    });
    // AST при сериализации: сначала в Value, потом из Value в строку.
    g.bench_function(BenchmarkId::new("serde/serialize_to_value_dom", N), |b| {
        b.iter(|| black_box(serde_json::to_value(black_box(&ticks)).unwrap()))
    });
    g.bench_function(BenchmarkId::new("serde/write_from_value_dom", N), |b| {
        let value = serde_json::to_value(&ticks).unwrap();
        b.iter(|| black_box(serde_json::to_string(black_box(&value)).unwrap()))
    });

    // simd-json: typed + DOM. Копия входа внутри iter — это цена API:
    // simd-json работает по &mut [u8] (пермутации байтов in place).
    g.bench_function(BenchmarkId::new("simd/parse_typed", N), |b| {
        b.iter(|| {
            let mut s = payload.clone().into_bytes();
            let t: Vec<Tick> = simd_json::from_slice(&mut s).unwrap();
            black_box(t)
        })
    });
    g.bench_function(BenchmarkId::new("simd/parse_dom", N), |b| {
        b.iter(|| {
            let mut s = payload.clone().into_bytes();
            black_box(simd_json::to_owned_value(&mut s).unwrap())
        })
    });

    g.finish();
}

criterion_group!(benches, bench_json);
criterion_main!(benches);
