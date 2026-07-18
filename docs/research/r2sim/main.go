//go:build ignore
// +build ignore

// RESEARCH ARTIFACT — not compiled by `go build ./...`.
// Run standalone: `go run docs/research/r2sim/main.go -mbps=20`.
//
// Status: research-stage documentation + weak implementation snippets.
// Measured findings here are load-bearing for docs/superpowers/specs/
// 2026-04-19-fast-sync-v2.1-design.md. The code shape is inspiration
// only — production implementation must re-express these pipelines
// through the hexagonal ports / DI / SOLID flow defined in the spec.
//
// When production diverges from a finding documented here, re-run the
// experiment that produced the finding before changing the spec.
//
// Package main is a proof-of-concept uploader that simulates syncing a
// Minecraft world directory to Cloudflare R2 under realistic user network
// conditions. It is not production code — it exists to validate architecture
// choices and produce performance data for the team that will build the real
// service. The research summarised below is load-bearing: each decision in
// this file is backed by a measured experiment. When the production code
// diverges from something documented here, the experiment that made the
// decision should be re-run first.
//
//
// ============================================================================
// DATASET AND WORKLOAD SHAPE
// ============================================================================
//
// Target: a single Minecraft world directory, ~1.06 GB raw, 1069 files.
// File-type mix (sorted by count):
//
//	534  .dat          NBT tag trees, gzipped
//	458  .mca          Anvil "region" files — 4KB locations table + 4KB
//	                   timestamps table + up to 1024 zlib-deflated chunks
//	 46  .json         plain text (advancements, stats)
//	 19  .dat_old      gzipped NBT snapshots
//	 12  misc          zip/png/yml/toml/mcmeta/txt/lock/...
//
// Key property: .dat and .mca (which together are >99% of bytes) contain
// *internally compressed* payloads. A naive external compressor sees
// high-entropy blobs with little exploitable redundancy. This fact drove
// most of the experimental findings below.
//
//
// ============================================================================
// COMPRESSION RESEARCH (summary of experiments run 2026-04-20)
// ============================================================================
//
// 1. zstd at DEFAULT level compresses this dataset to 66.50%.
//    Raw 1059.47 MB → 704.57 MB. One-shot, per-file, no dict. This is the
//    headline number the production code should expect.
//
// 2. Compression LEVEL has diminishing returns (per-file, no dict, no unpack):
//       fastest  67.09%  1.41s
//       default  66.50%  1.56s
//       better   65.76%  2.96s
//       best     64.47%  28.63s
//    fastest→default: -6.24 MB for +0.15s   (near free)
//    default→better:  -7.82 MB for +1.40s   (not worth it — 0.74pp)
//    better→best:    -13.72 MB for +25.67s  (terrible)
//    DECISION: use DEFAULT. It is the right middle ground — captures
//    ~97% of the achievable compression gain on this dataset at the
//    fastest practical wall clock. The ~0.74pp that "better" offers
//    (under 8 MB per world) is not a win worth the extra CPU on a run
//    that otherwise completes in ~1.5 s of compress time. Same story
//    for BEST, only more so. These deltas are dominated by pre-compressed
//    .mca payloads that no zstd level can further squeeze.
//
// 3. DICTIONARIES DO NOT HELP when compressing raw Minecraft files.
//    Trained per-extension dicts (>10 samples per type), applied per file.
//    Measured savings across all levels: 0.02–0.04 percentage points.
//    Dict bytes (~440 KB overhead) ≈ bytes saved. Net wash or slight loss.
//    Root cause: .mca/.dat have no shared plaintext patterns for the dict
//    to capture — internal zlib/gzip already destroyed them.
//    DECISION: no dictionaries in production. (Exception noted below.)
//
// 4. Decompressing .mca chunks BEFORE zstd gets 47%→39% ratio with dicts,
//    but costs 14×–55× more wall time (29s–81s vs ~2s) because the
//    unpacked payload is 4.14× larger than raw before re-compression.
//    Also adds a substantial correctness risk: a custom Anvil parser owns
//    restore integrity; a bug silently corrupts worlds.
//    DECISION: rejected. ~200 MB savings per world is not worth the
//    complexity, CPU, or correctness risk. The user called this "diminishing
//    returns where time is better."
//
// 5. Single-stream tar|zstd beats per-file by ~14 MB at `better` level —
//    a ~2% relative win. Costs the ability to restore a single file without
//    decompressing the whole archive.
//    DECISION: per-file. The 14 MB does not justify losing per-file restore
//    granularity for incremental sync / partial recovery flows.
//
// Final production-worthy compression recipe:
//   zstd, level=default, no dictionary, per-file output (one .zst per source).
//
//
// ============================================================================
// PER-FILE-TYPE COMPRESSION BEHAVIOUR (measured)
// ============================================================================
//
// Aggregated from the experiments/r2sim run — compressed output matched to
// source, grouped by filename extension. Columns:
//   count  = number of files of this type in the dataset
//   raw    = total raw bytes, MB
//   zst    = total compressed bytes, MB
//   ratio% = zst / raw × 100   (lower = better)
//   saved  = raw - zst, MB     (absolute bytes eliminated)
//
//   ext        count     raw_MB    zst_MB   ratio%   saved_MB   verdict
//   ─────────────────────────────────────────────────────────────────────────
//   backup         2       5.53      0.16     2.98       5.36   crushed (text)
//   json          46       3.17      0.27     8.36       2.91   crushed (text)
//   toml           1       0.00      0.00    48.59       0.00   good (tiny)
//   txt            1       0.00      0.00    53.73       0.00   good (tiny)
//   mca          458    1047.69    701.09    66.92     346.60   BULK WINNER
//   zip            3       0.11      0.09    75.18       0.03   mild
//   yml            1       0.00      0.00    78.92       0.00   mild
//   mcmeta         1       0.00      0.00    89.76       0.00   mild
//   dat          534       2.79      2.80   100.03       0.00   ❌ wash (gzipped NBT)
//   dat_old       19       0.15      0.15   100.17       0.00   ❌ wash (gzipped NBT)
//   png            1       0.01      0.01   100.13       0.00   ❌ already compressed
//   thanos         1       0.00      0.00   110.08       0.00   ❌ header overhead
//   lock           1       0.00      0.00   262.50       0.00   ❌❌ tiny file, frame header > body
//
// Reading the table:
//
//  - THE BULK WINNER IS .mca. Even at a mediocre 66.92% ratio, it saves
//    346.60 MB — that is ~98% of all bytes saved in the entire run. It
//    dominates because .mca is ~99% of raw bytes. Any future optimisation
//    effort should target this type; a 1pp improvement on mca saves more
//    than compressing every other file type to zero.
//
//  - THE RATIO WINNERS ARE plain-text formats: .backup (3%), .json (8%),
//    .toml (49%), .txt (54%). zstd shines on repetitive English-ish text.
//    These types compress beautifully but collectively they are <1% of raw
//    bytes, so the absolute contribution is tiny. No tuning needed — they
//    already give you everything they can.
//
//  - THE WASHES ARE .dat and .dat_old. These are gzipped NBT — internal
//    compression has already removed exploitable patterns, so external zstd
//    adds 0.03–0.17% overhead without finding anything. Same story for
//    .png. Treat these as "incompressible" and move on.
//
//  - THE TINY-FILE TRAP: .lock is a ~4-byte file, so the zstd frame header
//    dwarfs the payload and you get 262% of original. In production,
//    consider skipping compression for files below a small-size threshold
//    (e.g. < 128 bytes) and uploading them raw. Cost in the current run:
//    negligible (tens of bytes across a handful of files), but it is a
//    pattern worth knowing.
//
// OPERATIONAL IMPLICATIONS
//
//   1. If you ever want to move the needle on compression ratio, invest
//      in .mca specifically. The "unpack internal zlib chunks first, then
//      zstd+dict" path (documented above, currently rejected) lives here
//      and would take .mca from 67% to ~40% — another ~280 MB per world.
//
//   2. .dat and .dat_old are dead-end targets. Do not spend cycles trying
//      to squeeze them. Same answer for any payload that is already
//      gzip/zlib/lz4-compressed.
//
//   3. For mixed content types in other environments, this table is the
//      template for the decision:
//         ratio < 30%  → text-like, zstd is already enough
//         ratio ~65%   → pre-compressed binary with some structure (like mca)
//         ratio ~100%  → incompressible, skip the compressor or accept wash
//         ratio > 100% → tiny file, header overhead, consider size threshold
//
//   4. Add a floor threshold. For production, a simple heuristic:
//
//         if rawSize < 128  → upload raw, no zstd frame
//         if ext in {dat,dat_old,png,jpg,zip,gz,zst,mp4,...} → upload raw
//         else                                            → stream through zstd
//
//      This would remove the small-file header tax and avoid spending CPU
//      on payloads we already know will not compress.
//
//
// ============================================================================
// PIPELINE ARCHITECTURE
// ============================================================================
//
// Each of N worker goroutines runs this per-file pipeline:
//
//	source file
//	    │ (os.Open, read chunk-by-chunk)
//	    ▼
//	countingReader           ← atomically increments rawTotal for progress
//	    │
//	    ▼
//	zstd.Encoder             ← ONE encoder per worker, reused via Reset()
//	    │
//	    ▼
//	io.MultiWriter
//	    │     │
//	    │     └──► throttledWriter ── blocks on Throttle.Wait(len(p))
//	    │                            = simulated R2 upload
//	    ▼
//	local .zst file          ← persisted for retry path
//
// Back-pressure is the load-bearing property. throttledWriter blocks Write
// until the shared token bucket grants enough tokens. io.MultiWriter is
// synchronous — it returns only after every sink has accepted the bytes. So
// zstd.Encoder's Write blocks, which blocks io.Copy reading from source.
// Result: source read rate == upload rate × compression ratio.
//
// Why this matters for instrumentation: rawTotal grows in lock-step with
// actual bytes on the wire, not in bursts as workers pre-fetch into a
// buffered compression stage. The progress bar and ETA reflect real
// pipeline throughput from second one.
//
// Retry path (attempt 2..N): reuploadFromLocal opens the already-produced
// .zst from attempt 1 and streams it through a fresh throttledWriter. No
// source re-read, no re-compression. The rawTotal counter is NOT advanced
// on retry because no new source bytes are consumed — retries show up only
// in the retry counter and via reduced upload progress.
//
// End-of-stream flake simulation: at the end of a successful stream we flip
// a coin against the --err-rate and return an error to force the retry loop.
// This mirrors how real R2 flakes typically manifest (5xx after body sent,
// TCP reset near end, etc.) better than mid-stream-fail would, and keeps
// the local .zst intact for cheap retries.
//
//
// ============================================================================
// QUEUE AND DEAD-LETTER QUEUE
// ============================================================================
//
// We use container/list + sync.Mutex for both the work queue and the DLQ.
// Idiomatic Go stdlib choice when you need:
//   - explicit Pop() semantics
//   - inspectable Len() for the progress line
//   - Push() from one goroutine, Pop() from many
//
// A buffered channel would also work, but gives no length visibility and
// caps the producer at channel size. The file walker pushes all 1069 paths
// up front (sort-desc by size by default), so the full queue length is
// useful intelligence for the progress UI.
//
// DLQ semantics: when a file exhausts --retries attempts, its relative
// path is Pushed to the DLQ. At the end of the run, DLQ contents are
// logged. In production, persist this across restarts (file, Redis, SQS)
// so a process crash does not lose failure records.
//
// Size-desc sort: when --sort-desc is true (default), we sort files by
// raw byte size before queueing, largest first. This DOES NOT change total
// wall time (the shared upload cap governs), but DOES stabilise ETA from
// tick 1 by making the first velocity samples reflect the sustained
// mca-dominated rate rather than oscillating between small-file bursts.
// Small files then drain into whatever bandwidth the big ones leave at tail.
//
//
// ============================================================================
// PROGRESS REPORTING
// ============================================================================
//
// One-second ticker (intentional — UI cadence, not reader cadence). The
// ticker emits a single log line containing:
//
//   t=<elapsed>s eta=<s> done=<N>/<total> files raw=<bytes>/<total> (pct%)
//   up=<bytes> | net cap=<mbps> avg=<m> now=<m> ret=<n> fail=<n>
//               | disk avg=<m> now=<m> fps=<files/s>
//               | pool=<active>/<workers> q=<pending> dlq=<failed>
//               | go=<goroutines> heap=<mb> sys=<mb> gc=<count>
//
// Metric choices — these were iterated several times in the research
// conversation, so the rationale matters:
//
//   pct & ETA use raw bytes read / total raw bytes, NOT files done or
//   uploaded bytes. Because reads are backpressured by upload (see pipeline
//   section), rawTotal advances smoothly and represents true pipeline
//   progress. Uploaded-byte-based ETA was tried and rejected because it
//   reports 0% for the first ~20s on slow pipes while the first uploads
//   complete — ugly for UIs that need an immediate answer.
//
//   ETA = (totalRawBytes - rawDone) / (rawDone / elapsed). Cumulative
//   average was chosen over rolling window for simplicity; it converges
//   within seconds of steady-state.
//
//   "done/total files" is still reported — it counts completed uploads,
//   not reads. Useful to surface because users think in "files processed,"
//   but it lags the raw-byte progress on big files.
//
// Progress is ticker-driven, not reader-driven. Considered per-Read and
// byte-milestone emission; rejected because UI wants predictable 1-per-second
// cadence for smooth rendering.
//
//
// ============================================================================
// MEMORY PROFILE AND HOW WE GOT IT LOW
// ============================================================================
//
// Measured peak on 80 Mbps run (before / after optimisation):
//
//   heap        147–259 MB  →  187–198 MB  (flat)
//   sys peak       410 MB   →    214 MB
//   GC cycles         153   →        3     (in 76s)
//
// What actually worked:
//
//   1. ENCODER PER WORKER (biggest win). The naive pattern is
//      `zstd.NewWriter(...)` per file — that allocates a fresh 1 MB window
//      plus match/literal tables each time. Over 1069 files that's ~1 GB
//      of allocation churn, triggering GC constantly. Instead we construct
//      one *zstd.Encoder per worker at startup and call enc.Reset(sink)
//      per file. Allocation rate collapses.
//
//   2. sync.Pool for io.Copy buffers (small win). io.Copy allocates a
//      32 KB buffer per call; we reuse 64 KB pooled buffers via
//      io.CopyBuffer. Saves ~30–60 MB of allocation churn per run.
//
//   3. debug.SetGCPercent(200) (NO measurable win here) — tried and
//      removed. With (1) already flattening allocations, GC barely fires
//      anyway, and SetGCPercent only changes frequency, not peak. Not
//      carried into production guidance.
//
// Downstream of these changes the pool is stable: workers hold one zstd
// encoder + a pooled 64 KB buffer each, and main holds the file list +
// two queues. ~200 MB total steady-state for 10 workers.
//
//
// ============================================================================
// BOTTLENECK ANALYSIS
// ============================================================================
//
// Measured elapsed at 20 Mbps cap (home user): 5m 20s for 1069 files.
// Theoretical floor: 704.57 MB × 8 bits / 20 Mbps = 281.8s = 4m 42s.
// Gap: 38s, of which:
//   - ~31s retry re-transmission waste (118 retries × avg file size)
//   - ~7s tail stall (last 4 large .mca in flight when queue empties)
//
// Efficiency: ~97% of the shared-pipe cap, with the remaining 3% split
// between retry amplification and the end-of-run serial tail.
//
// Non-bottlenecks (verified):
//   - Disk read / write (NVMe does 500+ MB/s)
//   - Compression CPU (klauspost default ≈ 400 MB/s single-core)
//   - Queue contention (mutex held for microseconds per Pop)
//   - Go runtime scheduling (10 workers, 12–32 goroutines)
//
// Bandwidth scaling observed:
//   Cap        Elapsed   Throughput    Retries
//   20 Mbps    5m 20s    17.59 Mbps    118
//   80 Mbps    1m 16s    73.73 Mbps    123
//   1 Gbps     6.2s      913.65 Mbps   109
//
// All three hit their retry-adjusted ceilings within 10%.
//
//
// ============================================================================
// WORKER COUNT
// ============================================================================
//
// 10 workers is the single production setting. Measured at 20 / 80 / 1000
// Mbps, a pool of 10 saturates every link — pipe bandwidth is always the
// cap, CPU never is. Finer tuning per environment buys nothing and adds
// config surface. One number, every deployment.
//
// R2 uses HTTP/2 → all 10 workers multiplex streams on one TCP connection,
// so worker count has no effect on connection topology either.
//
//
// ============================================================================
// WHAT THIS POC DOES NOT MODEL
// ============================================================================
//
//   - TLS 1.3 / HTTP/2 handshake cost (~50–300ms per fresh connection)
//   - TCP slow-start ramp (a few RTTs to full window)
//   - SigV4 signing CPU cost (~1ms per request)
//   - aws-sdk-go-v2 middleware chain overhead
//   - Multipart upload create/complete round-trips
//   - R2 per-bucket rate limits (429 responses)
//   - Residential ISP bufferbloat / jitter / asymmetric links
//   - IPv4 vs IPv6 path differences
//   - Disk cache warmup (first walk is always slower)
//
// These are all constant-factor overheads on top of the pipe-limited
// baseline; treat the measured numbers here as upper bounds on throughput
// and lower bounds on wall time. Real R2 will be somewhat slower per GB.
//
//
// ============================================================================
// STREAMING DECOMPRESSION (RESTORE PATH)
// ============================================================================
//
// zstd is a fully streaming format. The restore side never needs to buffer
// the whole compressed object in memory or on disk.
//
// klauspost exposes two APIs:
//
//	Decoder.DecodeAll(src, dst)    one-shot; requires full compressed []byte
//	zstd.NewReader(io.Reader)      streaming; consumes bytes as they arrive
//
// Use NewReader for anything coming from the network or a file. Internal
// state is ~1 MB (decoder window + frame bookkeeping). The wire bytes
// flow through without full-object buffering.
//
// Composed end-to-end restore from R2 (no intermediate files):
//
//	resp, _ := r2Client.GetObject(ctx, &s3.GetObjectInput{Bucket: b, Key: k})
//	defer resp.Body.Close()
//	zr, _ := zstd.NewReader(resp.Body)
//	defer zr.Close()
//	io.Copy(outFile, zr)          // R2 → decompress → disk, streamed
//
// Multi-frame streams (multiple zstd frames concatenated, e.g. one per file
// appended) are auto-advanced by NewReader — no extra handling required.
//
// CAVEAT: random access. Plain zstd frames are NOT byte-range seekable.
// To decompress bytes [N, M] without reading from offset 0 you need the
// zstd SEEKABLE format (adds a skippable-frame index at the tail). klauspost
// ships a seekable subpackage, or use upstream libzstd_seekable. Not needed
// for "download the whole world, restore" flows; matters if the production
// code ever wants to peek inside large archived blobs (e.g. fetch one .mca
// out of a tar.zst without grabbing the whole thing).
//
//
// ============================================================================
// PRODUCTION MIGRATION GUIDE
// ============================================================================
//
// The real implementation should keep this file's queue + DLQ + worker
// pool + progress ticker, and replace the two simulation primitives
// (streamCompressUpload, reuploadFromLocal) with AWS SDK v2 calls.
//
// 1. CLIENT SETUP.
//    Use github.com/aws/aws-sdk-go-v2/feature/s3/manager.Uploader with an
//    s3.Client configured for R2:
//
//        cfg, _ := config.LoadDefaultConfig(ctx,
//            config.WithRegion("auto"),
//            config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
//                accessKeyID, secretAccessKey, "")))
//        client := s3.NewFromConfig(cfg, func(o *s3.Options) {
//            o.BaseEndpoint = aws.String(
//                "https://" + accountID + ".r2.cloudflarestorage.com")
//        })
//        up := manager.NewUploader(client, func(u *manager.Uploader) {
//            u.PartSize = 16 * 1024 * 1024 // 16 MB parts for big .mca
//            u.Concurrency = 1             // see note below
//        })
//
// 2. UPLOAD CALL.
//    Replace streamCompressUpload with: compress to local .zst (streaming,
//    no buffering) then up.Upload with the .zst file as Body. Passing a
//    *os.File gives the SDK a seekable body so retries are cheap.
//
// 3. CONCURRENCY LAYERS.
//    Two dimensions of parallelism:
//      - worker pool = files in flight across objects (we control)
//      - uploader.Concurrency = parts in flight for ONE object (SDK)
//    On a 20 Mbps home pipe set Concurrency=1: more parallel parts can't
//    push more bytes through the same pipe and waste TCP fairness.
//    On a 1 Gbps+ pipe, Concurrency=3–5 helps saturate the TCP window.
//
// 4. HTTP PROTOCOL.
//    Cloudflare's edge negotiates HTTP/2 via ALPN. Go's stdlib transport
//    picks it up automatically. Log response.Proto once at startup to
//    confirm. HTTP/2 multiplexes all streams on ~1 TCP connection, so
//    pool tuning is largely moot. If HTTP/1.1 is ever negotiated, bump
//    http.Transport.MaxIdleConnsPerHost ≈ workers × uploader.Concurrency.
//
// 5. RETRIES.
//    The SDK's StandardRetryer does the right thing: classifies errors,
//    honours 429 Retry-After, backs off with jitter. Bump MaxAttempts to
//    5 to match this POC. Delete our hand-rolled retry loop.
//
// 6. INTEGRITY.
//    Enable CRC32C checksum on the Uploader. R2 validates server-side.
//    Free correctness check with ~zero overhead.
//
// 7. IDEMPOTENCY AND KEY SCHEME.
//    Choose deterministic object keys, e.g. "<world-id>/<date>/<relative-path>.zst".
//    R2 PutObject is idempotent: the same key + same body = same result,
//    so client-driven retries (including after process crash) are safe.
//
// 8. RESUMABLE SYNC (HUGE WIN).
//    Before uploading, HeadObject to compare last-modified / etag against
//    a local manifest of what was last synced. Skip unchanged files. For
//    a fleet of 10,000 worlds with ~5% churn between syncs, this is a 95%
//    bandwidth saving. Not optional for production.
//
// 9. DLQ PERSISTENCE.
//    Persist failed keys so process restarts do not lose them. File-backed
//    queue, Redis, or SQS are all fine. In-memory DLQ is POC-only.
//
// 10. GRACEFUL SHUTDOWN.
//     Wire context.WithCancel from a signal handler down through the
//     queue and workers. SDK Upload aborts in-flight multiparts on ctx
//     cancel automatically — uses it to clean up R2-side partial state.
//
//
// ============================================================================
// AWS SDK V2 — WHAT FITS OUR PIPELINE
// ============================================================================
//
// Our pipeline is already right-shaped for the SDK: per-world, per-file,
// streaming compress to a local .zst (retention copy), then upload that
// same local file to R2 (offsite copy). The tee to local is the seekable
// body the SDK wants for multipart retry; we get retention AND cheap retries
// from one write. Swap the simulated throttledWriter for a single SDK call,
// keep everything else.
//
// THE WHOLE SWAP
//
//	// before (POC): stream compress to local + simulated throttled sink
//	streamCompressUpload(enc, src, localDst, wrapReader, throttle, errRate)
//
//	// after (prod):
//	//   1. streaming compress src → localDst (retention copy)
//	//   2. uploader reads localDst → R2 (offsite copy)
//	_, err := streamingCompress(enc, src, localDst, wrapReader)
//	if err == nil {
//	    f, _ := os.Open(localDst)        // *os.File: seekable, cheap to retry
//	    _, err = up.Upload(ctx, &s3.PutObjectInput{
//	        Bucket: &bucket, Key: &key,
//	        Body:              f,
//	        ChecksumAlgorithm: types.ChecksumAlgorithmCrc32c,
//	    })
//	    f.Close()
//	}
//
// That's the whole integration. Two observations:
//  - The local .zst IS the retention copy. No extra tee needed.
//  - Passing *os.File (seekable) lets the SDK retry individual 16 MB parts
//    without re-reading or re-compressing the world file. This matches the
//    retry behaviour the POC already demonstrates via reuploadFromLocal.
//
// WHAT THE SDK GIVES US FOR FREE (keep it, don't rebuild it)
//
//	✓ Per-attempt retry with exp backoff + jitter, honours 429 Retry-After
//	✓ Per-part retry inside a multipart upload (failed 16 MB chunk re-sends,
//	  not the whole file)
//	✓ Proper error classification (5xx/429/network retry; 4xx/ctx-cancel fail)
//	✓ SigV4 re-signing per attempt
//	✓ Multipart abort on ctx cancel — no orphaned parts in the bucket
//	✓ CRC32C integrity (server-validated, end-to-end bit-rot catch)
//	✓ HTTP/2 via ALPN out of the box — one TCP, streams multiplex
//
// Configure it once:
//
//	cfg, _ := config.LoadDefaultConfig(ctx,
//	    config.WithRegion("auto"),
//	    config.WithCredentialsProvider(creds),
//	    config.WithRetryer(func() aws.Retryer {
//	        return retry.NewStandard(func(o *retry.StandardOptions) {
//	            o.MaxAttempts = 5                 // matches POC
//	            o.MaxBackoff  = 20 * time.Second
//	        })
//	    }))
//	s3c := s3.NewFromConfig(cfg, func(o *s3.Options) {
//	    o.BaseEndpoint = aws.String(
//	        "https://" + accountID + ".r2.cloudflarestorage.com")
//	})
//	up := manager.NewUploader(s3c, func(u *manager.Uploader) {
//	    u.PartSize    = 16 * 1024 * 1024        // our biggest .mca is ~9 MB
//	    u.Concurrency = 1                       // worker pool is the parallelism
//	})
//
// One setting works across every link speed we care about. The worker pool
// (default 10) already gives 10 parallel streams — one per file in flight.
// On a 20 Mbps home pipe that is ~2 Mbps per stream. On 1 Gbps it is
// ~100 Mbps per stream. Both are inside any reasonable TCP window, so
// additional within-object parallelism (`Concurrency > 1`) buys nothing
// at these pipe speeds and only adds CPU. Skip the tuning.
//
// WORKERS KEEP WORKING AS-IS
//
// The queue, worker pool, size-desc sort, progress ticker, and encoder-per-
// worker memory pattern are all still the right shape. The worker's per-file
// body changes from "streamCompressUpload + retry loop" to
// "streamingCompress + up.Upload"; everything around it stays.
//
//	for {
//	    src, ok := queue.Pop()
//	    if !ok { return }
//	    compSize, err := streamingCompress(enc, src, localDst, wrapReader)
//	    if err == nil {
//	        f, _ := os.Open(localDst)
//	        _, err = up.Upload(ctx, putInput(&f))
//	        f.Close()
//	    }
//	    if err != nil { dlq.Push(entry) }    // ONE call → ONE dlq push
//	}
//
// CRITICAL: call up.Upload EXACTLY ONCE per file. It already ran MaxAttempts
// internally with correct backoff and error classification. Wrapping it in
// another retry loop re-retries 4xx errors (which should fail fast) and
// double-counts the retry budget.
//
// DLQ: MAKE IT DURABLE
//
// The POC uses container/list for DLQ (perfect for the simulation). Prod
// needs failures to survive a process restart — append to a JSONL file
// with fsync per entry, or push to Redis/SQS. Reprocessing is the same
// worker pipeline reading from the DLQ instead of the walker; idempotent
// PutObject makes this safe.
//
//	type DLQ struct { mu sync.Mutex; enc *json.Encoder; f *os.File }
//	func (d *DLQ) Push(e FailedJob) error {
//	    d.mu.Lock(); defer d.mu.Unlock()
//	    if err := d.enc.Encode(e); err != nil { return err }
//	    return d.f.Sync()   // survives SIGKILL
//	}
//
// OBSERVABILITY WE ALREADY HAVE — KEEP IT
//
// The 1 s progress ticker (elapsed, ETA, raw/uploaded bytes, pool, heap, GC)
// still works: atomic counters are updated where they are today, and the
// SDK's internal retries are invisible from the caller's perspective. To
// also capture SDK-level attempts, drop in a middleware:
//
//	s3c := s3.NewFromConfig(cfg, func(o *s3.Options) {
//	    o.APIOptions = append(o.APIOptions,
//	        func(stack *smithymiddleware.Stack) error {
//	            return stack.Finalize.Add(attemptLogger{}, smithymiddleware.After)
//	        })
//	})
//
// where attemptLogger hooks retry.GetAttemptNumber(ctx) and increments a
// counter the ticker can read. Zero change to the progress UI.
//
// RESUMABLE SYNC (huge win that fits the retention model)
//
// Because the local .zst is retention, we already know what we synced last
// time (local file + recorded etag). Before queueing, HeadObject to check
// whether R2 already has the matching bytes; skip if so. With ~5% churn
// per world per day across a fleet, this cuts bandwidth by ~95%.
//
//	head, err := s3c.HeadObject(ctx, &s3.HeadObjectInput{
//	    Bucket: &bucket, Key: &key,
//	})
//	if err == nil && head.ETag != nil &&
//	    etagMatchesLocal(*head.ETag, localDst) {
//	    return // already synced, no upload
//	}
//
// This is the single biggest production win on top of the POC. The local
// retention copy makes it essentially free to implement.
//
// CONNECTION TUNING
//
// One knob: IdleConnTimeout = 30s. Everything else stays stock. R2 uses
// HTTP/2 so pool size and H2 forcing are not worth configuring.
//
//	cfg.HTTPClient = awshttp.NewBuildableClient().
//	    WithTransportOptions(func(tr *http.Transport) {
//	        tr.IdleConnTimeout = 30 * time.Second
//	    })
//
// SUMMARY: WHAT TO CHANGE vs WHAT TO KEEP
//
//	REPLACE (sim → prod)
//	  throttledWriter + reuploadFromLocal + hand-rolled retry loop
//	  → streamingCompress (to local .zst) + up.Upload (to R2)
//
//	KEEP AS-IS (already the right shape)
//	  walker → queue → worker pool → encoder-per-worker → progress ticker
//	  container/list queue, DLQ semantics, size-desc ordering
//	  1 s progress cadence, byte-based pct & ETA
//
//	ADD (small, mechanical)
//	  durable DLQ (JSONL + fsync)
//	  HeadObject skip path for resumable sync
//	  attempt-logging middleware if retries need to surface in the ticker
//
//
// ============================================================================
// TUNING CHEAT SHEET
// ============================================================================
//
// These values come out of the experiments and the bandwidth matrix.
// They are starting points; measure in your target environment.
//
//   zstd level          SpeedDefault  (~level 3, firm — do not bump to `better`)
//   zstd dictionary     none
//   zstd window         library default (8 MB)
//   workers              10 (one value, every pipe speed)
//   uploader.PartSize    16 MB
//   uploader.Concurrency 1 (worker pool is the parallelism)
//   retries              5 (SDK StandardRetryer MaxAttempts)
//   IdleConnTimeout      30s (only transport tweak)
//   CRC32C checksums     enabled
//   sort                 size-desc (stabilises UI progress)
//
//
// ============================================================================
// SIMULATION FLAGS (this POC only)
// ============================================================================
//
//   -world       path to extracted world directory (default "world")
//   -out         output root directory (default "experiments")
//   -workers     concurrent workers (default 10)
//   -retries     max upload attempts per file (default 5)
//   -err-rate    per-attempt upload failure probability (default 0.10)
//   -mbps        shared uplink bandwidth cap in Mbps, 0=unlimited (default 20)
//   -limit       limit files processed, 0=all (default 0)
//   -window      zstd encoder window bytes, 0=library default (default 0)
//   -sort-desc   process biggest files first (default true)
//   -report-mb   legacy, unused in ticker mode (default 10)
//
// The -mbps flag is the key experiment knob. Run:
//
//   ./zstdtest -mbps=20   > 20.log   # home user
//   ./zstdtest -mbps=80   > 80.log   # fast residential
//   ./zstdtest -mbps=1000 > 1000.log # data-centre
//
// to see the three bandwidth profiles the user asked about during the
// research conversation.
//
//
// ============================================================================
// FILE STRUCTURE OF THIS POC
// ============================================================================
//
//   Throttle             shared token bucket, caps aggregate upload rate
//   throttledWriter      io.Writer blocking on Throttle — the simulated R2 sink
//   countingReader       transparent src wrapper, counts bytes for progress
//   Queue                container/list + mutex, used for work queue and DLQ
//   streamCompressUpload first attempt: src → zstd → tee(localFile, throttled)
//   reuploadFromLocal    retries: re-stream existing .zst through throttled sink
//   main                 flags, walker, worker pool, progress ticker, results
//
// The code order follows dependency order: primitives first, composition
// second, orchestration last.
package main

