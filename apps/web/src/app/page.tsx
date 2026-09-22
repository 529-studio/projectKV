'use client';
import dynamic from 'next/dynamic';
import { useCameras } from '../hooks/useCameras';
import { useTraffic } from '../hooks/useTraffic';
import LandingOverlay from '../components/LandingOverlay';

const MapComponent = dynamic(() => import('../components/MapComponent'), { ssr: false });

export default function Home() {
  const { cameras, loading, error } = useCameras();
  const { verdicts } = useTraffic();

  return (
    <main className="relative w-screen h-screen overflow-hidden bg-black text-white font-mono">
      <LandingOverlay />
      
      <div className="absolute inset-0 z-0">
        <MapComponent cameras={cameras} verdicts={verdicts} />
      </div>

      <div className="absolute top-4 left-4 z-10 bg-black/80 p-4 border border-zinc-800 rounded shadow-lg backdrop-blur">
        <h2 className="text-xl font-bold">ProjectKV</h2>
        <p className="text-xs text-zinc-400 mt-1">Hệ thống Giám sát Giao thông</p>
        {loading && <p className="text-xs mt-2 text-yellow-500">Đang tải dữ liệu...</p>}
        {error && <p className="text-xs mt-2 text-red-500">Lỗi: {error.message}</p>}
        {!loading && !error && <p className="text-xs mt-2 text-green-500">Sẵn sàng ({cameras.length} cameras)</p>}
      </div>
    </main>
  );
}
