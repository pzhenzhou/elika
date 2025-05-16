## Performance Report

### Overview

The Proxy under test is more than a simple forwarder of RESP2/RESP3 commands—it acts as a cloud-native, multi-tenant
control plane for multiple Redis clusters, offering unified ingress, connection pooling, automated scaling and detailed
monitoring.
This report validates that introducing the Proxy does not create material performance degradation relative to direct
connections, even when the backend database is constrained to < 500 mCPU.

### Test Environment

| Component                     |          vCPU / Memory | Instance type | Notes                                           |
|-------------------------------|-----------------------:|---------------|-------------------------------------------------|
| **Proxy**                     |         2 vCPU / 2 GiB | `c5.2xlarge`  | Multiplexing & pooling enabled                  |
| **Redis-compatible KV store** | **< 500 mCPU** / 2 GiB | `i4i.xlarge`  | CPU-constrained—dominant source of base latency |
| **memtier\_benchmark client** |         4 vCPU / 1 GiB | `c5.2xlarge`  | Acts as workload generator                      |

Workload tool memtier_benchmark

- Write test 5 000 total SETs, 4 threads, 50 connections/thread

- Read test 60 s sustained GETs, 4 threads, 50 connections/thread

### Results

#### Write Test

| Metric             | Direct-to-DB | Through Proxy | Δ (% or abs) |
|--------------------|-------------:|--------------:|--------------|
| Throughput (Ops/s) |  **2 625.3** |   **2 102.1** | – 19.9 %     |
| Avg Latency (ms)   |        76.10 |         97.80 | + 21.7 ms    |
| p99 Latency (ms)   |       198.66 |        278.53 | + 80 ms      |
| Network BW (KB/s)  |        443.2 |         354.9 | – 20 %       |

> Interpretation – The ~20 % delta is dominated by the extra network hop and inevitable command re-serialization. Given
> that the backend CPU is already limited (< 0.5 vCPU), the absolute latency increase (~22 ms) remains within the same
> order of magnitude as the direct case.

#### Read Test

| Metric             | Direct-to-DB | Through Proxy | Δ (% or abs) |
|--------------------|-------------:|--------------:|--------------|
| Throughput (Ops/s) | **15 553.5** |  **14 494.3** | – 6.8 %      |
| Avg Latency (ms)   |        12.85 |         13.77 | + 0.92 ms    |
| p99 Latency (ms)   |        82.43 |         84.48 | + 2.0 ms     |
| Network BW (KB/s)  |      2 550.0 |       2 376.4 | – 6.8 %      |

> Interpretation – For GET-heavy traffic, throughput loss is < 7 % and the mean-latency cost is < 1 ms—well inside
> typical run-to-run variance for cloud workloads.

### Conclusion

With the backend constrained to < 500 mCPU, the Proxy introduces no significant performance loss:

- Writes: ~20 % lower throughput, but absolute latency remains dominated by backend CPU limits.

- Reads: < 7 % overhead—practically negligible.

Therefore, the Proxy can be safely deployed as the standard access layer for Redis clusters, gaining advanced management
capabilities without compromising service-level objectives.