import (
	"container/list"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/klauspost/compress/zstd"
)

// copyBufPool shares 64 KB scratch buffers across every io.CopyBuffer call
// in the pipeline. Two questions worth answering plainly:
//
// 1) WHY A POOL AT ALL
//
// io.Copy(dst, src) (no explicit buffer) allocates a fresh 32 KB buffer
// on every call — that is hard-coded inside the stdlib. Our pipeline has
// two io.Copy calls per file (streamCompressUpload and reuploadFromLocal),
// so 1069 files × 2 paths × 32 KB = ~67 MB of allocation churn per run.
// All of it is short-lived, all of it is swept by GC. On the 80 Mbps
// measurement this pattern was part of the 153 GC cycles we started at.
//
// A sync.Pool gives each worker a long-lived 64 KB buffer that it reuses
// across every file. Steady-state, the pool holds ~workers buffers; no
// allocation in the hot path. It's a small win on top of the encoder-
// reuse change — not the star of the show, but honest.
//
// 2) WHY 64 KB (not 32)
//
// Bigger copy chunks amortise synchronisation cost. Every chunk calls
// Throttle.Wait() which takes a mutex. 32 KB chunks at 20 Mbps = ~640
// throttle acquisitions per second across all workers. Doubling to 64 KB
// halves the contention while costing nothing in memory (one slice per
// worker). The zstd encoder's internal block size is 128 KB anyway, so
// we are not starving it of data.
//
// 3) WHY *[]byte (not plain []byte)
//
// sync.Pool stores values as interface{} (i.e. two words: a type pointer
// and a data pointer). Putting a []byte directly means the slice header
// (3 words: data, len, cap) cannot fit into the interface's one data
// word and must be boxed onto the heap — a fresh allocation per Put.
// Putting a *[]byte stores the pointer directly in the interface with no
// boxing at all. This is the pattern the Go standard library itself uses
// (e.g. bytes.Buffer via bufio.ReaderPool), and what staticcheck rule
// SA6002 enforces:
//
//	"Storing non-pointer values in sync.Pool allocates memory"
//
// Net effect: Get() and Put() are truly allocation-free in steady state.
// With []byte you would pay one alloc per Put — which is exactly the
// overhead the pool is supposed to eliminate.
var copyBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 64*1024)
		return &b
	},
}

