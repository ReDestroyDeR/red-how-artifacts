//! ETL end-to-end: эфемерный поток тиков, читать и писать по окнам.
//!
//! Поток не материализуется: тик i — чистая функция индекса, окна
//! обрабатываются последовательно. Окно = 32 768 тиков ≈ 4.2 MB NDJSON.
//! «10 гигабайт» = 2500 окон ≈ 10.5 GB NDJSON-эквивалента; все подходы
//! обрабатывают ОДИН И ТОТ ЖЕ логический поток тиков, отличаются только
//! байты на проводе — их и печатаем.
//!
//! Правило честности: в таймер попадает только обработка. Генерация окна
//! (write-side стоимость) и подготовительные шаги (компрессия для zstd-read)
//! — вне таймера, иначе read-подходы платили бы за чужую работу.
//!
//! Запуск: cargo run --release --bin etl -- [--windows 2500] [--approach all]

use std::time::{Duration, Instant};

use prost::Message as _;
use serde_bench::*;
use zerocopy::IntoBytes as _;

/// 250 окон ≈ 1 GB NDJSON: расхождение MB/s с 2500-оконным прогоном < 3%
/// по замеренным точкам (100 окон уже дают ±3–5%, но короткие подходы
/// шумят). 2500 окон — один демонстрационный прогон на деградацию.
const DEFAULT_WINDOWS: u64 = 250;
const WIN_TICKS: usize = 32_768;

#[derive(Clone, Copy, PartialEq)]
enum Approach {
    ReadSerdeStream,
    ReadSimdLines,
    ReadPerfect,
    ReadRkyv,
    ReadProto,
    ReadSerdeZstd,
    WriteSerde,
    WritePerfect,
    WriteRkyv,
    WriteProto,
}

impl Approach {
    const ALL: &'static [(&'static str, Approach)] = &[
        ("read/serde_stream", Approach::ReadSerdeStream),
        ("read/simd_lines", Approach::ReadSimdLines),
        ("read/perfect", Approach::ReadPerfect),
        ("read/rkyv", Approach::ReadRkyv),
        ("read/proto", Approach::ReadProto),
        ("read/serde_zstd", Approach::ReadSerdeZstd),
        ("write/serde", Approach::WriteSerde),
        ("write/perfect", Approach::WritePerfect),
        ("write/rkyv", Approach::WriteRkyv),
        ("write/proto", Approach::WriteProto),
    ];
}

fn batch_msg_of(ticks: Vec<Tick>) -> BatchMsg {
    BatchMsg {
        ticks: ticks
            .into_iter()
            .map(|t| TickMsg {
                symbol: t.symbol,
                venue: t.venue,
                price: t.price,
                qty: t.qty,
                ts_ms: t.ts_ms,
                side: t.side,
                order_id: t.order_id,
                flags: t.flags,
            })
            .collect(),
    }
}

