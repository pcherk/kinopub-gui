import { useEffect, useState } from "react";
import { Download, Tablet, X } from "lucide-react";
import { api, type IPadExportStatus } from "../api";

// All iPad exports use one FIFO queue. Closing this window does not cancel work.
export function IPadPrepModal({ path, onClose }: { path?: string; onClose: () => void }) {
  const [jobs, setJobs] = useState<IPadExportStatus[]>([]);
  const [error, setError] = useState("");
  const [adding, setAdding] = useState(false);

  const refresh = () => api.ipadQueue().then(setJobs).catch(() => {});

  useEffect(() => {
    if (!path) return;
    let active = true;
    setAdding(true);
    setError("");
    api.ipadPrepare(path)
      .then(() => { if (active) refresh(); })
      .catch((e) => { if (active) setError(e.message || "Не удалось добавить в очередь"); })
      .finally(() => { if (active) setAdding(false); });
    return () => { active = false; };
  }, [path]);

  useEffect(() => {
    let active = true;
    const load = () => api.ipadQueue()
      .then((items) => { if (active) setJobs(items); })
      .catch((e) => { if (active) setError(e.message || "Ошибка чтения очереди"); });
    load();
    const timer = window.setInterval(load, 2000);
    return () => { active = false; window.clearInterval(timer); };
  }, []);

  const cancel = async (id: string) => {
    try { await api.ipadCancel(id); await refresh(); }
    catch (e: any) { setError(e.message || "Не удалось отменить"); }
  };
  const deleteCopy = async (id: string) => {
    if (!window.confirm("Удалить только подготовленную MP4-копию? Оригинал останется.")) return;
    try { await api.ipadDelete(id); await refresh(); }
    catch (e: any) { setError(e.message || "Не удалось удалить копию"); }
  };

  const active = jobs.filter((j) => j.status === "running" || j.status === "queued");
  const ready = jobs.filter((j) => j.status === "ready");
  const others = jobs.filter((j) => j.status === "failed" || j.status === "canceled");
  const ordered = [...active, ...ready, ...others];

  return (
    <div className="fixed inset-0 z-[110] flex items-center justify-center bg-black/80 p-3">
      <div className="card flex max-h-[90vh] w-full max-w-2xl flex-col p-4 sm:p-5">
        <div className="mb-3 flex items-center gap-2">
          <Tablet className="h-5 w-5 text-gold-300" />
          <h2 className="flex-1 text-lg font-semibold text-slate-100">Очередь для iPad</h2>
          <button onClick={onClose} className="rounded-md p-2 text-slate-300 hover:bg-white/10" aria-label="Закрыть">
            <X className="h-5 w-5" />
          </button>
        </div>
        <p className="mb-2 text-xs text-slate-400">
          H.264 до 1080p, AAC stereo. FFmpeg готовит файлы по одному; остальные ждут своей очереди.
        </p>
        <p className="mb-3 text-xs text-slate-400">
          Файлы находятся в downloads/iPad на Synology. Оригинальные фильмы не изменяются.
          Очередь продолжится после перезапуска контейнера.
        </p>
        {adding && <p className="mb-2 text-sm text-gold-200">Добавляем фильм в очередь…</p>}
        {error && <p role="alert" className="mb-3 text-sm text-ember-300">{error}</p>}
        <div className="min-h-0 flex-1 space-y-3 overflow-y-auto pr-1">
          {ordered.length === 0 && !adding && (
            <p className="py-7 text-center text-sm text-slate-400">
              Очередь пуста. Нажми «Для iPad» у любого фильма или серии.
            </p>
          )}
          {ordered.map((job) => (
            <div key={job.id} className="rounded-xl border border-white/10 bg-white/[0.03] p-3">
              <p className="mb-2 break-words text-sm font-medium text-slate-100">{job.fileName}</p>
              {job.status === "running" && (
                <>
                  <p className="mb-1 text-xs text-slate-300">Подготовка: {job.percent}%</p>
                  <div className="h-2 overflow-hidden rounded-full bg-white/10">
                    <div className="h-full bg-gold-400 transition-all" style={{ width: `${job.percent}%` }} />
                  </div>
                </>
              )}
              {job.status === "queued" && (
                <p className="text-xs text-slate-400">Ожидает · №{job.position ?? "?"} в очереди</p>
              )}
              {job.status === "ready" && (
                <p className="text-xs text-emerald-300">Готово · {((job.size ?? 0) / (1024 * 1024)).toFixed(0)} МБ</p>
              )}
              {job.status === "failed" && (
                <p className="text-xs text-ember-300">{job.error || "Ошибка FFmpeg"}</p>
              )}
              {job.status === "canceled" && <p className="text-xs text-slate-400">Отменено</p>}
              <div className="mt-3 flex flex-wrap items-center gap-2">
                {(job.status === "queued" || job.status === "running") && (
                  <button className="btn-ghost px-3 py-1.5 text-xs" onClick={() => cancel(job.id)}>
                    Отменить
                  </button>
                )}
                {job.status === "ready" && (
                  <>
                    <a
                      className="btn-primary inline-flex items-center gap-2 px-3 py-1.5 text-xs"
                      href={api.ipadDownloadUrl(job.id)}
                      download={job.fileName}
                    >
                      <Download className="h-4 w-4" /> Скачать на iPad
                    </a>
                    <button className="btn-ghost px-3 py-1.5 text-xs" onClick={() => deleteCopy(job.id)}>
                      Удалить копию
                    </button>
                  </>
                )}
              </div>
            </div>
          ))}
        </div>
        <p className="mt-3 text-xs text-slate-400">
          Можешь закрыть окно, поставить ещё фильмы в очередь или уйти со страницы.
        </p>
      </div>
    </div>
  );
}