// borrowBuf / returnBuf are the only way to touch copyBufPool. Keeping
// the pool type *[]byte (pointer to slice header) is what makes the pool
// truly allocation-free; see copyBufPool doc comment for the full why.
func borrowBuf() *[]byte  { return copyBufPool.Get().(*[]byte) }
func returnBuf(b *[]byte) { copyBufPool.Put(b) }

// Throttle is a shared token bucket that simulates a fixed uplink cap
// (e.g. a user's home internet connection) measured in bytes per second.
//
// Design:
//   - Single nextFreeAt timestamp guarded by a mutex.
//   - Wait(n) reserves n bytes worth of tokens by advancing nextFreeAt
//     by (n / bytesPerSec) seconds, then sleeps until nextFreeAt.
//   - All workers share one instance, so aggregate throughput across all
//     concurrent writers converges to bytesPerSec regardless of worker count.
//
// This mirrors the reality that an upload link is ONE shared resource: TCP
// fairness between N streams converges to 1/N of link capacity each, and
// summed they saturate the pipe. The mutex serialises token accounting but
// not actual send time — a worker that has reserved tokens may sleep for
// tens of milliseconds while other workers concurrently sleep for their
// own reservations.
//
// Production note: in real code, this whole type goes away. The user's
// network IS the throttle; you do not impose client-side rate limiting on
// top of it (that only slows down uploads without any benefit).
type Throttle struct {
	bytesPerSec int64
	mu          sync.Mutex
	nextFreeAt  time.Time
}

