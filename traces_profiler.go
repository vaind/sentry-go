package sentry

func (span *Span) startProfiling() {
	var sampleRate = span.clientOptions().ProfilesSampleRate
	switch {
	case sampleRate < 0.0 || sampleRate > 1.0:
		Logger.Printf("Skipping transaction profiling: ProfilesSampleRate out of range [0.0, 1.0]: %f", sampleRate)
	case sampleRate == 0.0 || rng.Float64() >= sampleRate:
		Logger.Printf("Skipping transaction profiling: ProfilesSampleRate is: %f", sampleRate)
	default:
		span.profiler = &_transactionProfiler{
			stopFunc: startProfiling(),
		}
	}
}

type transactionProfiler interface {
	Finish(span *Span, event *Event) *profileInfo
}

type _transactionProfiler struct {
	stopFunc func() *profilerResult
}

// Finish implements transactionProfiler.
func (tp *_transactionProfiler) Finish(span *Span, event *Event) *profileInfo {
	result := tp.stopFunc()
	info := &profileInfo{
		Version:     "1",
		Environment: event.Environment,
		EventID:     uuid(),
		Platform:    "go",
		Release:     event.Release,
		Timestamp:   result.startTime,
		Trace:       result.trace,
		Transaction: profileTransaction{
			// TODO capture the calling goroutine ID. It is currently not exposed by the runtime but we can
			// use the runtime.Stack() function to get the ID from the stack trace, e.g. by capturing the first sample
			// synchronously in the calling routine.
			ActiveThreadID: 0,
			DurationNS:     uint64(span.EndTime.Sub(span.StartTime).Nanoseconds()),
			ID:             "", // Event ID not available here yet
			Name:           span.Name,
			TraceID:        span.TraceID.String(),
		},
	}
	if len(info.Transaction.Name) == 0 {
		// Name is required by Relay so use the operation name if the span name is empty.
		info.Transaction.Name = span.Op
	}
	if runtimeContext, ok := event.Contexts["runtime"]; ok {
		if value, ok := runtimeContext["name"]; !ok {
			info.Runtime.Name = value.(string)
		}
		if value, ok := runtimeContext["version"]; !ok {
			info.Runtime.Version = value.(string)
		}
	}
	if osContext, ok := event.Contexts["os"]; ok {
		if value, ok := osContext["name"]; !ok {
			info.OS.Name = value.(string)
		}
	}
	if deviceContext, ok := event.Contexts["device"]; ok {
		if value, ok := deviceContext["arch"]; !ok {
			info.Device.Architecture = value.(string)
		}
	}
	return info
}