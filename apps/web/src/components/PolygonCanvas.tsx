'use client';

import React, { useCallback, useEffect, useRef, useState } from 'react';
import {
  MAX_ANNOTATIONS_PER_LABEL,
  MIN_POLYGON_POINTS,
  type PolygonAnnotation,
  type PolygonClass,
  type PolygonPoint,
} from '@/utils/labelerUtils';

/**
 * Monochromatic-friendly stroke tones per segmentation class. Colors follow the
 * existing map palette practice (distinct hues for data semantics, no emoji).
 */
export const POLYGON_CLASS_COLORS: Record<PolygonClass, string> = {
  roadway: '#94a3b8',
  water_puddle: '#3b82f6',
  motorcycle: '#eab308',
  car: '#f97316',
  bus: '#ef4444',
  truck: '#a855f7',
};

interface PolygonCanvasProps {
  imageUrl: string;
  imageAlt: string;
  annotations: PolygonAnnotation[];
  onChange: (next: PolygonAnnotation[]) => void;
  activeClass: PolygonClass;
  enabled: boolean;
  labels: {
    hint: string;
    polygonsLabel: string;
    empty: string;
    deleteBtn: string;
    autoBadge: string;
  };
}

interface DragState {
  annIndex: number;
  vertexIndex: number;
}

/**
 * SVG overlay polygon editor for segmentation ground truth (Issue #297,
 * ADR-011 D1). All coordinates are normalized [0, 1] via the viewBox trick,
 * and converted against the image natural size before persisting, so the
 * saved geometry is resolution-independent and matches the backend contract.
 */