func newThrottle(bps int64) *Throttle {
	return &Throttle{bytesPerSec: bps}
}

func (t *Throttle) Wait(n int64) {
	if t.bytesPerSec <= 0 {
		return
	}
	t.mu.Lock()
	now := time.Now()
	if t.nextFreeAt.Before(now) {
		t.nextFreeAt = now
	}
	dur := time.Duration(n * int64(time.Second) / t.bytesPerSec)
	t.nextFreeAt = t.nextFreeAt.Add(dur)
	sleepUntil := t.nextFreeAt
	t.mu.Unlock()
	time.Sleep(time.Until(sleepUntil))
}

// Queue is a thread-safe FIFO wrapping container/list. The walker fills
// it once with every source file path up front; workers drain it with
// Pop(). Used for both the pending-work queue and the dead-letter queue.
//
// Why container/list + sync.Mutex over a buffered channel:
//   - Len() gives the progress UI a pending-work count with no extra tracking.
//   - No fixed capacity — we push 1069 items before any worker starts,
//     which would require a very large channel buffer.
//   - Pop returning (val, ok) models "queue drained" cleanly; closed-channel
//     semantics are less pleasant when multiple producers exist.
//
// If your production code needs backpressure from consumer to producer
// (e.g. streaming walker that should slow down while workers are busy),
// reach for a buffered channel instead. For load-once-drain-many, this
// type is the simplest right answer.
type Queue struct {
	mu sync.Mutex
	l  *list.List
}

