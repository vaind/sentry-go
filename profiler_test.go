package sentry

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProfilerCollection(t *testing.T) {
	start := time.Now()
	stopFn := startProfiling()
	doWorkFor(35 * time.Millisecond)
	elapsed := time.Since(start)
	result := stopFn()
	require.GreaterOrEqual(t, result.startTime, start)
	require.Less(t, result.startTime, start.Add(elapsed))
	require.NotNil(t, result)
	validateProfile(t, result.trace, elapsed)
}

func TestProfilerCollectsOnStart(t *testing.T) {
	start := time.Now()
	result := startProfiling()()
	require.GreaterOrEqual(t, result.startTime, start)
	require.LessOrEqual(t, result.startTime, time.Now())
	require.NotNil(t, result)
	validateProfile(t, result.trace, time.Since(start))
}

func TestProfilerPanicDuringStartup(t *testing.T) {
	testProfilerPanic = -1
	testProfilerPanickedWith = nil
	stopFn := startProfiling()
	doWorkFor(35 * time.Millisecond)
	result := stopFn()
	require.Nil(t, result)
	require.Equal(t, "This is an expected panic in profilerGoroutine() during tests", testProfilerPanickedWith.(string))
}

func TestProfilerPanicOnTick(t *testing.T) {
	testProfilerPanic = 10_000
	testProfilerPanickedWith = nil
	start := time.Now()
	stopFn := startProfiling()
	doWorkFor(35 * time.Millisecond)
	elapsed := time.Since(start)
	result := stopFn()
	require.Equal(t, "This is an expected panic in Profiler.OnTick() during tests", testProfilerPanickedWith.(string))
	require.NotNil(t, result)
	validateProfile(t, result.trace, elapsed)
}

func TestProfilerPanicOnTickDirect(t *testing.T) {
	var require = require.New(t)

	testProfilerPanic = 1
	profiler := newProfiler()
	time.Sleep(time.Millisecond)
	// This is handled by the profiler goroutine and stops the profiler.
	require.Panics(profiler.onTick)
	require.Empty(profiler.trace.Samples)

	profiler.onTick()
	require.NotEmpty(profiler.trace.Samples)
}

func doWorkFor(duration time.Duration) {
	start := time.Now()
	for time.Since(start) < duration {
		_ = findPrimeNumber(1000)
	}
}

//nolint:unparam
func findPrimeNumber(n int) int {
	count := 0
	a := 2
	for count < n {
		b := 2
		prime := true // to check if found a prime
		for b*b <= a {
			if a%b == 0 {
				prime = false
				break
			}
			b++
		}
		if prime {
			count++
		}
		a++
	}
	return a - 1
}

func validateProfile(t *testing.T, trace *profileTrace, duration time.Duration) {
	var require = require.New(t)
	require.NotNil(trace)
	require.NotEmpty(trace.Samples)
	require.NotEmpty(trace.Stacks)
	require.NotEmpty(trace.Frames)
	require.NotEmpty(trace.ThreadMetadata)

	for _, sample := range trace.Samples {
		require.GreaterOrEqual(sample.ElapsedSinceStartNS, uint64(0))
		require.GreaterOrEqual(uint64(duration.Nanoseconds()), sample.ElapsedSinceStartNS)
		require.GreaterOrEqual(sample.StackID, 0)
		require.Less(sample.StackID, len(trace.Stacks))
		require.Contains(trace.ThreadMetadata, strconv.Itoa(int(sample.ThreadID)))
	}

	for _, thread := range trace.ThreadMetadata {
		require.NotEmpty(thread.Name)
	}

	for _, frame := range trace.Frames {
		require.NotEmpty(frame.Function)
		require.Greater(len(frame.AbsPath)+len(frame.Filename), 0)
		require.Greater(frame.Lineno, 0)
	}
}

func TestProfilerSamplingRate(t *testing.T) {
	var require = require.New(t)

	start := time.Now()
	stopFn := startProfiling()
	doWorkFor(500 * time.Millisecond)
	elapsed := time.Since(start)
	result := stopFn()

	require.NotEmpty(result.trace.Samples)
	var samplesByThread = map[uint64]uint64{}

	for _, sample := range result.trace.Samples {
		require.GreaterOrEqual(uint64(elapsed.Nanoseconds()), sample.ElapsedSinceStartNS)

		if prev, ok := samplesByThread[sample.ThreadID]; ok {
			// We can only verify the lower bound because the profiler callback may be scheduled less often than
			// expected, for example due to system ticker accuracy.
			// See https://stackoverflow.com/questions/70594795/more-accurate-ticker-than-time-newticker-in-go-on-macos
			// or https://github.com/golang/go/issues/44343
			require.Greater(sample.ElapsedSinceStartNS, prev)
		} else {
			// First sample should come in before the defined sampling rate.
			require.Less(sample.ElapsedSinceStartNS, uint64(profilerSamplingRate.Nanoseconds()))
		}
		samplesByThread[sample.ThreadID] = sample.ElapsedSinceStartNS
	}
}

// Benchmark results (run without executing which mess up results)
// $ go test -run=^$ -bench "BenchmarkProfiler*"
//
// goos: windows
// goarch: amd64
// pkg: github.com/getsentry/sentry-go
// cpu: 12th Gen Intel(R) Core(TM) i7-12700K
// BenchmarkProfilerStartStop-20                      38008             31072 ns/op           20980 B/op        108 allocs/op
// BenchmarkProfilerOnTick-20                         65700             18065 ns/op             260 B/op          4 allocs/op
// BenchmarkProfilerCollect-20                        67063             16907 ns/op               0 B/op          0 allocs/op
// BenchmarkProfilerProcess-20                      2296788               512.9 ns/op           268 B/op          4 allocs/op
// BenchmarkProfilerOverheadBaseline-20                 192           6250525 ns/op
// BenchmarkProfilerOverheadWithProfiler-20             187           6249490 ns/op

func BenchmarkProfilerStartStop(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		stopFn := startProfiling()
		_ = stopFn()
	}
}

func BenchmarkProfilerOnTick(b *testing.B) {
	profiler := newProfiler()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		profiler.onTick()
	}
}

func BenchmarkProfilerCollect(b *testing.B) {
	profiler := newProfiler()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = profiler.collectRecords()
	}
}

func BenchmarkProfilerProcess(b *testing.B) {
	profiler := newProfiler()
	records := profiler.collectRecords()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		profiler.processRecords(uint64(i), records)
	}
}

func doHardWork() {
	_ = findPrimeNumber(10000)
}

func BenchmarkProfilerOverheadBaseline(b *testing.B) {
	for i := 0; i < b.N; i++ {
		doHardWork()
	}
}

func BenchmarkProfilerOverheadWithProfiler(b *testing.B) {
	stopFn := startProfiling()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doHardWork()
	}
	b.StopTimer()
	stopFn()
}