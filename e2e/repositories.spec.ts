import { test, expect } from "@playwright/test";
import { loginAsDefault, createTestApi, openWorkspaceMenu } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("Repositories settings", () => {
  let api: TestApiClient;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    await loginAsDefault(page);
  });

  test.afterEach(async () => {
    if (api) {
      await api.cleanup();
    }
  });

  test("add, edit, and delete a repository", async ({ page }) => {
    // Navigate to settings
    await openWorkspaceMenu(page);
    await page.locator("text=Settings").click();
    await page.waitForURL("**/settings");

    // Click the Repositories tab in the vertical nav
    await page.getByRole("tab", { name: "Repositories" }).click();

    // Empty state should be visible
    await expect(
      page.getByText(/no repositories registered yet/i),
    ).toBeVisible();

    // Add first repository via the empty-state button
    await page
      .getByRole("button", { name: /add your first repository/i })
      .click();

    // Fill the dialog form
    await page
      .getByLabel(/url/i)
      .fill("https://github.com/multica-ai/test.git");
    await page.getByLabel(/name/i).fill("test-repo");
    await page.getByRole("button", { name: /add repository/i }).click();

    // Repo should appear in the list
    await expect(page.getByText("test-repo")).toBeVisible();
    await expect(
      page.getByText("https://github.com/multica-ai/test.git"),
    ).toBeVisible();

    // Edit the repository name
    await page.getByRole("button", { name: /edit/i }).first().click();
    await page.getByLabel(/name/i).fill("test-renamed");
    await page.getByRole("button", { name: /save changes/i }).click();
    await expect(page.getByText("test-renamed")).toBeVisible();

    // Delete the repository (accept the confirm dialog)
    page.once("dialog", (dialog) => dialog.accept());
    await page.getByRole("button", { name: /delete/i }).first().click();

    // Back to empty state
    await expect(
      page.getByText(/no repositories registered yet/i),
    ).toBeVisible();
  });
});
