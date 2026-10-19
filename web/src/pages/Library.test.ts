import { describe, expect, it } from "vitest";
import type { LibrarySeries, IPadExportStatus } from "../api";
import { itemIdFromURL, matchesQuery, supersededByLibrary, ipadStatusForPath } from "./Library";

function series(over: Partial<LibrarySeries> = {}): LibrarySeries {
  return {
    dir: "/downloads/Breaking Bad",
    stateFile: "/downloads/Breaking Bad/.state.json",
    seriesId: "42",
    title: "Во все тяжкие",
    originalTitle: "Breaking Bad",
    genres: ["драма", "криминал"],
    isMovie: false,
    count: 2,
    totalBytes: 1024,
    updatedAt: "2026-08-01T10:00:00Z",
    episodes: [],
    ...over,
  };
}

describe("matchesQuery", () => {
  it("keeps everything for an empty or whitespace-only query", () => {
    expect(matchesQuery(series(), "")).toBe(true);
    expect(matchesQuery(series(), "   ")).toBe(true);
  });

  it("matches the localized title case-insensitively", () => {
    expect(matchesQuery(series(), "ТЯЖКИЕ")).toBe(true);
    expect(matchesQuery(series(), "тяжкие")).toBe(true);
  });

  it("matches the original title", () => {
    expect(matchesQuery(series(), "breaking")).toBe(true);
  });

  it("matches a genre, so a genre chip and the search box agree", () => {
    expect(matchesQuery(series(), "криминал")).toBe(true);
  });

  it("requires every term to match, across different fields", () => {
    expect(matchesQuery(series(), "breaking драма")).toBe(true);
    expect(matchesQuery(series(), "breaking комедия")).toBe(false);
  });

  it("ignores extra whitespace between terms", () => {
    expect(matchesQuery(series(), "  breaking   драма  ")).toBe(true);
  });

  it("rejects a term that appears nowhere", () => {
    expect(matchesQuery(series(), "интерстеллар")).toBe(false);
  });

  it("handles an entry with no original title and no genres", () => {
    const bare = series({ originalTitle: undefined, genres: undefined });
    expect(matchesQuery(bare, "тяжкие")).toBe(true);
    expect(matchesQuery(bare, "breaking")).toBe(false);
  });
});

function libEpisode(season: number, episode: number, exists: boolean) {
  return {
    key: `S${season}E${episode}`,
    season,
    episode,
    title: "",
    path: `/downloads/S0${season}E0${episode}.mkv`,
    exists,
    bytes: 100,
    completedAt: "2026-08-01T10:00:00Z",
  };
}

function jobEpisode(season: number, episode: number) {
  return {
    key: `S0${season}E0${episode}`,
    season,
    episode,
    title: "",
    state: "failed",
    percent: 58,
    bytes: 0,
    total: 0,
    totalApprox: false,
    segDone: 0,
    segTotal: 0,
    speedBps: 0,
    etaSeconds: 0,
    attempts: 1,
  };
}

function job(over: Record<string, unknown> = {}): any {
  return {
    id: "j1",
    url: "https://kino.pub/item/view/409",
    status: "failed",
    title: "Fear and Loathing",
    outputPath: "/downloads",
    dryRun: false,
    quality: "1080p",
    createdAt: "2026-07-12T10:00:00Z",
    episodes: [jobEpisode(1, 1)],
    logs: [],
    ...over,
  };
}

describe("itemIdFromURL", () => {
  it("pulls the id out of a catalog URL", () => {
    expect(itemIdFromURL("https://kino.pub/item/view/409")).toBe("409");
    expect(itemIdFromURL("https://kino.watch/item/view/77/season/2")).toBe("77");
  });

  it("returns empty for a URL without an item id", () => {
    expect(itemIdFromURL("https://example.com/x")).toBe("");
    expect(itemIdFromURL("")).toBe("");
  });
});

describe("supersededByLibrary", () => {
  const entry = (over: Partial<LibrarySeries> = {}) =>
    series({ inputUrl: "https://kino.pub/item/view/409", episodes: [libEpisode(1, 1, true)], ...over });

  it("is superseded when every planned episode is on disk", () => {
    expect(supersededByLibrary(job(), [entry()])).toBe(true);
  });

  it("matches on season/episode, not on key spelling", () => {
    // The queue writes "S01E01" while the state file writes "S1E1".
    expect(job().episodes[0].key).not.toBe(entry().episodes[0].key);
    expect(supersededByLibrary(job(), [entry()])).toBe(true);
  });

  it("is not superseded when the file is recorded but gone from disk", () => {
    expect(supersededByLibrary(job(), [entry({ episodes: [libEpisode(1, 1, false)] })])).toBe(false);
  });

  it("is not superseded when only some planned episodes are on disk", () => {
    const multi = job({ episodes: [jobEpisode(1, 1), jobEpisode(1, 2)] });
    expect(supersededByLibrary(multi, [entry()])).toBe(false);
  });

  it("is not superseded when the library holds a different title", () => {
    expect(supersededByLibrary(job(), [entry({ inputUrl: "https://kino.pub/item/view/999" })])).toBe(false);
  });

  it("is not superseded when the run died before planning anything", () => {
    expect(supersededByLibrary(job({ episodes: [] }), [entry()])).toBe(false);
  });

  it("is not superseded when the job URL carries no item id", () => {
    expect(supersededByLibrary(job({ url: "https://example.com/x" }), [entry()])).toBe(false);
  });

  it("matches a library entry that only knows its numeric series id", () => {
    const byId = entry({ inputUrl: undefined, seriesId: "409" });
    expect(supersededByLibrary(job(), [byId])).toBe(true);
  });
});

describe("iPad status on library cards", () => {
  function exportJob(id: string, status: IPadExportStatus["status"], profile: "fast" | "compact" | "balanced"): IPadExportStatus {
    return { id, status, profile, percent: 45, fileName: "Film.mp4",
      sourcePath: "/downloads/Film.mkv", position: 2 };
  }
  it("shows ready converted files without opening the queue", () => {
    expect(ipadStatusForPath("/downloads/Film.mkv", [exportJob("1","ready","compact")])?.status).toBe("ready");
  });
  it("prioritizes compact export and ongoing preparation over an old fast result", () => {
    const jobs = [
      exportJob("1","ready","fast"),
      exportJob("2","ready","compact"),
      exportJob("3","queued","compact"),
    ];
    expect(ipadStatusForPath("/downloads/Film.mkv", jobs)?.status).toBe("queued");
    expect(ipadStatusForPath("/downloads/Film.mkv", jobs.slice(0,2))?.profile).toBe("compact");
    const highQuality = exportJob("4","ready","balanced");
    expect(ipadStatusForPath("/downloads/Film.mkv", [...jobs.slice(0,2),highQuality])?.profile).toBe("balanced");
  });
  it("does not show stale canceled or unrelated jobs", () => {
    expect(ipadStatusForPath("/downloads/Film.mkv", [exportJob("1","deleted","compact")])).toBeUndefined();
    expect(ipadStatusForPath("/downloads/Other.mkv", [exportJob("1","ready","compact")])).toBeUndefined();
  });
});