export default function PolygonCanvas({
  imageUrl,
  imageAlt,
  annotations,
  onChange,
  activeClass,
  enabled,
  labels,
}: PolygonCanvasProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const svgRef = useRef<SVGSVGElement>(null);
  const [natural, setNatural] = useState<{ w: number; h: number }>({ w: 0, h: 0 });
  const [draft, setDraft] = useState<PolygonPoint[]>([]);
  const [drag, setDrag] = useState<DragState | null>(null);
  const [containerWidth, setContainerWidth] = useState(1);

  // Measure the rendered wrapper width for constant-pixel handle sizes.
  // ResizeObserver is the enhancement; getBoundingClientRect on mount, image
  // load, and window resize are the baseline — embedded browser views may not
  // deliver ResizeObserver callbacks, which previously froze handle geometry.
  const measureWidth = useCallback(() => {
    const el = containerRef.current;
    if (!el) return;
    const w = el.getBoundingClientRect().width;
    if (w > 0) setContainerWidth(w);
  }, []);

  useEffect(() => {
    measureWidth();
    if (typeof window !== 'undefined') {
      window.addEventListener('resize', measureWidth);
    }
    let observer: ResizeObserver | null = null;
    if (typeof ResizeObserver !== 'undefined') {
      observer = new ResizeObserver(() => measureWidth());
      if (containerRef.current) observer.observe(containerRef.current);
    }
    return () => {
      if (typeof window !== 'undefined') {
        window.removeEventListener('resize', measureWidth);
      }
      observer?.disconnect();
    };
  }, [measureWidth]);

  const vertexRadius = 7 / containerWidth;

  const toNormalized = useCallback((clientX: number, clientY: number): PolygonPoint => {
    const rect = svgRef.current?.getBoundingClientRect();
    if (!rect || rect.width <= 0 || rect.height <= 0) return [0, 0];
    const x = Math.max(0, Math.min(1, (clientX - rect.left) / rect.width));
    const y = Math.max(0, Math.min(1, (clientY - rect.top) / rect.height));
    return [Math.round(x * 1e6) / 1e6, Math.round(y * 1e6) / 1e6];
  }, []);

  const closeDraft = useCallback(() => {
    if (draft.length < MIN_POLYGON_POINTS) return;
    if (annotations.length >= MAX_ANNOTATIONS_PER_LABEL) return;
    onChange([...annotations, { class: activeClass, polygon: draft, source: 'human' }]);
    setDraft([]);
  }, [draft, annotations, activeClass, onChange]);

  // Enter closes the draft polygon; Escape discards it.
  useEffect(() => {
    if (draft.length === 0) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setDraft([]);
      } else if (e.key === 'Enter') {
        e.preventDefault();
        closeDraft();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [draft, closeDraft]);

  const handleSvgClick = (e: React.MouseEvent<SVGSVGElement>) => {
    if (!enabled || drag) return;
    if (annotations.length >= MAX_ANNOTATIONS_PER_LABEL) return;
    const pt = toNormalized(e.clientX, e.clientY);

    // Snap radius in normalized units, derived from the live rect so stale
    // container width state can never distort close-hit detection.
    const rect = svgRef.current?.getBoundingClientRect();
    const snapRadius = rect && rect.width > 0 ? 12 / rect.width : 0.02;

    // Clicking near the first vertex closes the polygon.
    if (draft.length >= MIN_POLYGON_POINTS) {
      const [fx, fy] = draft[0];
      const [px, py] = pt;
      if (Math.abs(px - fx) < snapRadius && Math.abs(py - fy) < snapRadius) {
        closeDraft();
        return;
      }
    }
    setDraft((d) => (d.length >= 64 ? d : [...d, pt]));
  };

  const handlePointerMove = (e: React.PointerEvent<SVGSVGElement>) => {
    if (!drag) return;
    const pt = toNormalized(e.clientX, e.clientY);
    onChange(
      annotations.map((ann, i) =>
        i === drag.annIndex
          ? { ...ann, polygon: ann.polygon.map((v, j) => (j === drag.vertexIndex ? pt : v)) }
          : ann
      )
    );
  };

  const deleteAnnotation = (index: number) => {
    onChange(annotations.filter((_, i) => i !== index));
  };

  const pointsAttr = (pts: PolygonPoint[]) => pts.map(([x, y]) => `${x},${y}`).join(' ');

  return (
    <div className="flex flex-col gap-2 w-full h-full items-center justify-center">
      <div ref={containerRef} className="relative max-h-full max-w-full">
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src={imageUrl}
          alt={imageAlt}
          onLoad={(e) => {
            const img = e.currentTarget;
            setNatural({ w: img.naturalWidth, h: img.naturalHeight });
            measureWidth();
          }}
          className="max-h-full max-w-full object-contain select-none"
          draggable={false}
        />
        <svg
          ref={svgRef}
          viewBox="0 0 1 1"
          preserveAspectRatio="none"
          className={`absolute inset-0 w-full h-full ${enabled ? 'cursor-crosshair' : 'pointer-events-none'}`}
          onClick={handleSvgClick}
          onPointerMove={handlePointerMove}
          onPointerUp={() => setDrag(null)}
          onPointerLeave={() => setDrag(null)}
        >
          {annotations.map((ann, i) => (
            <g key={i}>
              <polygon
                points={pointsAttr(ann.polygon)}
                fill={POLYGON_CLASS_COLORS[ann.class]}
                fillOpacity={0.18}
                stroke={POLYGON_CLASS_COLORS[ann.class]}
                strokeWidth={2}
                vectorEffect="non-scaling-stroke"
              />
              {enabled &&
                ann.polygon.map((v, j) => (
                  <circle
                    key={j}
                    cx={v[0]}
                    cy={v[1]}
                    r={vertexRadius}
                    fill="#18181b"
                    stroke={POLYGON_CLASS_COLORS[ann.class]}
                    strokeWidth={2}
                    vectorEffect="non-scaling-stroke"
                    className="cursor-move"
                    onPointerDown={(e) => {
                      e.stopPropagation();
                      setDrag({ annIndex: i, vertexIndex: j });
                    }}
                  />
                ))}
            </g>
          ))}

          {draft.length > 0 && (
            <g>
              {draft.length >= 2 && (
                <polygon
                  points={pointsAttr(draft)}
                  fill={POLYGON_CLASS_COLORS[activeClass]}
                  fillOpacity={0.12}
                  stroke={POLYGON_CLASS_COLORS[activeClass]}
                  strokeWidth={2}
                  strokeDasharray="4 3"
                  vectorEffect="non-scaling-stroke"
                />
              )}
              {draft.map((v, j) => (
                <circle
                  key={j}
                  cx={v[0]}
                  cy={v[1]}
                  r={j === 0 && draft.length >= MIN_POLYGON_POINTS ? vertexRadius * 1.5 : vertexRadius}
                  fill={j === 0 && draft.length >= MIN_POLYGON_POINTS ? POLYGON_CLASS_COLORS[activeClass] : '#18181b'}
                  stroke={POLYGON_CLASS_COLORS[activeClass]}
                  strokeWidth={2}
                  vectorEffect="non-scaling-stroke"
                />
              ))}
            </g>
          )}
        </svg>
      </div>

      <div className="w-full flex flex-col gap-1 text-[11px] font-mono text-zinc-400">
        <div className="flex items-center justify-between">
          <span className="uppercase tracking-wider">{labels.polygonsLabel}</span>
          {enabled && <span className="text-zinc-600">{labels.hint}</span>}
        </div>
        {annotations.length === 0 ? (
          <span className="text-zinc-600">{labels.empty}</span>
        ) : (
          <div className="flex flex-wrap gap-1.5">
            {annotations.map((ann, i) => (
              <span
                key={i}
                className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded border border-zinc-700 bg-zinc-900 text-zinc-300"
              >
                <span
                  aria-hidden
                  className="inline-block w-2 h-2 rounded-full"
                  style={{ backgroundColor: POLYGON_CLASS_COLORS[ann.class] }}
                />
                {ann.class} ({ann.polygon.length})
                {ann.source === 'sam_prelabel' && (
                  <span className="text-zinc-500">{labels.autoBadge}</span>
                )}
                {enabled && (
                  <button
                    type="button"
                    onClick={() => deleteAnnotation(i)}
                    className="text-zinc-500 hover:text-red-400 transition"
                  >
                    {labels.deleteBtn}
                  </button>
                )}
              </span>
            ))}
          </div>
        )}
        {natural.w > 0 && <span className="sr-only">natural {natural.w}x{natural.h}</span>}
      </div>
    </div>
  );
}
