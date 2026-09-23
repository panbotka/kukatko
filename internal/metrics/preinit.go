package metrics

// A labelled family exports nothing until its first observation, so on an idle
// instance a dashboard could not tell "nothing failed" from "the instrumentation
// is broken". The methods below create every label combination the instance can
// produce at zero, at startup, from the one place that knows the combinations:
// the job types come from the worker's handler registry, the operations from the
// embeddings client, the renditions from the configured encoding plan. Each is a
// WithLabelValues call per combination, so calling one twice is harmless.

// InitJobTypes pre-initialises the job families for every type in jobTypes: the
// started counter per type, and the finished counter and the duration histogram
// per type and outcome.
func (r *Registry) InitJobTypes(jobTypes []string) {
	for _, jobType := range jobTypes {
		r.jobsStarted.WithLabelValues(jobType)
		for _, outcome := range jobOutcomes {
			r.jobsFinished.WithLabelValues(jobType, outcome)
			r.jobDuration.WithLabelValues(jobType, outcome)
		}
	}
}

// InitEmbeddingOperations pre-initialises the embeddings call-duration histogram
// for every operation in operations and every call outcome.
//
// The reachability gauge is deliberately left alone: it has no honest zero. A
// target that has not been probed yet is unknown, not offline, and publishing 0
// would fire an outage alert on every restart until the first probe lands.
func (r *Registry) InitEmbeddingOperations(operations []string) {
	for _, operation := range operations {
		for _, outcome := range callOutcomes {
			r.embeddingDuration.WithLabelValues(operation, outcome)
		}
	}
}

// InitVideoEncode pre-initialises the per-rendition streaming-encode duration
// histogram and output-bytes counter for every rendition in renditions and every
// encode outcome. Call it only when streaming is enabled: an instance that
// encodes nothing produces no rendition label at all.
func (r *Registry) InitVideoEncode(renditions []string) {
	for _, rendition := range renditions {
		for _, outcome := range callOutcomes {
			r.encodeDuration.WithLabelValues(rendition, outcome)
			r.encodeOutputBytes.WithLabelValues(rendition, outcome)
		}
	}
}
