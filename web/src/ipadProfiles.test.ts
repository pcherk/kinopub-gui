import { describe, expect, it } from "vitest";
import { IPAD_PROFILES, ipadProfileName } from "./ipadProfiles";

describe("iPad profile labels", () => {
  it("lists exactly three user-facing profiles in the recommended order", () => {
    expect(IPAD_PROFILES.map((p) => p.title)).toEqual([
      "Оптимальный", "Быстрый / оригинал", "Экономия места",
    ]);
    expect(IPAD_PROFILES.map((p) => p.id)).toEqual(["balanced", "fast", "compact"]);
  });
  it("uses the same names in library and export queue", () => {
    for (const p of IPAD_PROFILES) expect(ipadProfileName(p.id)).toBe(p.title);
  });
});
