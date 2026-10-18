import { useEffect, useState } from "react";
import { Download, Tablet, X } from "lucide-react";
import { api, type IPadExportStatus } from "../api";

export function IPadPrepModal({ path, onClose }: { path: string; onClose: () => void }) {
  const [job, setJob] = useState<IPadExportStatus | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let active = true;
    setJob(null);
    setError("");
    api.ipadPrepare(path).then((next) => {
      if (active) setJob(next);
    }).catch((e) => { if (active) setError(e.message || "Не удалось подготовить файл"); });
    return () => { active = false; };
  }, [path]);

  useEffect(() => {
    if (!job || job.status !== "running") return;
    let alive = true;
    const timer = window.setTimeout(() => {
      api.ipadStatus(job.id)
        .then((next) => { if (alive) setJob(next); })
        .catch((e) => { if (alive) setError(e.message || "Не удалось проверить обработку"); });
    }, 1500);
    return () => { alive = false; window.clearTimeout(timer); };
  }, [job]);

  const deleteCopy = async () => {
    if (!job || job.status !== "ready") return;
    if (!window.confirm("Удалить только подготовленную MP4-копию на Synology? Оригинал сохранится.")) return;
    try { setJob(await api.ipadDelete(job.id)); }
    catch (e: any) { setError(e.message || "Не удалось удалить копию"); }
  };

  const cancel = async () => {
    if (!job || job.status !== "running") return;
    try { setJob(await api.ipadCancel(job.id)); }
    catch (e: any) { setError(e.message || "Не удалось отменить"); }
  };

  return (
    <div className="fixed inset-0 z-[110] flex items-center justify-center bg-black/80 p-4">
      <div className="card w-full max-w-lg p-5">
        <div className="mb-4 flex items-center gap-2">
          <Tablet className="h-5 w-5 text-gold-300" />
          <h2 className="flex-1 text-lg font-semibold text-slate-100">Подготовить для iPad</h2>
          <button onClick={onClose} className="rounded-md p-2 text-slate-300 hover:bg-white/10" aria-label="Закрыть">
            <X className="h-5 w-5" />
          </button>
        </div>
        <p className="mb-2 break-words text-sm text-slate-200">{path.split("/").pop()}</p>
        <p className="mb-4 text-xs text-slate-400">
          MP4 · H.264 до 1080p · AAC stereo. Исходный файл сохранится без изменений.
        </p>

        {!job && !error && <p className="text-sm text-slate-300">Проверяем видео и запускаем подготовку…</p>}
        {job?.status === "running" && (
          <>
            <p className="mb-2 text-sm text-slate-300">Подготовка: {job.percent}%</p>
            <div className="h-2 overflow-hidden rounded-full bg-white/10">
              <div className="h-full bg-gold-400 transition-all" style={{ width: `${job.percent}%` }} />
            </div>
            <p className="mt-2 text-xs text-slate-400">
              Можно закрыть окно. Обработка на Synology продолжится.
            </p>
            <button className="btn-ghost mt-4" onClick={cancel}>Отменить подготовку</button>
          </>
        )}
        {job?.status === "ready" && (
          <>
            <p className="mb-3 text-sm text-emerald-300">
              Готово · {((job.size ?? 0) / (1024 * 1024)).toFixed(0)} МБ
            </p>
            <a className="btn-primary inline-flex items-center gap-2" href={api.ipadDownloadUrl(job.id)} download={job.fileName}>
              <Download className="h-4 w-4" /> Скачать на iPad
            </a>
            <p className="mt-3 text-xs text-slate-400">Safari сохранит фильм в «Файлы» → «Загрузки». После этого интернет не потребуется.</p>
            <button className="btn-ghost mt-3 block text-xs" onClick={deleteCopy}>Удалить подготовленную копию с NAS</button>
          </>
        )}
        {job?.status === "failed" && <p role="alert" className="text-sm text-ember-300">{job.error || "Ошибка FFmpeg"}</p>}
        {job?.status === "deleted" && <p className="text-sm text-slate-400">Копия удалена с NAS. Оригинал не затронут.</p>}
        {job?.status === "canceled" && <p className="text-sm text-slate-400">Подготовка отменена.</p>}
        {error && <p role="alert" className="mt-3 text-sm text-ember-300">{error}</p>}
      </div>
    </div>
  );
}
