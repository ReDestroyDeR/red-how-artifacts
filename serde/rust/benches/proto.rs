//! Ось «protobuf: наивные encode/decode» (prost, codegen через derive).

use std::hint::black_box;

use criterion::{criterion_group, criterion_main, BenchmarkId, Criterion, Throughput};
use prost::Message as _;
use serde_bench::*;

fn bench_proto(c: &mut Criterion) {
    let batch = gen_batch_msg(N);
    let encoded = batch.encode_to_vec();

    let mut g = c.benchmark_group("proto");
    g.throughput(Throughput::Bytes(encoded.len() as u64));
    g.bench_function(BenchmarkId::new("prost/encode", N), |b| {
        b.iter(|| black_box(black_box(&batch).encode_to_vec()))
    });
    g.bench_function(BenchmarkId::new("prost/decode_full", N), |b| {
        b.iter(|| {
            let m = BatchMsg::decode(black_box(encoded.as_slice())).unwrap();
            black_box(m)
        })
    });
    g.finish();
}

criterion_group!(benches, bench_proto);
criterion_main!(benches);