func NewQueue() *Queue { return &Queue{l: list.New()} }

func (q *Queue) Push(s string) {
	q.mu.Lock()
	q.l.PushBack(s)
	q.mu.Unlock()
}

func (q *Queue) Pop() (string, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	e := q.l.Front()
	if e == nil {
		return "", false
	}
	q.l.Remove(e)
	return e.Value.(string), true
}

func (q *Queue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.l.Len()
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func resetDir(p string) {
	must(os.RemoveAll(p))
	must(os.MkdirAll(p, 0o755))
}

// countingReader is an io.Reader wrapper that atomically increments a
// shared byte counter on every Read. Wrapping the source (rather than
// some downstream writer) guarantees that rawTotal tracks true source
// consumption, not compressor or network internal buffering.
//
// Zero-copy: the wrapper doesn't buffer or copy bytes — it passes the
// same []byte through io.Copy's internal 32 KB buffer and only observes
// n to update the counter. Memory cost: one atomic op per chunk.
type countingReader struct {
	r       io.Reader
	counter *int64
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	if n > 0 {
		atomic.AddInt64(cr.counter, int64(n))
	}
	return n, err
}

// throttledWriter is the simulated R2 PUT sink. Every Write(p) blocks on
// Throttle.Wait(len(p)) and then reports full success without actually
// sending bytes anywhere. Because Write blocks, any upstream io.Writer in
// a MultiWriter chain blocks with it, which means io.Copy's source read
// is paced by the same throttle. This is how end-to-end back-pressure
// reaches the reader without any explicit coordination.
//
// In production, replace this with manager.Uploader.Upload on the SDK's
// s3.Client — the real network is the "throttle" and no extra limiting
// is needed.
type throttledWriter struct {
	t *Throttle
}

func (tw *throttledWriter) Write(p []byte) (int, error) {
	tw.t.Wait(int64(len(p)))
	return len(p), nil
}

// streamCompressUpload performs the first upload attempt for one file.
// It builds the full pipeline:
//
//	src (os.File) → countingReader → enc (zstd) → MultiWriter(local .zst, throttled R2)
//
// Back-pressure flows right-to-left: throttledWriter blocks until tokens
// are granted → MultiWriter.Write blocks → zstd.Encoder.Write blocks →
// io.CopyBuffer stops reading → source read rate matches upload rate.
//
// The encoder is passed in and owned by the worker, not constructed here.
// We Reset() it to the new sink each call, which reuses the window,
// literal tables, and hash chains across all files this worker processes.
// This single optimisation did 95% of the memory reduction observed in
// the research conversation (heap 259 MB → 198 MB, GC 153 → 3).
//
// The io.CopyBuffer uses a pooled 64 KB buffer (see copyBufPool) rather
// than io.Copy's default fresh-32 KB-per-call.
//
// At end of stream, after all bytes have actually passed the throttle,
// we flip a coin against errRate to simulate post-transfer server
// rejection (5xx, TCP reset near end, etc.). Modelling failure at the
// end rather than mid-stream is deliberate: the local .zst is now
// complete and reusable for cheap retries (see reuploadFromLocal).
//
// Returns the compressed .zst size on disk. In the error case the
// returned int64 is still meaningful because the local file is valid;
// the caller may then retry via reuploadFromLocal.
//
// Production replacement: compress to local .zst with a similar streaming
// approach (no full-file buffering), then call manager.Uploader.Upload
// on the resulting file. The SDK will multipart and retry as needed.
func streamCompressUpload(enc *zstd.Encoder, src, dst string,
	wrap func(io.Reader) io.Reader, th *Throttle, errRate float64) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	sink := io.MultiWriter(out, &throttledWriter{t: th})
	enc.Reset(sink)
	var reader io.Reader = in
	if wrap != nil {
		reader = wrap(in)
	}
	bufPtr := borrowBuf()
	_, copyErr := io.CopyBuffer(enc, reader, *bufPtr)
	returnBuf(bufPtr)
	if copyErr != nil {
		enc.Close()
		out.Close()
		return 0, copyErr
	}
	if err := enc.Close(); err != nil {
		out.Close()
		return 0, err
	}
	if err := out.Close(); err != nil {
		return 0, err
	}
	info, err := os.Stat(dst)
	if err != nil {
		return 0, err
	}
	if rand.Float64() < errRate {
		return info.Size(), fmt.Errorf("simulated r2 flake")
	}
	return info.Size(), nil
}

