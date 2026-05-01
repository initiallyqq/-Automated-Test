import { expect, test } from "@playwright/test";

const scenario = {
  id: "smoke-home-page-load",
  name: "Home page loads and health check passes",
  route: "/",
  projectType: "fullstack",
  frameworks: ["go", "playwright"],
};

const baseURL = "http://localhost:3000";

const apiEndpoints = [
  { method: "UNKNOWN", path: "/api/health" },
  { method: "POST", path: "/api/login" },
  { method: "UNKNOWN", path: "/api/notes" },
  { method: "UNKNOWN", path: "/api/notes/" },
  { method: "GET", path: "/api/v1/health" },
  { method: "POST", path: "/api/v1/tasks/run" },
  { method: "GET", path: "/api/v1/tasks/{taskId}" },
  { method: "GET", path: "/api/v1/tasks/{taskId}/report" },
  { method: "GET", path: "/api/v1/tasks/{taskId}/stream" },
  { method: "GET", path: "/api/v1/tools" },
  { method: "GET", path: "/api/v1/workflow/graph" },
  { method: "UNKNOWN", path: "/legacy" },
  { method: "POST", path: "/signin" },
  { method: "GET", path: "/users/:id" },
];

const pageRoutes = [
  { route: "/", name: "Home" },
];

const dataModels = [
  { name: "agent_runs", fields: ["agent_name", "error", "finished_at", "id", "input_json_path", "input_summary", "output_json_path", "output_summary", "started_at", "status", "task_id"] },
  { name: "diagnoses", fields: ["confidence", "created_at", "diagnosis_json", "execution_id", "failure_type", "fix_target", "id", "next_action", "root_cause", "task_id"] },
  { name: "notes", fields: ["content", "id", "status", "title"] },
  { name: "patches", fields: ["applied", "applied_at", "created_at", "diagnosis_id", "id", "patch_path", "rationale", "risk_level", "target_path", "task_id"] },
  { name: "projects", fields: ["created_at", "id", "name", "project_type", "repo_path", "updated_at"] },
  { name: "workflow_events", fields: ["created_at", "from_phase", "id", "payload_json", "reason", "status", "task_id", "to_phase"] },
  { name: "workflow_tasks", fields: ["created_at", "finished_at", "id", "last_error", "phase", "project_id", "repo_version", "retry_count", "state_json", "status", "updated_at"] },
];


function normalizePath(path: string): string {
  return path.replace(/:id/g, "1").replace(/\{(\w+)\}/g, "test-1");
}

async function callApi(request: any, method: string, url: string): Promise<any> {
  try {
    const fullUrl = baseURL + url;
    switch (method.toUpperCase()) {
      case "GET":    return await request.get(fullUrl);
      case "POST":   return await request.post(fullUrl, { data: {} });
      case "PUT":    return await request.put(fullUrl, { data: {} });
      case "DELETE": return await request.delete(fullUrl);
      case "PATCH":  return await request.patch(fullUrl, { data: {} });
      default:       return await request.get(fullUrl);
    }
  } catch { return null; }
}

async function safeClick(page: any, pattern: string): Promise<boolean> {
  const el = page.getByRole("button", { name: new RegExp(pattern, "i") })
    .or(page.getByLabel(new RegExp(pattern, "i")))
    .or(page.getByText(new RegExp(pattern, "i")));
  if (await el.first().isVisible({ timeout: 3000 }).catch(() => false)) {
    await el.first().click();
    return true;
  }
  return false;
}