/// Прогон: суммарное время ТОЛЬКО обработки, байтов формата, тиков.
fn run(approach: Approach, windows: u64) -> (Duration, u64, u64) {
    let mut timed = Duration::ZERO;
    let mut fmt_bytes = 0u64;
    let mut ticks_total = 0u64;

    for w in 0..windows {
        let from = w * WIN_TICKS as u64;
        match approach {
            Approach::ReadSerdeStream => {
                let window = ndjson_window(from, WIN_TICKS);
                fmt_bytes += window.len() as u64;
                let t0 = Instant::now();
                let n = serde_json::Deserializer::from_slice(&window)
                    .into_iter::<Tick>()
                    .filter_map(|r| r.ok())
                    .count();
                timed += t0.elapsed();
                ticks_total += n as u64;
            }
            Approach::ReadSimdLines => {
                let mut buf = ndjson_window(from, WIN_TICKS);
                fmt_bytes += buf.len() as u64;
                let t0 = Instant::now();
                let mut n = 0;
                for line in buf.split_inclusive_mut(|c| *c == b'\n') {
                    let end = line.len() - if line.last() == Some(&b'\n') { 1 } else { 0 };
                    let t: Tick = simd_json::from_slice(&mut line[..end]).unwrap();
                    std::hint::black_box(&t);
                    n += 1;
                }
                timed += t0.elapsed();
                ticks_total += n;
            }
            Approach::ReadPerfect => {
                let wire = wire_window(from, WIN_TICKS);
                fmt_bytes += wire.len() as u64;
                let t0 = Instant::now();
                let w = cast_wire(&wire).unwrap();
                // Один слитый проход, безветвевая валидация: LLVM опускает
                // bool->int в setcc и векторизует (+27% к ветвящейся форме,
                // матрица форм в results). В Go та же форма наоборот хуже —
                // его компилятор возвращает ветки, там канонична ветвящаяся.
                let mut sum = 0u64;
                let mut bad = 0u64;
                for t in w {
                    sum += t.price;
                    bad |= u64::from(t.symbol_idx as u64 >= SYMBOLS.len() as u64)
                        | u64::from(t.side > 1)
                        | u64::from(t.flags >= 16)
                        | u64::from(t.ts_ms <= 1_000_000_000_000);
                }
                timed += t0.elapsed();
                std::hint::black_box((sum, bad == 0));
                ticks_total += w.len() as u64;
            }
            Approach::ReadRkyv => {
                let archived = rkyv_bytes(&gen_ticks_range(from, WIN_TICKS));
                fmt_bytes += archived.len() as u64;
                let t0 = Instant::now();
                let a: &ArchivedTicks =
                    rkyv::access::<ArchivedTicks, RkyvError>(&archived).unwrap();
                let sum: u64 = a.iter().map(|t| t.price.to_native()).sum();
                timed += t0.elapsed();
                std::hint::black_box(sum);
                ticks_total += a.len() as u64;
            }
            Approach::ReadProto => {
                let proto = batch_msg_of(gen_ticks_range(from, WIN_TICKS)).encode_to_vec();
                fmt_bytes += proto.len() as u64;
                let t0 = Instant::now();
                let n = BatchMsg::decode(proto.as_slice()).unwrap().ticks.len();
                timed += t0.elapsed();
                ticks_total += n as u64;
            }
            Approach::ReadSerdeZstd => {
                let window = ndjson_window(from, WIN_TICKS);
                fmt_bytes += window.len() as u64;
                // Компрессия — стоимость producer'а, в read-таймер не входит.
                let compressed = zstd::bulk::compress(&window, 3).unwrap();
                std::hint::black_box(compressed.len());
                let t0 = Instant::now();
                let raw = zstd::bulk::decompress(&compressed, window.len()).unwrap();
                let n = serde_json::Deserializer::from_slice(&raw)
                    .into_iter::<Tick>()
                    .filter_map(|r| r.ok())
                    .count();
                timed += t0.elapsed();
                ticks_total += n as u64;
            }
            Approach::WriteSerde => {
                // Тики «пришли» из источника — вне таймера; таймерует сериализация.
                let ticks = gen_ticks_range(from, WIN_TICKS);
                let mut buf = Vec::with_capacity(WIN_TICKS * 128);
                let t0 = Instant::now();
                for t in &ticks {
                    serde_json::to_writer(&mut buf, t).unwrap();
                    buf.push(b'\n');
                }
                timed += t0.elapsed();
                fmt_bytes += buf.len() as u64;
                std::hint::black_box(&buf);
                ticks_total += ticks.len() as u64;
            }
            Approach::WritePerfect => {
                let ticks = gen_ticks_range(from, WIN_TICKS);
                let mut buf = Vec::with_capacity(WIN_TICKS * WIRE_SIZE);
                let t0 = Instant::now();
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
                timed += t0.elapsed();
                fmt_bytes += buf.len() as u64;
                std::hint::black_box(&buf);
                ticks_total += ticks.len() as u64;
            }
            Approach::WriteRkyv => {
                let ticks = gen_ticks_range(from, WIN_TICKS);
                let t0 = Instant::now();
                let bytes = rkyv_bytes(&ticks);
                timed += t0.elapsed();
                fmt_bytes += bytes.len() as u64;
                std::hint::black_box(&bytes);
                ticks_total += ticks.len() as u64;
            }
            Approach::WriteProto => {
                let batch = batch_msg_of(gen_ticks_range(from, WIN_TICKS));
                let t0 = Instant::now();
                let bytes = batch.encode_to_vec();
                timed += t0.elapsed();
                fmt_bytes += bytes.len() as u64;
                std::hint::black_box(&bytes);
                ticks_total += batch.ticks.len() as u64;
            }
        }
    }
    (timed, fmt_bytes, ticks_total)
}

fn main() {
    let mut windows = DEFAULT_WINDOWS;
    let mut approach_name = String::from("all");
    let mut args = std::env::args().skip(1);
    while let Some(arg) = args.next() {
        match arg.as_str() {
            "--windows" => windows = args.next().unwrap().parse().unwrap(),
            "--approach" => approach_name = args.next().unwrap(),
            other => {
                eprintln!("usage: etl [--windows N] [--approach all|<name>]");
                eprintln!("unknown arg: {other}");
                std::process::exit(2);
            }
        }
    }

    let approaches: Vec<&str> = if approach_name == "all" {
        Approach::ALL.iter().map(|(n, _)| *n).collect()
    } else {
        if Approach::ALL.iter().all(|(n, _)| *n != approach_name) {
            eprintln!("unknown approach: {approach_name}");
            eprintln!("available: {}", Approach::ALL.iter().map(|(n, _)| *n).collect::<Vec<_>>().join(", "));
            std::process::exit(2);
        }
        vec![&approach_name]
    };

    let ndjson_gb = (windows * WIN_TICKS as u64 * 128) as f64 / 2f64.powi(30);
    let allocator = if option_env!("CARGO_FEATURE_JEMALLOC").is_some() {
        "jemalloc"
    } else if option_env!("CARGO_FEATURE_MIMALLOC").is_some() {
        "mimalloc"
    } else {
        "system (macOS malloc)"
    };
    println!(
        "аллокатор: {} | поток: {} окон × {} тиков ≈ {:.1} GB NDJSON-эквивалента (сжатие вне read-таймера)",
        allocator, windows, WIN_TICKS, ndjson_gb
    );
    println!();
    println!("| подход | время обработки | формат, GB | MB/s формата | тиков/с |");
    println!("|---|---:|---:|---:|---:|");

    for name in approaches {
        run(Approach::ALL.iter().find(|(n, _)| *n == name).unwrap().1, 1); // прогрев вне замера
        let (elapsed, fmt_bytes, ticks) =
            run(Approach::ALL.iter().find(|(n, _)| *n == name).unwrap().1, windows);
        println!(
            "| {} | {:.2?} | {:.2} | {:.0} | {:.1}M |",
            name,
            elapsed,
            fmt_bytes as f64 / 2f64.powi(30),
            fmt_bytes as f64 / 2f64.powi(20) / elapsed.as_secs_f64(),
            ticks as f64 / elapsed.as_secs_f64() / 1e6,
        );
    }
}
