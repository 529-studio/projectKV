## Lesson 6: Single-Threaded HTTP Servers vs. Concurrent Go Dispatchers (Queue Buildup & BrokenPipeError)

- **Incident & Tracking:** Issue #167, Pull Request #168 (`fix/167-cvengine-threading-httpserver`).
- **Symptom:**
  During hotset analysis cycles, `go-server` logged recurrent timeout failures:
  ```
  WARN analysis failed, degrading to unknown camera=... error="cv-engine: POST /predict: Post \"http://localhost:8000/predict\": context deadline exceeded"
  ```
  Concurrently, `cv-engine` logged unhandled socket error tracebacks:
  ```
  BrokenPipeError: [Errno 32] Broken pipe
  ```
- **Root Cause:**
  `services/cv-engine/server.py` instantiated the default single-threaded `http.server.HTTPServer`. Meanwhile, `go-server`'s `hotset.Scheduler` dispatched up to 10 parallel goroutines (`HotsetMaxParallel = 10`) with a 10s per-call context timeout (`context.WithTimeout(ctx, 10*time.Second)`).  
  Because `HTTPServer` processed incoming requests strictly sequentially, requests piled up in the OS TCP backlog. Later requests in the batch exceeded the 10s client deadline while waiting in the queue. The Go client aborted the context and closed the TCP connection. When Python finally dequeued the socket and called `self.wfile.write(body)`, writing to the closed connection raised `BrokenPipeError: [Errno 32]`. Python's `BaseHTTPRequestHandler` did not catch `BrokenPipeError`, dumping an unhandled stack trace to `stderr`.
- **Mandatory Takeaway:**
  1. **Threaded Microservice Servers**: Any internal service receiving concurrent requests from Go worker pools MUST use multi-threaded or asynchronous server architectures (`ThreadingHTTPServer`, FastAPI/Uvicorn, or worker pools) rather than single-threaded sequential servers.
  2. **Defensive Socket Error Handling**: Network services must catch `(BrokenPipeError, ConnectionResetError)` on client write operations to gracefully handle client timeouts and connection drops without dumping traceback noise.
  3. **Inference Concurrency Verification**: Verify runtime thread-safety (such as OpenVINO `create_infer_request()`) under multi-threaded load to ensure parallelism does not introduce race conditions or segfaults.
