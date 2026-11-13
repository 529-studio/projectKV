package hotset

// RecordableObservation reports whether v is a genuine traffic observation that
// belongs in the continuous Hot Set history consumed by the forecast feature
// store and the coverage endpoint (ADR-012 D2).
//
// Three exclusion rules, each guarding against a distinct data-quality defect:
//
//   - Stale or frozen verdicts are excluded. Replaying an unchanged frame as a
//     fresh observation would poison lag and rolling features with fabricated
//     variation (a 60-90s refresh source legitimately repeats a frame, but the
//     FrozenDetector already tolerates that; a verdict it does flag is a feed
//     that is genuinely not advancing).
//   - UNKNOWN verdicts are excluded. An UNKNOWN is the absence of a
//     measurement: a dead feed, an open breaker, or an exhausted analyzer
//     budget. Persisting it as a zero-saturation observation would turn an
//     outage into "free-flowing traffic" in training data.
//   - Verdicts that carry no PCU/saturation series are excluded, unless the
//     source is the PCU-computing engine (onnx). A VLM-only label has no
//     continuous series to record.
//
// It is exported because the production fan-out in cmd/server and the 24h
// coverage verification simulation must share one definition of "recordable";
// a test that reimplemented this gate could pass while production silently
// dropped the same observations.
func RecordableObservation(v Verdict) bool {
	if v.Stale {
		return false
	}
	if v.Severity == SeverityUnknown {
		return false
	}
	return v.Source == "onnx" || v.PCUTotal > 0 || v.SaturationIndex > 0
}
