import { expect, test } from "@playwright/test";

test.skip(process.env.RUN_FIREBASE_EMULATOR_TESTS !== "1", "exige backend e Firebase Emulators locais");

test("cadastro, primeiro perfil e criação de hábito", async ({ page }, testInfo) => {
  const email = `e2e-${Date.now()}-${testInfo.workerIndex}@example.test`;
  await page.goto("/cadastro");
  await page.getByLabel("Apelido").fill("Teste E2E");
  await page.getByLabel("Idade").fill("15");
  await page.getByLabel("E-mail").fill(email);
  await page.getByLabel("Senha").fill("senha-e2e-123");
  await page.getByRole("button", { name: "Criar minha conta" }).click();
  await expect(page).toHaveURL(/\/perfil$/);

  await page.goto("/criar-habito");
  await page.getByLabel("Título *").fill("Ler no E2E");
  await page.getByLabel("Descrição *").fill("Hábito criado pela homologação local.");
  await page.locator('input[name="weekdays"][value="1"]').check({ force: true });
  await page.getByLabel("Horário *").fill("19:00");
  await page.getByLabel("Meta semanal *").fill("1");
  await page.getByRole("button", { name: "Salvar hábito" }).click();
  await expect(page).toHaveURL(/\/meus-habitos/);
  await expect(page.getByText("Ler no E2E")).toBeVisible();
});
