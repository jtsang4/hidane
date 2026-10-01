import { expect, test } from "./fixtures.js";

test.describe("token gate", () => {
  test.use({
    withToken: false,
    // The wrong token is meant to be refused: the browser logs the 401s it gets back.
    allowedErrors: [/401|Unauthorized/i, /EventSource/i],
  });

  test("asks for the token, rejects a wrong one and opens the app with the right one", async ({ page }) => {
    await page.goto("/");

    const gate = page.getByText("输入 API Token（HIDANE_API_TOKEN）。");
    const tokenInput = page.getByPlaceholder("token", { exact: true });
    const enter = page.getByRole("button", { name: "进入" });
    const chatLink = page.getByRole("link", { name: "会话" });

    await expect(gate).toBeVisible();
    await expect(tokenInput).toHaveAttribute("type", "password");
    await expect(enter).toBeDisabled();
    await expect(chatLink).toHaveCount(0);

    // A wrong token gets past the client gate, is refused by the server, and lands back here.
    const refused = page.waitForResponse((response) => response.url().includes("/api/") && response.status() === 401);
    await tokenInput.fill("not-the-token");
    await enter.click();
    expect((await refused).status()).toBe(401);
    await expect(page.getByRole("alert").filter({ hasText: "Token 无效，请重新输入。" })).toBeVisible();
    await expect(gate).toBeVisible();
    await expect(chatLink).toHaveCount(0);
    expect(await page.evaluate(() => window.localStorage.getItem("hidane-token"))).toBeNull();

    await tokenInput.fill("e2e-token");
    await tokenInput.press("Enter");
    await expect(chatLink).toBeVisible();
    await expect(page.getByPlaceholder("说点什么…")).toBeVisible();
    await expect(gate).toHaveCount(0);
    await expect(page.getByRole("status", { name: "事件流状态: 实时" })).toBeAttached();
    expect(await page.evaluate(() => window.localStorage.getItem("hidane-token"))).toBe("e2e-token");

    // The token survives a reload: the gate does not come back.
    await page.reload();
    await expect(chatLink).toBeVisible();
    await expect(gate).toHaveCount(0);
  });

  test("the API refuses requests without the bearer token", async ({ request }) => {
    expect((await request.get("/health")).status()).toBe(200);
    expect((await request.get("/api/status")).status()).toBe(401);
    expect((await request.get("/api/status", { headers: { authorization: "Bearer wrong" } })).status()).toBe(401);
  });
});
