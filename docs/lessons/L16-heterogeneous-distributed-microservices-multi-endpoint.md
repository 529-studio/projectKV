## Lesson 16: Heterogeneous Distributed Microservices & Multi-Endpoint Portainer Orchestration

- **Incident & Tracking:** Branch `feat/distributed-cv-cicd-pipeline`, Offloading CV Engine to `inference worker` worker VM.
- **Symptom:**
  Running OpenVINO neural network inference directly on staging edge hardware (`edge host`, Intel Celeron N2930, 4 cores, no AVX2) resulted in 425ms P50 latency per frame and caused severe resource contention, starving CPU and memory for the Go API server and Traefik reverse proxy during peak hotset cycles.
- **Root Cause:**
  Workload asymmetry between lightweight network I/O orchestration (Go backend) and compute-intensive tensor operations (OpenVINO CV inference). Deploying both into a single Docker Compose stack on an underpowered edge host forced CPU saturation.
- **Mandatory Takeaway:**
  1. **Decoupled Workload Topology**: Distribute microservices based on hardware affinity: keep lightweight network I/O on edge nodes (`edge host`) while offloading batch CV inference to compute-optimized worker nodes (`inference worker`, AMD Ryzen 7 6800HS) interconnected via a private WireGuard/Tailscale mesh.
  2. **Compose Profile Isolation**: Use Docker Compose `profiles: ["model-local"]` in `docker-compose.stg.yml` so that developer local workflows remain unified (`docker compose up`) without accidentally launching heavy inference containers on memory-constrained staging hosts.
  3. **Multi-Endpoint Portainer Orchestration in CI/CD**: In deployment automation (`docker-publish.yml`), route stack updates to their dedicated Portainer endpoints (`endpointId=3` for app services, `endpointId=4` for CV worker) and assert container health directly through the Portainer REST API.
  4. **Strict Cgroups & Disk Guard on Constrained Workers**: On quota-restricted worker hosts (e.g., 20 GB root volume), enforce strict container resource constraints (`cpus: "2.00"`, `memory: "2048M"`) and configure an automated Disk Guard daemon (`docker-disk-guard.sh`) to proactively prune builder cache and dangling images before root volumes exhaust.
