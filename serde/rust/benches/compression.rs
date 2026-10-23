//! Ось «сжатие»: ускоряет или замедляет конвейер.
//! CPU-only конвейер: compress(serialized) и decompress + parse.
//! Экономию сети считаем в статье, здесь — только CPU и размер.

use std::hint::black_box;

use criterion::{criterion_group, criterion_main, BenchmarkId, Criterion, Throughput};
use prost::Message as _;
use serde_bench::*;

fn bench_compression(c: &mut Criterion) {
    let payload = payload_json(N);
    let ticks = gen_ticks(N);
    let archived = rkyv_bytes(&ticks);
    let table = symbol_table();
    let wire = encode_wire(&ticks, &table);
    let batch = gen_batch_msg(N);
    let proto = batch.encode_to_vec();

    let mut g = c.benchmark_group("compression");
    g.throughput(Throughput::Bytes(payload.len() as u64));

    for level in [1i32, 3, 9] {
        g.bench_function(BenchmarkId::new("zstd_json/compress", level), |b| {
            b.iter(|| black_box(zstd::bulk::compress(black_box(payload.as_bytes()), level).unwrap()))
        });
    }
    let compressed3 = zstd::bulk::compress(payload.as_bytes(), 3).unwrap();
    g.bench_function(BenchmarkId::new("zstd_json/decompress", 3), |b| {
        b.iter(|| {
            black_box(
                zstd::bulk::decompress(black_box(&compressed3), payload.len()).unwrap(),
            )
        })
    });
    // Полный конвейер «сжатый транспорт»: decompress + наивный parse.
    g.bench_function(BenchmarkId::new("zstd_json/decode_plus_parse", 3), |b| {
        b.iter(|| {
            let raw = zstd::bulk::decompress(black_box(&compressed3), payload.len()).unwrap();
            let t: Vec<Tick> = serde_json::from_slice(black_box(&raw)).unwrap();
            black_box(t)
        })
    });

    // Сжатие других форматов — на одном уровне 3, для таблицы размеров.
    g.throughput(Throughput::Bytes(proto.len() as u64));
    g.bench_function(BenchmarkId::new("zstd_proto/compress", 3), |b| {
        b.iter(|| black_box(zstd::bulk::compress(black_box(&proto), 3).unwrap()))
    });
    g.throughput(Throughput::Bytes(archived.len() as u64));
    g.bench_function(BenchmarkId::new("zstd_rkyv/compress", 3), |b| {
        b.iter(|| black_box(zstd::bulk::compress(black_box(&archived), 3).unwrap()))
    });
    g.throughput(Throughput::Bytes(wire.len() as u64));
    g.bench_function(BenchmarkId::new("zstd_flat48/compress", 3), |b| {
        b.iter(|| black_box(zstd::bulk::compress(black_box(&wire), 3).unwrap()))
    });

    g.finish();
}

criterion_group!(benches, bench_compression);
criterion_main!(benches);
