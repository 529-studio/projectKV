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
import PhuongTreeFilter, { OTHER_WARDS_KEY, OUTSIDE_HCMC_KEY, extractCorridor } from '../components/PhuongTreeFilter';

const MapComponent = dynamic(() => import('../components/MapComponent'), { ssr: false });

export default function Home() {
  const { cameras, loading, error } = useCameras();
  const { verdicts, analysisEnabled } = useTraffic();
  
  const [selectedCamera, setSelectedCamera] = useState<Camera | null>(null);
  const [searchTerm, setSearchTerm] = useState('');
  const [pinnedCameraIds, setPinnedCameraIds] = useState<string[]>([]);
  const [legendOpen, setLegendOpen] = useState(false);
  const [mobileDrawerOpen, setMobileDrawerOpen] = useState(false);

  // Hierarchical Filter State (D3 - Issue #41)
  const [selectedPhuong, setSelectedPhuong] = useState<string | null>(null);
  const [selectedCorridor, setSelectedCorridor] = useState<string | null>(null);
  const [lang, setLang] = useState<'vi' | 'en'>('vi');

  // Filter map cameras based on Phường / corridor selection
  const mapCameras = useMemo(() => {
    let result = cameras;
    if (selectedPhuong) {
      if (selectedPhuong === OTHER_WARDS_KEY || selectedPhuong === OUTSIDE_HCMC_KEY) {
        result = result.filter(c => !c.phuong_name);
      } else {
        result = result.filter(c => c.phuong_name === selectedPhuong);
      }
    }
    if (selectedCorridor) {
      result = result.filter(c => extractCorridor(c.displayName, c.title) === selectedCorridor);
    }
    return result;
  }, [cameras, selectedPhuong, selectedCorridor]);

  const isFiltered = Boolean(selectedPhuong || selectedCorridor);

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
      
      {/* SIDEBAR — Desktop Tree Filter */}
      <aside className="w-80 lg:w-96 border-r border-zinc-800 bg-zinc-950 flex flex-col z-10 shrink-0 hidden md:flex">
        <div className="p-4 border-b border-zinc-800 bg-zinc-950 shrink-0">
          <div className="flex items-center gap-2">
            <img src="/wared-icon.png" alt="Warded Logo" className="w-6 h-6 object-contain" />
            <h1 className="text-xl font-mono font-bold text-white tracking-tight">Warded</h1>
          </div>
          <p className="text-[11px] font-mono text-zinc-500 mt-1">
            {lang === 'vi'
              ? 'Giúp đoạn đường của bạn đỡ chông gai hơn mỗi ngày.'
              : 'Making your commute smoother every day.'}
          </p>
        </div>
        <div className="flex-1 min-h-0 overflow-hidden">
          <PhuongTreeFilter
            cameras={cameras}
            loading={loading}
            error={error}
            selectedPhuong={selectedPhuong}
            selectedCorridor={selectedCorridor}
            onSelectPhuong={setSelectedPhuong}
            onSelectCorridor={setSelectedCorridor}
            onCameraClick={setSelectedCamera}
            searchTerm={searchTerm}
            onSearchChange={setSearchTerm}
            lang={lang}
            onLangChange={setLang}
          />
        </div>
      </aside>

      {/* MAP WORKSPACE */}
      <main className="flex-1 flex flex-col h-full relative overflow-hidden bg-zinc-950">
        <div className="flex-1 w-full h-full relative z-0">
          <MapComponent
            cameras={mapCameras}
            verdicts={verdicts}
            onCameraClick={setSelectedCamera}
            selectedCamera={selectedCamera}
            isFiltered={isFiltered}
          />
        </div>
        
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
          analysisEnabled={analysisEnabled}
          lang={lang}
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
          selectedPhuong={selectedPhuong}
          selectedCorridor={selectedCorridor}
          onSelectPhuong={setSelectedPhuong}
          onSelectCorridor={setSelectedCorridor}
          searchTerm={searchTerm}
          onSearchChange={setSearchTerm}
          lang={lang}
          onLangChange={setLang}
        />
      </main>

      {/* MODAL — global viewport overlay */}
      <CameraModal 
        camera={selectedCamera} 
        verdict={selectedCamera ? verdicts.get(selectedCamera.id) : undefined} 
        onClose={() => setSelectedCamera(null)} 
        isPinned={selectedCamera ? pinnedCameraIds.includes(selectedCamera.id) : false}
        onTogglePin={() => selectedCamera && togglePin(selectedCamera.id)}
      />
    </div>
  );
}
