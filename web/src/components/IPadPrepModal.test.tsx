import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { IPadPrepModal } from "./IPadPrepModal";
import { api, type IPadExportStatus } from "../api";

vi.mock("../api", () => ({
  api: {
    ipadQueue: vi.fn(),
    ipadPrepare: vi.fn(),
    ipadPrepareAll: vi.fn(),
    ipadClear: vi.fn(),
    ipadCancel: vi.fn(),
    ipadDelete: vi.fn(),
    ipadDownloadUrl: (id: string) => `/api/ipad/download/${id}`,
  },
}));

beforeEach(() => {
  vi.mocked(api.ipadQueue).mockResolvedValue([]);
  vi.mocked(api.ipadPrepare).mockResolvedValue({
    id: "film1", status: "queued", fileName: "Film - iPad.mp4", percent: 0, profile: "balanced",
  });
  vi.mocked(api.ipadPrepareAll).mockResolvedValue({ added: 3, skipped: 2, total: 5, queueFull: false });
  vi.mocked(api.ipadClear).mockResolvedValue({ removed: 2 });
  vi.spyOn(window, "confirm").mockReturnValue(true);
});
afterEach(() => {vi.restoreAllMocks();vi.clearAllMocks()});

describe("iPad queue interface", () => {
  it("adds all media using the chosen optimal profile", async () => {
    render(<IPadPrepModal onClose={() => {}} />);
    fireEvent.click(screen.getByText("Подготовить ВСЕ фильмы для iPad"));
    await waitFor(() => expect(api.ipadPrepareAll).toHaveBeenCalledWith("balanced"));
    expect(await screen.findByText(/Добавлено: 3/)).toBeTruthy();
  });
  it("clears stale history without deleting video files", async () => {
    render(<IPadPrepModal onClose={() => {}} />);
    fireEvent.click(screen.getByText("Очистить историю ошибок"));
    await waitFor(() => expect(api.ipadClear).toHaveBeenCalledOnce());
    expect(api.ipadDelete).not.toHaveBeenCalled();
  });
  it("exposes direct MP4 download and Safari offline instructions", async () => {
    const item: IPadExportStatus = { id: "abc", status: "ready", percent: 100,
      profile: "balanced",fileName: "Film - iPad 1080p.mp4", size: 12345678 };
    vi.mocked(api.ipadQueue).mockResolvedValue([item]);
    render(<IPadPrepModal onClose={() => {}} />);
    const el=await screen.findByText("СКАЧАТЬ MP4 на это устройство");
    expect(el.closest("a")?.getAttribute("href")).toBe("/api/ipad/download/abc");
    expect(screen.getByText(/«На моём iPad»/)).toBeTruthy();
  });
  it("offers exactly three clear profiles and selects optimal by default", async () => {
    render(<IPadPrepModal path="/data/Film.mkv" onClose={() => {}} />);
    const options = ["Оптимальный", "Быстрый / оригинал", "Экономия места"];
    const profileGroup = screen.getByRole("group", { name: "Профиль подготовки для iPad" });
    const buttons = profileGroup.querySelectorAll("button");
    expect(buttons).toHaveLength(3);
    expect(Array.from(buttons).map((b) => b.textContent?.includes("Оптимальный") ? "Оптимальный" :
      b.textContent?.includes("Быстрый / оригинал") ? "Быстрый / оригинал" : "Экономия места")).toEqual(options);
    expect(buttons[0].getAttribute("aria-pressed")).toBe("true");
    expect(buttons[1].getAttribute("aria-pressed")).toBe("false");
  });

  it("sends the selected fast/original and economy profiles to the backend", async () => {
    render(<IPadPrepModal path="/data/Film.mkv" onClose={() => {}} />);
    fireEvent.click(screen.getByText("Быстрый / оригинал"));
    fireEvent.click(screen.getByText("Добавить в очередь"));
    await waitFor(() => expect(api.ipadPrepare).toHaveBeenCalledWith("/data/Film.mkv", "fast"));
    fireEvent.click(screen.getByText("Экономия места"));
    fireEvent.click(screen.getByText("Добавить ещё в выбранном режиме"));
    await waitFor(() => expect(api.ipadPrepare).toHaveBeenCalledWith("/data/Film.mkv", "compact"));
  });

  it("mass enqueues with the chosen profile", async () => {
    render(<IPadPrepModal path="/data/Film.mkv" onClose={() => {}} />);
    fireEvent.click(screen.getByText("Экономия места"));
    fireEvent.click(screen.getByText("Подготовить ВСЕ фильмы для iPad"));
    await waitFor(() => expect(api.ipadPrepareAll).toHaveBeenCalledWith("compact"));
  });
});
