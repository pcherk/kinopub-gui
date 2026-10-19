import type { IPadProfile } from "./api";

export const IPAD_PROFILES: {
  id: IPadProfile;
  title: string;
  description: string;
}[] = [
  {
    id: "balanced",
    title: "Оптимальный",
    description: "Чёткое видео до 1080p и разумный размер. Лучший выбор для iPad.",
  },
  {
    id: "fast",
    title: "Быстрый / оригинал",
    description: "Без перекодирования, если видео совместимо. Быстро, но файл может быть большим.",
  },
  {
    id: "compact",
    title: "Экономия места",
    description: "До 720p, больше фильмов поместится на iPad. В сложных сценах детализация ниже.",
  },
];

export function ipadProfileName(profile?: IPadProfile): string {
  return IPAD_PROFILES.find((p) => p.id === profile)?.title ?? "Неизвестный профиль";
}
