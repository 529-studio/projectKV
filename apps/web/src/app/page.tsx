'use client';
import dynamic from 'next/dynamic';
import { useState, useMemo } from 'react';
import { useCameras, Camera } from '../hooks/useCameras';
import { useTraffic } from '../hooks/useTraffic';
import LandingOverlay from '../components/LandingOverlay';
import CameraModal from '../components/CameraModal';
import PinnedDock from '../components/PinnedDock';
import MapLegend from '../components/MapLegend';
import MobileCameraDrawer from '../components/MobileCameraDrawer';

const MapComponent = dynamic(() => import('../components/MapComponent'), { ssr: false });

export default function Home() {
  const { cameras, loading, error } = useCameras();
  const { verdicts } = useTraffic();
  
  const [selectedCamera, setSelectedCamera] = useState<Camera | null>(null);
  const [searchTerm, setSearchTerm] = useState('');
  const [pinnedCameraIds, setPinnedCameraIds] = useState<string[]>([]);
  const [legendOpen, setLegendOpen] = useState(false);
  const [mobileDrawerOpen, setMobileDrawerOpen] = useState(false);

  const filteredCameras = useMemo(() => {
    if (!searchTerm) return cameras;
    const lower = searchTerm.toLowerCase();
    return cameras.filter(c => 
      (c.displayName || c.title || '').toLowerCase().includes(lower) || 
      (c.zone || '').toLowerCase().includes(lower)
    );
  }, [cameras, searchTerm]);

  const pinnedCameras = useMemo(() => {
    return pinnedCameraIds.map(id => cameras.find(c => c.id === id)).filter(Boolean) as Camera[];
  }, [pinnedCameraIds, cameras]);

  const togglePin = (camId: string) => {
    setPinnedCameraIds(prev => 
      prev.includes(camId) ? prev.filter(id => id !== camId) : [...prev, camId]
    );
  };

  return (
    <div className="flex h-screen w-full overflow-hidden bg-zinc-950 font-sans text-zinc-300">
      <LandingOverlay />
      
      {/* SIDEBAR */}
      <aside className="w-80 border-r border-zinc-800 bg-zinc-900/50 flex flex-col z-10 shrink-0 hidden md:flex">
        <div className="p-4 border-b border-zinc-800 bg-zinc-950">
          <div className="flex items-center gap-2">
            <img src="/wared-icon.png" alt="Warded Logo" className="w-6 h-6 object-contain" />
            <h1 className="text-xl font-mono font-bold text-white tracking-tight">Warded</h1>
          </div>
          <p className="text-[11px] font-mono text-zinc-500 mt-1">Giúp đoạn đường của bạn đỡ chông gai hơn mỗi ngày.</p>
        </div>
        <div className="p-3 border-b border-zinc-800 bg-zinc-900">
          <input 
            type="text" 
            placeholder="Tìm kiếm camera..." 
            className="w-full bg-zinc-950 border border-zinc-700 rounded px-3 py-1.5 text-xs text-white placeholder-zinc-500 focus:outline-none focus:border-zinc-500 font-mono"
            onChange={(e) => setSearchTerm(e.target.value)}
          />
        </div>
        <div className="flex-1 p-2 overflow-y-auto space-y-1">
          {loading ? (
            <p className="text-xs text-yellow-500 font-mono p-2">Đang tải {cameras.length} camera...</p>
          ) : error ? (
            <p className="text-xs text-red-500 font-mono p-2">Lỗi: {error.message}</p>
          ) : (
            filteredCameras.map(cam => (
              <div 
                key={cam.id} 
                onClick={() => setSelectedCamera(cam)}
                className="p-2 border border-zinc-800 bg-zinc-950 hover:bg-zinc-800 cursor-pointer rounded flex flex-col gap-1"
              >
                <span className="text-xs font-bold text-white truncate">{cam.displayName || cam.title}</span>
                <span className="text-[10px] text-zinc-500 truncate">{cam.zone}</span>
              </div>
            ))
          )}
        </div>
      </aside>

      {/* MAP WORKSPACE */}
      <main className="flex-1 flex flex-col h-full relative overflow-hidden bg-zinc-950 z-0">
        <div className="flex-1 w-full h-full relative z-0">
          <MapComponent cameras={cameras} verdicts={verdicts} onCameraClick={setSelectedCamera} />
        </div>
        
        {/* MODAL */}
        <CameraModal 
          camera={selectedCamera} 
          verdict={selectedCamera ? verdicts.get(selectedCamera.id) : undefined} 
          onClose={() => setSelectedCamera(null)} 
          isPinned={selectedCamera ? pinnedCameraIds.includes(selectedCamera.id) : false}
          onTogglePin={() => selectedCamera && togglePin(selectedCamera.id)}
        />
        
        {/* BOTTOM DOCK */}
        <PinnedDock 
          pinnedCameras={pinnedCameras} 
          onUnpin={togglePin} 
          onClear={() => setPinnedCameraIds([])} 
        />

        {/* MAP LEGEND — collapsible */}
        <MapLegend
          isOpen={legendOpen}
          onToggle={() => setLegendOpen(prev => !prev)}
        />

        {/* MOBILE CAMERA LIST — trigger button (hidden on md+) */}
        <button
          onClick={() => setMobileDrawerOpen(true)}
          className="md:hidden absolute bottom-20 right-3 z-[800] w-11 h-11 bg-zinc-950 border border-zinc-700 hover:border-zinc-400 text-white rounded flex items-center justify-center shadow-2xl transition-colors"
          aria-label="Danh sách camera"
        >
          <span className="text-base leading-none">☰</span>
        </button>

        {/* MOBILE CAMERA DRAWER */}
        <MobileCameraDrawer
          cameras={cameras}
          loading={loading}
          error={error}
          isOpen={mobileDrawerOpen}
          onClose={() => setMobileDrawerOpen(false)}
          onCameraClick={setSelectedCamera}
          searchTerm={searchTerm}
          onSearchChange={setSearchTerm}
        />
      </main>
    </div>
  );
}
