import { expect, test } from "@playwright/test";

const API_BASE = process.env.API_BASE_URL ?? "http://localhost:8080";

test.describe("API health", () => {
  test("liveness probe returns ok", async ({ request }) => {
    const res = await request.get(`${API_BASE}/api/orca/healthz`);
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body).toHaveProperty("status");
  });

  test("readiness probe returns ok", async ({ request }) => {
    const res = await request.get(`${API_BASE}/api/orca/readyz`);
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body).toHaveProperty("status");
  });

  test("workflows list endpoint responds", async ({ request }) => {
    const res = await request.get(`${API_BASE}/api/orca/workflows?limit=1&offset=0`);
    // 200 OK or 401 Unauthorized are both valid — just not 5xx
    expect(res.status()).toBeLessThan(500);
  });
});

test.describe("UI", () => {
  test("login page loads", async ({ page }) => {
    await page.goto("/");
    // The app either shows a login page or redirects to /login — just check it renders
    await expect(page).not.toHaveTitle(/error/i);
    await expect(page.locator("body")).not.toBeEmpty();
  });

  test("UI serves static assets", async ({ request }) => {
    const res = await request.get("/");
    expect(res.status()).toBeLessThan(500);
  });
});
