/** Inlined from phuongFormatters to avoid ESM/tsc extension-resolution conflict. */
function extractCorridor(displayName?: string, title?: string): string {
  const raw = (displayName || title || '').trim();
  if (!raw) return 'Khác';
  const parts = raw.split(/\s+[-–—−]\s+/);
  return parts[0].trim() || raw;
}

export interface SequenceCamera {
  id: string;
  title?: string;
  displayName?: string;
  road_key?: string | null;
  road_keys?: string[];
  road_name?: string | null;
  latitude?: number;
  longitude?: number;
  isOnline?: boolean;
  duplicateOf?: string;
  isGhost?: boolean;
  status?: string;
}

export interface CameraSequenceParams<T extends SequenceCamera = SequenceCamera> {
  currentCamera: T | null;
  allCameras: T[];
  pinnedCameras: T[];
  selectedCorridor: string | null;
  lang: 'vi' | 'en';
}

export interface CameraSequenceResult<T extends SequenceCamera = SequenceCamera> {
  prevCamera: T | null;
  nextCamera: T | null;
  currentIndex: number;
  totalCount: number;
  contextLabel: string;
}

/**
 * Resolves sequential adjacent cameras (< and >) based on contextual priority:
 * 1. Pinned dock list: If the current camera is in pinned list, navigate through pinned cameras.
 * 2. Active corridor: If selectedCorridor is set, navigate within that corridor.
 * 3. Same street / corridor auto-grouping: For unpinned cameras, find all cameras on the same street (e.g. Điện Biên Phủ).
 */
export function getCameraSequence<T extends SequenceCamera = SequenceCamera>({
  currentCamera,
  allCameras,
  pinnedCameras,
  selectedCorridor,
  lang,
}: CameraSequenceParams<T>): CameraSequenceResult<T> {
  if (!currentCamera) {
    return {
      prevCamera: null,
      nextCamera: null,
      currentIndex: -1,
      totalCount: 0,
      contextLabel: '',
    };
  }

  // 1. Pinned Dock context
  const isPinned = pinnedCameras.some((c) => c.id === currentCamera.id);
  if (isPinned && pinnedCameras.length > 0) {
    const idx = pinnedCameras.findIndex((c) => c.id === currentCamera.id);
    if (idx !== -1) {
      const prevCamera = idx > 0 ? pinnedCameras[idx - 1] : null;
      const nextCamera = idx < pinnedCameras.length - 1 ? pinnedCameras[idx + 1] : null;
      const labelPrefix = lang === 'vi' ? 'Ghim' : 'Pinned';
      return {
        prevCamera,
        nextCamera,
        currentIndex: idx,
        totalCount: pinnedCameras.length,
        contextLabel: `${labelPrefix}: ${idx + 1}/${pinnedCameras.length}`,
      };
    }
  }

  // 2. Explicit selected corridor context
  if (selectedCorridor) {
    const corridorCams = allCameras.filter(
      (c) =>
        extractCorridor(c.displayName, c.title) === selectedCorridor ||
        c.road_name === selectedCorridor ||
        c.road_key === selectedCorridor
    );
    corridorCams.sort((a, b) =>
      (a.displayName || a.title || '').localeCompare(b.displayName || b.title || '', 'vi')
    );
    const idx = corridorCams.findIndex((c) => c.id === currentCamera.id);
    if (idx !== -1 && corridorCams.length > 1) {
      const prevCamera = idx > 0 ? corridorCams[idx - 1] : null;
      const nextCamera = idx < corridorCams.length - 1 ? corridorCams[idx + 1] : null;
      return {
        prevCamera,
        nextCamera,
        currentIndex: idx,
        totalCount: corridorCams.length,
        contextLabel: `${selectedCorridor}: ${idx + 1}/${corridorCams.length}`,
      };
    }
  }

  // 3. Fallback: Automatic street corridor grouping from camera name
  const streetName = extractCorridor(currentCamera.displayName, currentCamera.title);
  if (streetName && streetName !== 'Khác') {
    const streetCams = allCameras.filter(
      (c) => extractCorridor(c.displayName, c.title) === streetName
    );
    streetCams.sort((a, b) =>
      (a.displayName || a.title || '').localeCompare(b.displayName || b.title || '', 'vi')
    );
    const idx = streetCams.findIndex((c) => c.id === currentCamera.id);
    if (idx !== -1 && streetCams.length > 1) {
      const prevCamera = idx > 0 ? streetCams[idx - 1] : null;
      const nextCamera = idx < streetCams.length - 1 ? streetCams[idx + 1] : null;
      return {
        prevCamera,
        nextCamera,
        currentIndex: idx,
        totalCount: streetCams.length,
        contextLabel: `${streetName}: ${idx + 1}/${streetCams.length}`,
      };
    }
  }

  // Single camera or no sequence
  return {
    prevCamera: null,
    nextCamera: null,
    currentIndex: 0,
    totalCount: 1,
    contextLabel: '',
  };
}
