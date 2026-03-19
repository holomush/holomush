import { test, expect } from "@playwright/test";

test.describe("Web Client E2E", () => {
  test("guest can connect, send say, and see event echo", async ({ page }) => {
    await page.goto("/");

    await expect(page.getByRole("heading", { name: "HoloMUSH Web Client" })).toBeVisible();

    await page.getByRole("button", { name: "Connect as Guest" }).click();

    await expect(page.getByText("Connected as")).toBeVisible({ timeout: 10000 });

    await page.getByPlaceholder("say hello").fill("say hello world");
    await page.getByRole("button", { name: "Send" }).click();

    await expect(page.getByText("hello world")).toBeVisible({ timeout: 10000 });

    await page.getByRole("button", { name: "Disconnect" }).click();
    await expect(page.getByRole("button", { name: "Connect as Guest" })).toBeVisible();
  });
});
