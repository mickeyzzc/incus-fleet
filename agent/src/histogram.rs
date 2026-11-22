use std::collections::HashMap;

use crate::proto::v1::{MetricType, Sample, Series};

const BOUNDS: [f64; 12] = [
    0.000_001, 0.000_01, 0.000_1, 0.001, 0.01, 0.1, 1.0, 2.0, 4.0, 8.0, 16.0, 32.0,
];

/// Histogram is a bounded log2-style histogram suitable for eBPF event latencies.
#[derive(Clone, Debug)]
pub struct Histogram {
    buckets: [u64; 13],
    count: u64,
    sum: f64,
}

impl Default for Histogram {
    fn default() -> Self {
        Self {
            buckets: [0; 13],
            count: 0,
            sum: 0.0,
        }
    }
}

impl Histogram {
    pub fn observe(&mut self, value: f64) {
        let mut index = BOUNDS.len();
        for (i, bound) in BOUNDS.iter().enumerate() {
            if value <= *bound {
                index = i;
                break;
            }
        }
        self.buckets[index] += 1;
        self.count += 1;
        self.sum += value;
    }

    #[allow(dead_code)] // used when merging per-CPU eBPF histograms
    pub fn merge(&mut self, other: &Histogram) {
        for (left, right) in self.buckets.iter_mut().zip(other.buckets) {
            *left += right;
        }
        self.count += other.count;
        self.sum += other.sum;
    }

    /// Render `_bucket`, `_sum`, and `_count` series with cumulative buckets.
    pub fn series(&self, name: &str, labels: &HashMap<String, String>, at_ms: i64) -> Vec<Series> {
        let mut out = Vec::with_capacity(BOUNDS.len() + 2);
        let mut cumulative = 0;
        for (i, bound) in BOUNDS.iter().enumerate() {
            cumulative += self.buckets[i];
            let mut bucket_labels = labels.clone();
            bucket_labels.insert("le".into(), format!("{bound}"));
            out.push(Series {
                name: format!("{name}_bucket"),
                labels: bucket_labels,
                r#type: MetricType::Counter as i32,
                samples: vec![Sample {
                    at_ms,
                    value: cumulative as f64,
                }],
            });
        }
        let mut inf = labels.clone();
        inf.insert("le".into(), "+Inf".into());
        out.push(Series {
            name: format!("{name}_bucket"),
            labels: inf,
            r#type: MetricType::Counter as i32,
            samples: vec![Sample {
                at_ms,
                value: self.count as f64,
            }],
        });
        out.push(Series {
            name: format!("{name}_sum"),
            labels: labels.clone(),
            r#type: MetricType::Counter as i32,
            samples: vec![Sample {
                at_ms,
                value: self.sum,
            }],
        });
        out.push(Series {
            name: format!("{name}_count"),
            labels: labels.clone(),
            r#type: MetricType::Counter as i32,
            samples: vec![Sample {
                at_ms,
                value: self.count as f64,
            }],
        });
        out
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn observes_with_bounded_buckets() {
        let mut hist = Histogram::default();
        hist.observe(0.0005);
        hist.observe(0.5);
        hist.observe(100.0);
        assert_eq!(hist.count, 3);
        assert_eq!(hist.buckets[3], 1);
        assert_eq!(hist.buckets[6], 1);
        assert_eq!(hist.buckets[12], 1);
        assert!((hist.sum - 100.5005).abs() < 0.0001);
    }

    #[test]
    fn merges_and_renders() {
        let mut left = Histogram::default();
        left.observe(0.001);
        let mut right = Histogram::default();
        right.observe(1.0);
        left.merge(&right);
        assert_eq!(left.count, 2);
        let series = left.series("fleet_host_test_latency_seconds", &HashMap::new(), 100);
        assert!(series.iter().any(|item| item.name.ends_with("_bucket")
            && item.labels["le"] == "+Inf"
            && item.samples[0].value == 2.0));
        assert!(series.iter().any(|item| item.name.ends_with("_sum")));
        assert!(series.iter().any(|item| item.name.ends_with("_count")));
    }
}