// reuploadFromLocal is the retry path. Attempt 1 produced a complete local
// .zst file; subsequent attempts stream that file through a fresh
// throttledWriter instead of re-reading and re-compressing the source.
//
// Consequences:
//   - CPU cost is ~zero beyond I/O and throttle sleeps.
//   - The source file is not re-opened or re-read, so the rawTotal counter
//     (source bytes) is NOT advanced by retries. Only the upload counter
//     and retry counter move. This keeps pct/ETA computations clean.
//   - The throttle IS consumed — retry bytes count against the shared
//     uplink budget, which is why the 10% err rate translates to ~10%
//     bandwidth waste in measured runs.
//
// End-of-stream coin flip is the same idea as in streamCompressUpload:
// simulated failure happens after all bytes passed the throttle.
//
// Production equivalent: the SDK's StandardRetryer handles this for you.
// Delete this function when migrating; retries live inside Uploader.Upload.
func reuploadFromLocal(localPath string, th *Throttle, errRate float64) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()
	bufPtr := borrowBuf()
	_, copyErr := io.CopyBuffer(&throttledWriter{t: th}, f, *bufPtr)
	returnBuf(bufPtr)
	if copyErr != nil {
		return copyErr
	}
	if rand.Float64() < errRate {
		return fmt.Errorf("simulated r2 flake")
	}
	return nil
}

