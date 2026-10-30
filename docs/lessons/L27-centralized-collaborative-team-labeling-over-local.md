## Lesson 27: Centralized Collaborative Team Labeling Over Local Synchronization & Cloud Storage Justification

- **Incident & Tracking:** Issue #214, Pull Request for S5-T4 (`[infra + web] S5-T4 — R2 blob storage for archiver + shared team labeler`).
- **Symptom:**
  Collaborative ground-truth evaluation and training set annotation was friction-prone: team members had to manually synchronize JPEG proof frames from server disk to their local workstations via ad-hoc scripts or SFTP. Furthermore, accessing Cloudflare R2 public edge URLs (`https://file.529studio.site`) required navigating Cloudflare Access Zero Trust authentication walls, preventing smooth browser-based frame rendering for evaluation workflows.
- **Root Cause:**
  1. **Storage Abstraction Gap**: The `storage.Storage` interface only exposed `Put`, `Get`, `Delete`, and `Exists`, lacking a `List(ctx, prefix)` method. The server could write snapshots but could not enumerate partition dates or snapshot catalogs dynamically.
  2. **Zero Trust Access Friction**: Public CDN endpoints behind corporate Zero Trust redirects cannot be consumed as direct `<img>` tags by collaborative web tools without complex cookie sessions.
- **Mandatory Takeaway & Design Principles:**
  1. **Dual-Driver Listing Abstraction**: Extended `storage.Storage` with `List(ctx, prefix) ([]Item, error)` implemented for both `LocalStorage` (directory walk with traversal protection) and `R2Storage` (`s3.ListObjectsV2` pagination), preserving complete offline local dev/test autonomy.
  2. **Secure Proxy Streaming Route**: Implemented `GET /api/archiver/snapshots/raw?key=...` with strict path traversal validation (`ErrInvalidPath`), streaming JPEG binaries directly to authorized web clients with immutable caching headers.
  3. **Monochromatic Collaborative Web Labeler**: Built `apps/web/src/app/labeler` with standard rapid-fire keyboard shortcuts (`1-3` flood, `Q-W-E-R` traffic, `A-S-D` weather, `Z-X-C` time of day, `Enter` save/next), zero emojis, and bilingual support (`vi` default), saving ground truth directly to cloud storage.
  4. **Architecture & Scope Justification Rule**: Adoption of Cloudflare R2 blob storage is explicitly justified to enable centralized collaborative team labeling and eliminate manual synchronization friction, **NOT** because the system has breached the ADR-004 §D7 disk capacity threshold (100 MB / 5,000 files).
