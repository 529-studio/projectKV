import { Camera } from '../hooks/useCameras';
import { API_ENDPOINTS } from '../config/constants';

interface PinnedDockProps {
  pinnedCameras: Camera[];
  
  onUnpin: (id: string) => void;
  onClear: () => void;
}

export default function PinnedDock({ pinnedCameras, onUnpin, onClear }: PinnedDockProps) {
  if (pinnedCameras.length === 0) return null;

  return (
    <div className="absolute bottom-0 left-0 right-0 z-[800] bg-zinc-950/95 border-t border-zinc-800 shadow-2xl backdrop-blur-md">
      <div className="flex items-center justify-between px-3 py-1.5 bg-zinc-900 border-b border-zinc-800 font-mono text-xs select-none">
        <div className="flex items-center gap-2">
          <span className="font-bold text-white tracking-wide">DOCK THEO DÕI</span>
          <span className="bg-zinc-950 border border-zinc-700 text-zinc-300 px-1.5 py-0.5 rounded">
            [{pinnedCameras.length} CAM]
          </span>
        </div>
        <button 
          onClick={onClear}
          className="px-2 py-0.5 bg-zinc-950 border border-zinc-700 hover:border-zinc-400 text-zinc-300 hover:text-white rounded transition"
        >
          [XÓA HẾT]
        </button>
      </div>
      <div className="flex gap-2 p-2 overflow-x-auto h-[120px]">
        {pinnedCameras.map(cam => {
          
          return (
            <div key={cam.id} className="relative min-w-[160px] h-full bg-black border border-zinc-800 rounded group flex-shrink-0">
              <img 
                src={`http://localhost:3000${API_ENDPOINTS.SNAPSHOT(cam.id)}?t=${Date.now()}`}
                className="w-full h-full object-cover rounded opacity-80 group-hover:opacity-100 transition-opacity"
                alt={cam.title}
              />
              <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/90 to-transparent p-2 text-[10px] font-mono text-white truncate">
                {cam.displayName || cam.title}
              </div>
              <button 
                onClick={() => onUnpin(cam.id)}
                className="absolute top-1 right-1 w-5 h-5 bg-red-500/80 hover:bg-red-500 text-white rounded flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity"
              >
                ×
              </button>
            </div>
          );
        })}
      </div>
    </div>
  );
}