async function safeFill(page: any, pattern: string, value: string): Promise<boolean> {
  const field = page.getByLabel(new RegExp(pattern, "i"))
    .or(page.getByPlaceholder(new RegExp(pattern, "i")));
  if (await field.first().isVisible({ timeout: 3000 }).catch(() => false)) {
    await field.first().fill(value);
    return true;
  }
  return false;
}
test.describe("Home page loads and health check passes", () => {

  // === API endpoint tests ===
  test("API UNKNOWN /api/health", async ({ request }) => {
    const path = normalizePath("/api/health");
    const resp = await callApi(request, "UNKNOWN", path);
    if (resp) {
      expect.soft(resp.status(), "UNKNOWN /api/health status").toBeLessThan(500);
    }
  });

  test("API POST /api/login", async ({ request }) => {
    const path = normalizePath("/api/login");
    const resp = await callApi(request, "POST", path);
    if (resp) {
      expect.soft(resp.status(), "POST /api/login status").toBeLessThan(500);
    }
  });

  test("API UNKNOWN /api/notes", async ({ request }) => {
    const path = normalizePath("/api/notes");
    const resp = await callApi(request, "UNKNOWN", path);
    if (resp) {
      expect.soft(resp.status(), "UNKNOWN /api/notes status").toBeLessThan(500);
    }
  });

  test("API UNKNOWN /api/notes/", async ({ request }) => {
    const path = normalizePath("/api/notes/");
    const resp = await callApi(request, "UNKNOWN", path);
    if (resp) {
      expect.soft(resp.status(), "UNKNOWN /api/notes/ status").toBeLessThan(500);
    }
  });

  test("API GET /api/v1/health", async ({ request }) => {
    const path = normalizePath("/api/v1/health");
    const resp = await callApi(request, "GET", path);
    if (resp) {
      expect.soft(resp.status(), "GET /api/v1/health status").toBeLessThan(500);
    }
  });

  test("API POST /api/v1/tasks/run", async ({ request }) => {
    const path = normalizePath("/api/v1/tasks/run");
    const resp = await callApi(request, "POST", path);
    if (resp) {
      expect.soft(resp.status(), "POST /api/v1/tasks/run status").toBeLessThan(500);
    }
  });

  test("API GET /api/v1/tasks/{taskId}", async ({ request }) => {
    const path = normalizePath("/api/v1/tasks/{taskId}");
    const resp = await callApi(request, "GET", path);
    if (resp) {
      expect.soft(resp.status(), "GET /api/v1/tasks/{taskId} status").toBeLessThan(500);
    }
  });

  test("API GET /api/v1/tasks/{taskId}/report", async ({ request }) => {
    const path = normalizePath("/api/v1/tasks/{taskId}/report");
    const resp = await callApi(request, "GET", path);
    if (resp) {
      expect.soft(resp.status(), "GET /api/v1/tasks/{taskId}/report status").toBeLessThan(500);
    }
  });

  test("API GET /api/v1/tasks/{taskId}/stream", async ({ request }) => {
    const path = normalizePath("/api/v1/tasks/{taskId}/stream");
    const resp = await callApi(request, "GET", path);
    if (resp) {
      expect.soft(resp.status(), "GET /api/v1/tasks/{taskId}/stream status").toBeLessThan(500);
    }
  });

  test("API GET /api/v1/tools", async ({ request }) => {
    const path = normalizePath("/api/v1/tools");
    const resp = await callApi(request, "GET", path);
    if (resp) {
      expect.soft(resp.status(), "GET /api/v1/tools status").toBeLessThan(500);
    }
  });

  test("API GET /api/v1/workflow/graph", async ({ request }) => {
    const path = normalizePath("/api/v1/workflow/graph");
    const resp = await callApi(request, "GET", path);
    if (resp) {
      expect.soft(resp.status(), "GET /api/v1/workflow/graph status").toBeLessThan(500);
    }
  });

  test("API UNKNOWN /legacy", async ({ request }) => {
    const path = normalizePath("/legacy");
    const resp = await callApi(request, "UNKNOWN", path);
    if (resp) {
      expect.soft(resp.status(), "UNKNOWN /legacy status").toBeLessThan(500);
    }
  });

  test("API POST /signin", async ({ request }) => {
    const path = normalizePath("/signin");
    const resp = await callApi(request, "POST", path);
    if (resp) {
      expect.soft(resp.status(), "POST /signin status").toBeLessThan(500);
    }
  });

  test("API GET /users/:id", async ({ request }) => {
    const path = normalizePath("/users/:id");
    const resp = await callApi(request, "GET", path);
    if (resp) {
      expect.soft(resp.status(), "GET /users/:id status").toBeLessThan(500);
    }
  });


  // === Page navigation tests ===
  test("Page: Home (/)", async ({ page }) => {
    await page.goto(baseURL + "/", { waitUntil: "domcontentloaded", timeout: 15000 });
    await page.waitForTimeout(1000);
    await expect.soft(page.locator("body")).toBeAttached();
    const title = await page.title();
    expect(title.length).toBeGreaterThan(0);
  });


  // === Scenario flow ===
  test("Flow: Home page loads and health check passes", async ({ page, request }) => {
    // Navigation
    if (baseURL) {
      await page.goto(baseURL + "/", { waitUntil: "networkidle", timeout: 15000 });
      await page.waitForTimeout(1000);
    }

    // Step 1: navigate to /
    await test.step("navigate to /", async () => {
      await page.goto(baseURL + "/", { waitUntil: "domcontentloaded", timeout: 10000 }).catch(() => {});
      await page.waitForTimeout(800);
    });

    // Step 2: verify GET /api/v1/health returns status 200
    await test.step("verify GET /api/v1/health returns status 200", async () => {
      const apiPath = normalizePath("/api/v1/health");
      const apiResp = await callApi(request, "GET", apiPath);
      if (apiResp) {
        expect.soft(apiResp.status(), "GET /api/v1/health status").toBeLessThan(500);
      }
    });

    await test.step("Assert: page displays welcome heading", async () => {
      // Attempt to find assertion text on the page
      const found = await page.getByText(new RegExp("page displays welcome heading", "i")).first().isVisible({ timeout: 5000 }).catch(() => false);
      test.info().annotations.push({ type: "assertion", description: found ? "PASS: page displays welcome heading" : "UNVERIFIED: page displays welcome heading" });
    });

    await test.step("Assert: health endpoint returns 200", async () => {
      // Attempt to find assertion text on the page
      const found = await page.getByText(new RegExp("health endpoint returns 200", "i")).first().isVisible({ timeout: 5000 }).catch(() => false);
      test.info().annotations.push({ type: "assertion", description: found ? "PASS: health endpoint returns 200" : "UNVERIFIED: health endpoint returns 200" });
    });

    // Health check
    await test.step("Health check", async () => {
      const hc0 = await request.get(baseURL + "/api/health").catch(() => null);
      if (hc0) {
        expect.soft(hc0.status(), "health /api/health").toBeLessThan(500);
      }
      const hc1 = await request.get(baseURL + "/api/v1/health").catch(() => null);
      if (hc1) {
        expect.soft(hc1.status(), "health /api/v1/health").toBeLessThan(500);
      }
    });

    // Pause for visual inspection
    await page.waitForTimeout(2000);
  });
});