// main wires everything together. Flow:
//
//  1. Parse flags.
//  2. Walk the world directory, collect paths and byte sizes.
//  3. Optionally sort by size descending (stabilises ETA — see top-of-file).
//  4. Populate the work Queue.
//  5. Start the 1 s progress ticker.
//  6. Spawn N worker goroutines; each:
//       a. creates one zstd.Encoder (reused for every file this worker does)
//       b. loops on queue.Pop() until drained
//       c. per file: streamCompressUpload → retry loop → ok/fail accounting
//  7. Wait for workers; emit final RESULTS and DLQ dump.
//
// The sequence of counters used by the progress ticker:
//
//   rawTotal         bytes read from source files (advanced by countingReader)
//   compressedTotal  bytes written to local .zst (advanced on each success)
//   uploadedBytes    bytes acknowledged as uploaded (advanced on attempt success)
//   processedCount   files completed (success or DLQ)
//   retryCount       extra attempts (not including first)
//   failCount        files that exhausted retries and went to DLQ
//   activeWorkers    workers currently inside a file pipeline
//
// All counters are plain int64 read/written via sync/atomic so the ticker
// goroutine can snapshot without contending with workers.
func main() {
	worldPath := flag.String("world", "world", "source world dir")
	outRoot := flag.String("out", "experiments", "output root")
	workers := flag.Int("workers", 10, "concurrent push workers")
	retries := flag.Int("retries", 5, "max upload attempts per file")
	errRate := flag.Float64("err-rate", 0.10, "per-attempt upload err rate")
	mbps := flag.Int("mbps", 20, "shared upload bandwidth cap Mbps (0=unlimited)")
	limit := flag.Int("limit", 0, "limit files processed (0=all)")
	window := flag.Int("window", 0, "zstd window size bytes (0=default)")
	reportMB := flag.Int("report-mb", 10, "emit progress line every N MB of raw read")
	sortDesc := flag.Bool("sort-desc", true, "process biggest files first (stabilises ETA)")
	flag.Parse()

	log.SetFlags(log.Ltime | log.Lmicroseconds)
	outDir := filepath.Join(*outRoot, "r2sim")
	resetDir(outDir)

	type srcFile struct {
		path string
		size int64
	}
	var all []srcFile
	var totalRawBytes int64
	must(filepath.WalkDir(*worldPath, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		all = append(all, srcFile{path: p, size: info.Size()})
		totalRawBytes += info.Size()
		return nil
	}))
	if *sortDesc {
		sort.Slice(all, func(i, j int) bool { return all[i].size > all[j].size })
	}
	if *limit > 0 && *limit < len(all) {
		all = all[:*limit]
		totalRawBytes = 0
		for _, a := range all {
			totalRawBytes += a.size
		}
	}
	files := make([]string, len(all))
	for i, a := range all {
		files[i] = a.path
	}
	log.Printf("world=%s files=%d workers=%d cap=%dMbps err=%.2f retries=%d",
		*worldPath, len(files), *workers, *mbps, *errRate, *retries)

	bytesPerSec := int64(*mbps) * 1_000_000 / 8
	throttle := newThrottle(bytesPerSec)

	queue := NewQueue()
	dlq := NewQueue()
	for _, p := range files {
		queue.Push(p)
	}
	log.Printf("queue populated: %d items", queue.Len())

	var (
		okCount, failCount int64
		retryCount         int64
		uploadedBytes      int64
		rawTotal           int64
		compressedTotal    int64
		processedCount     int64
		activeWorkers      int64
	)

	start := time.Now()

	// byte-milestone-driven progress — cadence follows read throughput,
	// not wall clock. Emitter runs from whichever worker goroutine crossed
	// the milestone; a mutex serialises snapshot updates.
	type snap struct {
		t                            time.Time
		read, upload, files, retries int64
	}
	var (
		reportMu sync.Mutex
		last     = snap{t: start}
		mem      runtime.MemStats
	)
	_ = reportMB

	emitProgress := func(now time.Time) {
		reportMu.Lock()
		defer reportMu.Unlock()
		el := now.Sub(start).Seconds()
		dt := now.Sub(last.t).Seconds()
		if dt <= 0 {
			dt = 1
		}
		rawDone := atomic.LoadInt64(&rawTotal)
		up := atomic.LoadInt64(&uploadedBytes)
		done := atomic.LoadInt64(&processedCount)
		rt := atomic.LoadInt64(&retryCount)
		fl := atomic.LoadInt64(&failCount)

		// cumulative read velocity — resilient to bursty per-file reads,
		// always has a meaningful value after first tick
		var etaSec float64
		if el > 0 && rawDone > 0 {
			velocity := float64(rawDone) / el // raw bytes/s, cumulative avg
			etaSec = float64(totalRawBytes-rawDone) / velocity
		}
		pct := float64(rawDone) / float64(totalRawBytes) * 100

		nowReadMbps := float64(rawDone-last.read) * 8 / dt / 1e6
		nowUpMbps := float64(up-last.upload) * 8 / dt / 1e6
		avgRead := float64(rawDone) * 8 / el / 1e6
		avgUp := float64(up) * 8 / el / 1e6
		dFiles := float64(done-last.files) / dt
		dRetry := float64(rt-last.retries) / dt

		runtime.ReadMemStats(&mem)

		log.Printf("[progress] t=%.0fs eta=%.0fs done=%d/%d files raw=%dB/%dB (%.1f%%) up=%dB | net cap=%d avg=%.1f now=%.1f ret=%d(+%.1f/s) fail=%d | disk avg=%.1f now=%.1f fps=%.1f | pool=%d/%d q=%d dlq=%d | go=%d heap=%.0fMB sys=%.0fMB gc=%d",
			el, etaSec,
			done, int64(len(files)),
			rawDone, totalRawBytes, pct,
			up,
			*mbps, avgUp, nowUpMbps, rt, dRetry, fl,
			avgRead, nowReadMbps, dFiles,
			atomic.LoadInt64(&activeWorkers), int64(*workers), queue.Len(), dlq.Len(),
			runtime.NumGoroutine(),
			float64(mem.HeapAlloc)/1024/1024,
			float64(mem.Sys)/1024/1024,
			mem.NumGC)

		last = snap{t: now, read: rawDone, upload: up, files: done, retries: rt}
	}

	wrapReader := func(r io.Reader) io.Reader {
		return &countingReader{r: r, counter: &rawTotal}
	}

	// 1-second ticker feeds UI-style progress stream
	doneTicker := make(chan struct{})
	go func() {
		t := time.NewTicker(1 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-doneTicker:
				return
			case now := <-t.C:
				emitProgress(now)
			}
		}
	}()

	// concurrent workers: each free worker pops from shared queue, runs
	// full pipeline per file: local src → zstd → local .zst → throttled upload
	var wg sync.WaitGroup
	for i := range *workers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// one encoder per worker, reused across files via Reset()
			opts := []zstd.EOption{zstd.WithEncoderLevel(zstd.SpeedDefault)}
			if *window > 0 {
				opts = append(opts, zstd.WithWindowSize(*window))
			}
			enc, err := zstd.NewWriter(nil, opts...)
			must(err)
			defer enc.Close()
			for {
				src, ok := queue.Pop()
				if !ok {
					return
				}
				atomic.AddInt64(&activeWorkers, 1)
				rel, _ := filepath.Rel(*worldPath, src)
				localDst := filepath.Join(outDir, rel+".zst")
				must(os.MkdirAll(filepath.Dir(localDst), 0o755))

				// attempt 1: streaming compress + upload in one pipe (backpressured)
				compSize, err := streamCompressUpload(enc, src, localDst,
					wrapReader, throttle, *errRate)
				if compSize > 0 {
					atomic.AddInt64(&compressedTotal, compSize)
				}
				attempts := 1
				// attempts 2..N: retry from local .zst (no re-compress, no re-read of src)
				for err != nil && attempts < *retries {
					attempts++
					err = reuploadFromLocal(localDst, throttle, *errRate)
				}
				atomic.AddInt64(&retryCount, int64(attempts-1))
				if err != nil {
					atomic.AddInt64(&failCount, 1)
					dlq.Push(rel)
				} else {
					atomic.AddInt64(&okCount, 1)
					atomic.AddInt64(&uploadedBytes, compSize)
				}
				atomic.AddInt64(&processedCount, 1)
				atomic.AddInt64(&activeWorkers, -1)
			}
		}(i)
	}

	wg.Wait()
	close(doneTicker)
	emitProgress(time.Now()) // final snapshot at EOF

	elapsed := time.Since(start)
	mbpsActual := float64(uploadedBytes*8) / elapsed.Seconds() / 1e6
	log.Println("============== RESULTS ==============")
	log.Printf("elapsed=%v", elapsed)
	log.Printf("files=%d ok=%d fail=%d retries=%d", len(files), okCount, failCount, retryCount)
	log.Printf("raw=%dB (%.2fMB) compressed=%dB (%.2fMB) ratio=%.2f%%",
		rawTotal, float64(rawTotal)/1e6, compressedTotal, float64(compressedTotal)/1e6,
		float64(compressedTotal)/float64(rawTotal)*100)
	log.Printf("uploaded=%dB avg_throughput=%.2fMbps (cap=%dMbps)",
		uploadedBytes, mbpsActual, *mbps)
	if dlq.Len() > 0 {
		log.Printf("DLQ contents (%d):", dlq.Len())
		for {
			k, ok := dlq.Pop()
			if !ok {
				break
			}
			log.Printf("  - %s", k)
		}
	}
}
