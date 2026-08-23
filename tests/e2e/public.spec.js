import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

for (const path of ["/", "/aprenda-4rs", "/entrar", "/cadastro"]) {
  test(`${path} responde, reflui e não tem violações axe críticas`, async ({ page }) => {
    await page.goto(path);
    await expect(page.locator("body")).toBeVisible();
    await expect(page.locator("html")).toHaveAttribute("lang", "pt-BR");
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth);
    expect(overflow).toBe(false);
    const results = await new AxeBuilder({ page }).analyze();
    expect(results.violations.filter((item) => item.impact === "critical")).toEqual([]);
  });
}

test("headers defensivos estão presentes", async ({ request }) => {
  const response = await request.get("/perfil", { maxRedirects: 0 });
  expect(response.headers()["content-security-policy"]).toContain("worker-src 'self'");
  expect(response.headers()["x-content-type-options"]).toBe("nosniff");
  expect(response.headers()["x-request-id"]).toBeTruthy();
  expect(response.headers()["cache-control"]).toBe("no-store");
});
