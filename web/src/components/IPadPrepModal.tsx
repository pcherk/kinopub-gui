import { useEffect, useState } from "react";
import { Download, Tablet, X } from "lucide-react";
import { api, type IPadExportStatus } from "../api";

// All iPad exports use one FIFO queue. Closing this window does not cancel work.
export function IPadPrepModal({ path, onClose }: { path?: string; onClose: () => void }) {
  const [jobs, setJobs] = useState<IPadExportStatus[]>([]);
  const [error, setError] = useState("");
  const [adding, setAdding] = useState(false);
  const [profile, setProfile] = useState<"balanced" | "compact" | "fast">("balanced");
  const [justAdded, setJustAdded] = useState(false);
  const [bulkBusy,setBulkBusy] = useState(false);
  const [bulkMessage,setBulkMessage] = useState("");
  const refresh = () => api.ipadQueue().then(setJobs).catch(() => {});
  const add = async () => {
    if (!path || adding) return;
    setAdding(true);
    setError("");
    try {
      await api.ipadPrepare(path, profile);
      setJustAdded(true);
      await refresh();
    } catch (e: any) { setError(e.message || "Не удалось добавить в очередь"); }
    finally { setAdding(false); }
  };

  useEffect(() => {
    let active = true;
    const load = () => api.ipadQueue()
      .then((items) => { if (active) setJobs(items); })
      .catch((e) => { if (active) setError(e.message || "Ошибка чтения очереди"); });
    load();
    const timer = window.setInterval(load, 2000);
    return () => { active = false; window.clearInterval(timer); };
  }, []);

  const bulkPrepare = async () => {
    if (bulkBusy) return;
    if (!window.confirm("Добавить все скачанные фильмы и серии в очередь iPad? Уже готовые или добавленные будут пропущены.")) return;
    setBulkBusy(true);
    setError("");
    try {
      const result=await api.ipadPrepareAll(profile);
      setBulkMessage(`Добавлено: ${result.added}. Пропущено: ${result.skipped}.${result.queueFull ? " Очередь заполнена — остальные можно добавить позже." : ""}`);
      await refresh();
    } catch (e: any) {setError(e.message || "Не удалось поставить всё в очередь");}
    finally {setBulkBusy(false);}
  };
  const clearHistory = async () => {
    if (!window.confirm("Очистить отменённые, неудачные и отсутствующие записи? Оригиналы и готовые MP4 не удаляются.")) return;
    try {
      const result=await api.ipadClear();
      setBulkMessage(`Убрано записей из истории: ${result.removed}`);
      await refresh();
    } catch (e: any) {setError(e.message || "Не удалось очистить историю");}
  };

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
          Оптимальное качество 1080p по умолчанию. NAS готовит до двух файлов одновременно; остальные ждут очереди.
        </p>
        <p className="mb-3 text-xs text-slate-400">
          Файлы находятся в downloads/iPad на Synology. Оригинальные фильмы не изменяются.
          Очередь продолжится после перезапуска контейнера.
        </p>
        {path && (
          <div className="mb-3 rounded-xl border border-white/10 bg-white/[0.03] p-3">
            <p className="mb-2 break-words text-sm font-medium text-slate-100">{path.split("/").pop()}</p>
            <div className="mb-3 flex flex-wrap gap-2">
              <button type="button" onClick={() => setProfile("balanced")}
                className={profile === "balanced" ? "btn-primary px-3 py-2 text-xs" : "btn-ghost px-3 py-2 text-xs"}>
                Оптимальный · до 1080p
              </button>
              <button type="button" onClick={() => setProfile("compact")}
                className={profile === "compact" ? "btn-primary px-3 py-2 text-xs" : "btn-ghost px-3 py-2 text-xs"}>
                Экономия места · 720p
              </button>
              <button type="button" onClick={() => setProfile("fast")}
                className={profile === "fast" ? "btn-primary px-3 py-2 text-xs" : "btn-ghost px-3 py-2 text-xs"}>
                Быстрый · до 1080p
              </button>
            </div>
            <p className="mb-3 text-xs text-slate-400">
              {profile === "balanced"
                ? "1080p, H.264 с сохранением деталей. Обычно занимает меньше места, чем исходный фильм, но конвертируется дольше."
                : profile === "compact"
                  ? "Максимальная экономия места. Возможна потеря деталей на динамичных сценах."
                  : "Совместимое видео копируется без перекодирования. Файл может быть большим."}
            </p>
            <button type="button" className="btn-primary px-4 py-2 text-sm" onClick={add} disabled={adding}>
              {adding ? "Добавляем…" : justAdded ? "Добавить ещё в выбранном режиме" : "Добавить в очередь"}
            </button>
          </div>
        )}
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <button type="button" className="btn-primary px-3 py-2 text-sm" onClick={bulkPrepare} disabled={bulkBusy}>
            {bulkBusy ? "Добавляем…" : "Подготовить ВСЕ фильмы для iPad"}
          </button>
          <button type="button" className="btn-ghost px-3 py-2 text-xs" onClick={clearHistory}>
            Очистить историю ошибок
          </button>
        </div>
        {bulkMessage && <p role="status" className="mb-3 text-xs text-emerald-300">{bulkMessage}</p>}
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
              <p className="mb-1 break-words text-sm font-medium text-slate-100">{job.fileName}</p>
              <p className="mb-2 text-xs text-slate-500">{job.profile === "balanced" ? "Оптимальный · 1080p" : job.profile === "compact" ? "Экономия места · 720p" : "Быстрый · до 1080p"}</p>
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
                <p className="text-xs text-emerald-300">
                  Готово · {((job.size ?? 0) / (1024 * 1024)).toFixed(0)} МБ
                  {job.sourceSize && job.size && job.sourceSize > job.size
                    ? ` · на ${Math.round(100 * (job.sourceSize - job.size) / job.sourceSize)}% меньше оригинала`
                    : ""}
                </p>
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
                      <Download className="h-4 w-4" /> СКАЧАТЬ MP4 на это устройство
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
        <div className="mt-3 border-t border-white/10 pt-3 text-xs text-slate-300">
          <p className="font-semibold text-gold-200">Сохранение для просмотра без интернета</p>
          <p className="mt-1">1. Нажми «СКАЧАТЬ MP4 на это устройство» у готового фильма.</p>
          <p className="mt-1">2. В Safari открой значок загрузок (стрелка), затем приложение «Файлы» → «Загрузки».</p>
          <p className="mt-1">3. Для надёжного офлайн-просмотра перемести MP4 в «На моём iPad».</p>
          <p className="mt-2 text-slate-500">Можно закрыть окно — NAS продолжит подготовку.</p>
        </div>
      </div>
    </div>
  );
}
