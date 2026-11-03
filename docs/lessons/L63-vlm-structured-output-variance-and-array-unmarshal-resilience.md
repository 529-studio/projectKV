# L63 — VLM Structured Output Variance and Array Unmarshal Resilience

## Context

During autonomous live chokepoint verification in `flood.Service`, the Gemini multimodal API intermittently wraps its structured output in a single-element JSON array rather than a bare JSON object:

```json
[
  {
    "traffic_severity": "CLEAR",
    "flood_severity": "DRY",
    "confidence": 0.95,
    "description": "Traffic is flowing freely with minimal vehicles visible on dry roads at the intersection."
  }
]
```

Even though the system prompt requests `Return ONLY the JSON object. No other text`, LLMs occasionally format valid JSON as a list. The Go backend unmarshaled directly into `var vj verdictJSON`, causing Go's standard library `json.Unmarshal` to fail with:
`cannot unmarshal array into Go value of type cvclient.verdictJSON`. This caused background chokepoint environmental analysis to abort abruptly.

## Root Cause

LLM structured output variance is an expected operational reality. Rigidly assuming the VLM will strictly produce a top-level JSON object without defensiveness creates fragile runtime failure modes in upstream pipelines.

## Guardrail

- **Defensive Unmarshal Fallback**: When unmarshaling LLM JSON verdicts into a target struct, provide an immediate fallback to `[]TargetStruct`. If an array is returned with `len > 0`, safely extract the primary verdict (`arr[0]`).
- **Explicit Failure for Empty Arrays**: If the LLM returns an empty array `[]`, reject with an explicit descriptive error (`gemini returned empty verdict array`) rather than degrading into nil pointer panics or silent empty structs.
- **End-to-End Regression Coverage**: Every VLM parsing path must be validated with tests covering markdown-fenced objects, raw objects, single-element arrays, markdown-fenced arrays, and empty arrays.
