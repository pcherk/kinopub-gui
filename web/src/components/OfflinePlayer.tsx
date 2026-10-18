import { useEffect, useRef, useState } from "react";
import Hls from "hls.js";
import { X } from "lucide-react";

// The NAS browser plays local files; unlike /api/open, this does not attempt
// to launch a graphical desktop application inside the Docker container.
export function OfflinePlayer({ path, onClose }: { path: string; onClose: () => void }) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const [error, setError] = useState("");
  const isMKV = /\.mkv$/i.test(path);
  const [fallbackHLS, setFallbackHLS] = useState(false);
  const useHLS = isMKV || fallbackHLS;
  const query = new URLSearchParams({ path });
  const src = useHLS ? `/api/local/hls?${query.toString()}` : `/api/local/video?${query.toString()}`;

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;
    setError("");
    let hls: Hls | undefined;
    if (useHLS) {
      if (Hls.isSupported()) {
        hls = new Hls({ enableWorker: true, lowLatencyMode: false, maxBufferLength: 30 });
        hls.loadSource(src);
        hls.attachMedia(video);
        hls.on(Hls.Events.ERROR, (_event, data) => {
          if (data.fatal) setError("Не удалось подготовить видео. Проверьте FFmpeg и журнал KinoPub.");
        });
      } else if (video.canPlayType("application/vnd.apple.mpegurl")) {
        video.src = src;
      } else {
        setError("Браузер не поддерживает HLS-видео.");
      }
    } else {
      video.src = src;
    }
    return () => {
      video.pause();
      video.removeAttribute("src");
      video.load();
      hls?.destroy();
    };
  }, [path, src, useHLS]);

  return (
    <div className="fixed inset-0 z-[100] flex flex-col bg-black/95">
      <div className="flex min-h-12 items-center justify-between gap-3 border-b border-white/10 px-4 py-2">
        <p className="min-w-0 truncate text-sm text-slate-200">{path.split("/").pop()}</p>
        <button
          type="button"
          onClick={onClose}
          title="Закрыть"
          aria-label="Закрыть видео"
          className="rounded-lg p-2 text-white hover:bg-white/10"
        >
          <X className="h-5 w-5" />
        </button>
      </div>
      <div className="flex min-h-0 flex-1 items-center justify-center">
        <video
          ref={videoRef}
          controls
          autoPlay
          playsInline
          className="h-full max-h-full w-full bg-black object-contain"
          onError={() => {
            if (!useHLS) {
              setFallbackHLS(true);
            } else {
              setError("Браузер не смог воспроизвести видео даже после подготовки через FFmpeg.");
            }
          }}
        />
      </div>
      {useHLS && <p className="px-4 py-2 text-center text-xs text-slate-400">MKV подготавливается через FFmpeg. Первая загрузка и перемотка вперёд могут занять время.</p>}
      {error && <p role="alert" className="bg-red-950 px-4 py-3 text-center text-sm text-red-200">{error}</p>}
    </div>
  );
}
