import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: ".",
  timeout: 30_000,
  retries: 0,
  reporter: "list",
  webServer: process.env.E2E_START_SERVER === "1" ? {
    command: "cd ../.. && go run ./cmd/web",
    url: process.env.E2E_BASE_URL || "http://127.0.0.1:8080/health",
    reuseExistingServer: false,
    timeout: 120_000
  } : undefined,
  use: {
    baseURL: process.env.E2E_BASE_URL || "http://127.0.0.1:8080",
    trace: "retain-on-failure"
  },
  projects: [
    { name: "chromium-desktop", use: { ...devices["Desktop Chrome"] } },
    { name: "firefox-desktop", use: { ...devices["Desktop Firefox"] } },
    { name: "chromium-mobile", use: { ...devices["Pixel 7"] } }
  ]
});
