import { describe, expect, it } from "vitest";
import {
  formatDate,
  formatRelative,
  toneForStatus,
  prettyJson,
  workflowModes,
  scopeKinds,
} from "../../lib/orca/presentation";

describe("formatDate", () => {
  it("returns - for null input", () => {
    expect(formatDate(null)).toBe("-");
  });

  it("returns - for undefined input", () => {
    expect(formatDate(undefined)).toBe("-");
  });

  it("returns the raw value for an invalid date string", () => {
    expect(formatDate("not-a-date")).toBe("not-a-date");
  });

  it("returns a formatted string for a valid ISO date", () => {
    const result = formatDate("2024-01-15T10:30:00Z");
    expect(result).not.toBe("-");
    expect(typeof result).toBe("string");
    expect(result.length).toBeGreaterThan(0);
  });
});

describe("formatRelative", () => {
  it("returns - for null input", () => {
    expect(formatRelative(null)).toBe("-");
  });

  it("returns - for undefined input", () => {
    expect(formatRelative(undefined)).toBe("-");
  });

  it("returns the raw value for an invalid date string", () => {
    expect(formatRelative("not-a-date")).toBe("not-a-date");
  });

  it("returns a relative string for a recent time", () => {
    const recent = new Date(Date.now() - 5 * 60_000).toISOString();
    const result = formatRelative(recent);
    expect(typeof result).toBe("string");
    expect(result).not.toBe("-");
  });
});

describe("toneForStatus", () => {
  it("returns success tone for completed", () => {
    expect(toneForStatus("completed")).toContain("success");
  });

  it("returns success tone for ready", () => {
    expect(toneForStatus("ready")).toContain("success");
  });

  it("returns warning tone for running", () => {
    expect(toneForStatus("running")).toContain("warning");
  });

  it("returns warning tone for pending", () => {
    expect(toneForStatus("pending")).toContain("warning");
  });

  it("returns danger tone for failed", () => {
    expect(toneForStatus("failed")).toContain("danger");
  });

  it("returns danger tone for cancelled", () => {
    expect(toneForStatus("cancelled")).toContain("danger");
  });

  it("returns muted tone for unknown status", () => {
    expect(toneForStatus("unknown")).toContain("muted");
  });

  it("returns muted tone for undefined", () => {
    expect(toneForStatus(undefined)).toContain("muted");
  });
});

describe("prettyJson", () => {
  it("formats an object with 2-space indentation", () => {
    const result = prettyJson({ key: "value" });
    expect(result).toBe('{\n  "key": "value"\n}');
  });

  it("handles arrays", () => {
    const result = prettyJson([1, 2, 3]);
    expect(result).toBe("[\n  1,\n  2,\n  3\n]");
  });

  it("handles null", () => {
    expect(prettyJson(null)).toBe("null");
  });
});

describe("workflowModes", () => {
  it("contains expected modes", () => {
    const values = workflowModes.map((m) => m.value);
    expect(values).toContain("software");
    expect(values).toContain("research");
    expect(values).toContain("ops");
  });

  it("every mode has a label", () => {
    workflowModes.forEach((m) => {
      expect(m.label.length).toBeGreaterThan(0);
    });
  });
});

describe("scopeKinds", () => {
  it("contains global, org, and team", () => {
    const values = scopeKinds.map((s) => s.value);
    expect(values).toContain("global");
    expect(values).toContain("org");
    expect(values).toContain("team");
  });
});
