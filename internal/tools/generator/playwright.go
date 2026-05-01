package generator

import (
	"context"
	"crypto/sha256"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"automated-test/internal/domain/workflow"
)

type PlaywrightGenerator struct{}

type GenerateRequest struct {
	ProjectRoot    string
	ProjectProfile *workflow.ProjectProfile
	PageGraph      *workflow.PageGraph
	ApiGraph       *workflow.ApiGraph
	DataModelGraph *workflow.DataModelGraph
	Scenarios      []workflow.Scenario
	BaseURL        string
	Visual         bool
}

type GenerateResult struct {
	Artifacts []workflow.TestArtifact
}

func NewPlaywrightGenerator() *PlaywrightGenerator {
	return &PlaywrightGenerator{}
}

func (g *PlaywrightGenerator) Generate(ctx context.Context, req GenerateRequest) (GenerateResult, error) {
	if err := ctx.Err(); err != nil {
		return GenerateResult{}, err
	}
	if req.ProjectRoot == "" {
		req.ProjectRoot = "."
	}
	root, err := filepath.Abs(req.ProjectRoot)
	if err != nil {
		return GenerateResult{}, err
	}
	if err := removeGeneratedSpecs(root); err != nil {
		return GenerateResult{}, err
	}

	files := map[string]string{
		filepath.Join(root, "e2e", "playwright.config.ts"): playwrightConfig(),
	}
	scenarios := req.Scenarios
	if len(scenarios) == 0 {
		scenarios = defaultScenarios(req)
	}
	if req.Visual {
		scenario := workflow.Scenario{ID: "visible-user-flow", Name: "Visible user flow"}
		if len(scenarios) > 0 {
			scenario = scenarios[0]
			scenario.ID = "visible-user-flow"
			scenario.Name = firstNonEmptyString(scenario.Name, "Visible user flow")
		}
		files[filepath.Join(root, "e2e", "specs", "visual", "visible-user-flow.spec.ts")] = visualSpec(req, scenario)
	} else {
		for _, scenario := range scenarios {
			if scenario.ID == "" {
				continue
			}
			path := filepath.Join(root, "e2e", "specs", "smoke", scenario.ID+".spec.ts")
			files[path] = smokeSpec(req, scenario)
		}
	}

	artifacts := make([]workflow.TestArtifact, 0, len(files))
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return GenerateResult{}, err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return GenerateResult{}, err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return GenerateResult{}, err
		}
		rel = filepath.ToSlash(rel)
		artifacts = append(artifacts, workflow.TestArtifact{
			ID:          artifactID(rel),
			ScenarioID:  scenarioIDFromPath(rel),
			Type:        artifactType(rel),
			Path:        rel,
			Language:    "typescript",
			ContentHash: hash(content),
		})
	}

	return GenerateResult{Artifacts: artifacts}, nil
}

func visualSpec(req GenerateRequest, scenario workflow.Scenario) string {
	route := visualRoute(req, scenarioRoute(scenario))
	baseURL := strings.TrimRight(req.BaseURL, "/")
	var b strings.Builder
	b.WriteString("import { expect, test } from \"@playwright/test\";\n\n")
	b.WriteString(fmt.Sprintf("const baseURL = %q;\n", baseURL))
	b.WriteString(fmt.Sprintf("const route = %q;\n\n", route))
	b.WriteString(helpersCode())
	b.WriteString("\ntest.describe(\"Visible browser automation\", () => {\n")
	b.WriteString("  test(\"MCP-like visible user flow\", async ({ page }) => {\n")
	b.WriteString("    await test.step(\"Open application\", async () => {\n")
	b.WriteString("      await page.goto(baseURL + route, { waitUntil: \"domcontentloaded\", timeout: 15000 });\n")
	b.WriteString("      await expect(page.locator(\"body\")).toBeVisible();\n")
	b.WriteString("    });\n\n")
	b.WriteString("    await test.step(\"Fill visible form\", async () => {\n")
	b.WriteString("      await safeFill(page, \"Title\", \"Visible automation note\");\n")
	b.WriteString("      await safeFill(page, \"Content\", \"Created by the built-in visual runner\");\n")
	b.WriteString("      const status = page.getByLabel(\"Status\");\n")
	b.WriteString("      if (await status.isVisible({ timeout: 2000 }).catch(() => false)) {\n")
	b.WriteString("        await status.selectOption(\"done\").catch(() => {});\n")
	b.WriteString("      }\n")
	b.WriteString("    });\n\n")
	b.WriteString("    await test.step(\"Submit and verify\", async () => {\n")
	b.WriteString("      const clicked = await safeClick(page, \"Add Note|Create|Submit|Save\");\n")
	b.WriteString("      expect(clicked).toBeTruthy();\n")
	b.WriteString("      await expect(page.getByText(\"Visible automation note\")).toBeVisible({ timeout: 5000 });\n")
	b.WriteString("    });\n")
	for _, step := range scenario.Steps {
		step = strings.TrimSpace(step)
		if step == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("    test.info().annotations.push({ type: \"planned-step\", description: %q });\n", step))
	}
	b.WriteString("  });\n")
	b.WriteString("});\n")
	return b.String()
}

func playwrightConfig() string {
	return `import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./specs",
  timeout: 60_000,
  expect: { timeout: 15_000 },
  retries: 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: [
    ["list"],
    ["json", { outputFile: "../artifacts/playwright-report.json" }],
    ["html", { outputFolder: "../artifacts/playwright-report", open: "never" }],
  ],
  use: {
    trace: "on",
    screenshot: "on",
    video: "on",
    launchOptions: {
      slowMo: Number(process.env.PLAYWRIGHT_SLOW_MO || "0"),
    },
    actionTimeout: 10_000,
    navigationTimeout: 15_000,
  },
  outputDir: "../artifacts/test-results",
});
`
}

func smokeSpec(req GenerateRequest, scenario workflow.Scenario) string {
	var b strings.Builder

	name := scenario.Name
	if name == "" {
		name = "Fullstack smoke flow"
	}
	route := scenarioRoute(scenario)
	projectType := "unknown"
	if req.ProjectProfile != nil && strings.TrimSpace(req.ProjectProfile.ProjectType) != "" {
		projectType = strings.TrimSpace(req.ProjectProfile.ProjectType)
	}
	frameworks := projectFrameworks(req.ProjectProfile)

	// --- imports ---
	b.WriteString("import { expect, test } from \"@playwright/test\";\n\n")

	// --- scenario metadata ---
	b.WriteString("const scenario = {\n")
	b.WriteString(fmt.Sprintf("  id: %q,\n", scenario.ID))
	b.WriteString(fmt.Sprintf("  name: %q,\n", name))
	b.WriteString(fmt.Sprintf("  route: %q,\n", route))
	b.WriteString(fmt.Sprintf("  projectType: %q,\n", projectType))
	b.WriteString(fmt.Sprintf("  frameworks: %s,\n", quotedStringSlice(frameworks)))
	b.WriteString(fmt.Sprintf("  preconditions: %s,\n", quotedStringSlice(scenario.Preconditions)))
	b.WriteString(fmt.Sprintf("  apis: %s,\n", quotedStringSlice(apiSummaries(req.ApiGraph))))
	b.WriteString(fmt.Sprintf("  models: %s,\n", quotedStringSlice(modelSummaries(req.DataModelGraph))))
	b.WriteString("};\n\n")

	baseURL := strings.TrimRight(req.BaseURL, "/")
	if baseURL != "" {
		b.WriteString(fmt.Sprintf("const baseURL = %q;\n\n", baseURL))
	} else {
		b.WriteString("const baseURL = \"\";\n\n")
	}

	// --- API endpoints ---
	writeApiEndpoints(&b, req.ApiGraph)

	// --- Page routes ---
	writePageRoutes(&b, req.PageGraph)

	// --- Data models ---
	writeDataModels(&b, req.DataModelGraph)

	// --- Inline helpers ---
	b.WriteString(helpersCode())

	// --- Describe block ---
	b.WriteString(fmt.Sprintf("test.describe(%q, () => {\n", name))

	// Block A: API tests
	writeApiTestBlocks(&b, req.ApiGraph, baseURL)

	// Block A2: Application-level data mutation tests
	writeDataMutationTestBlocks(&b, req, baseURL)

	// Block B: Page tests
	writePageTestBlocks(&b, req.PageGraph, baseURL)

	// Block C: Scenario flow
	writeScenarioFlow(&b, req, scenario, baseURL)

	b.WriteString("});\n")

	return b.String()
}

// --- helper functions for TypeScript ---

func helpersCode() string {
	return `
function normalizePath(path: string): string {
  return path.replace(/:id/g, "1").replace(/\{(\w+)\}/g, "test-1");
}

async function callApi(request: any, method: string, url: string): Promise<any> {
  try {
    const fullUrl = baseURL + url;
    const payload = samplePayloadFor(method, url);
    switch (method.toUpperCase()) {
      case "GET":    return await request.get(fullUrl);
      case "POST":   return await request.post(fullUrl, { data: payload });
      case "PUT":    return await request.put(fullUrl, { data: payload });
      case "DELETE": return await request.delete(fullUrl);
      case "PATCH":  return await request.patch(fullUrl, { data: payload });
      default:       return await request.get(fullUrl);
    }
  } catch { return null; }
}

function samplePayloadFor(method: string, url: string): any {
  if (!/^(POST|PUT|PATCH)$/i.test(method)) {
    return {};
  }
  const model = modelForUrl(url);
  if (!model || !Array.isArray(model.fields) || model.fields.length === 0) {
    return {
      name: "Auto test " + Date.now(),
      title: "Auto test " + Date.now(),
      content: "Created by generated Playwright test",
    };
  }
  const payload: Record<string, any> = {};
  for (const field of model.fields) {
    const key = String(field);
    if (/^(id|uuid|created_at|createdAt|updated_at|updatedAt|deleted_at|deletedAt)$/i.test(key)) {
      continue;
    }
    payload[key] = sampleValueForField(key);
  }
  if (Object.keys(payload).length === 0) {
    payload.name = "Auto test " + Date.now();
  }
  return payload;
}

function modelForUrl(url: string): any {
  const normalized = url.toLowerCase();
  for (const model of dataModels) {
    const name = String(model.name || "").toLowerCase();
    const singular = name.endsWith("s") ? name.slice(0, -1) : name;
    if (name && (normalized.includes("/" + name) || normalized.includes("/" + singular))) {
      return model;
    }
  }
  return dataModels[0] || null;
}

function sampleValueForField(field: string): any {
  const key = field.toLowerCase();
  if (key.includes("email")) return "autotest+" + Date.now() + "@example.test";
  if (key.includes("status")) return "todo";
  if (key.includes("title") || key.includes("name")) return "Auto test " + Date.now();
  if (key.includes("content") || key.includes("description") || key.includes("body")) return "Created by generated Playwright test";
  if (key.startsWith("is_") || key.startsWith("has_") || key === "active" || key === "enabled") return true;
  if (key.endsWith("_count") || key.includes("amount") || key.includes("price") || key.includes("total")) return 1;
  return "autotest-" + Date.now();
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

async function fillKnownNoteForm(page: any): Promise<void> {
  await safeFill(page, "Title", "Test Note");
  await safeFill(page, "Content", "Test Content");
  const status = page.getByLabel("Status");
  if (await status.first().isVisible({ timeout: 2000 }).catch(() => false)) {
    await status.first().selectOption("done").catch(() => {});
  }
}
`
}

// --- write metadata blocks ---

func writeApiEndpoints(b *strings.Builder, graph *workflow.ApiGraph) {
	if graph == nil || len(graph.Endpoints) == 0 {
		return
	}
	b.WriteString("const apiEndpoints = [\n")
	for _, ep := range graph.Endpoints {
		method := firstNonEmptyString(ep.Method, "GET")
		path := firstNonEmptyString(ep.Path, "/unknown")
		b.WriteString(fmt.Sprintf("  { method: %q, path: %q },\n", method, path))
	}
	b.WriteString("];\n\n")
}

func writePageRoutes(b *strings.Builder, graph *workflow.PageGraph) {
	if graph == nil || len(graph.Pages) == 0 {
		return
	}
	b.WriteString("const pageRoutes = [\n")
	for _, pg := range graph.Pages {
		route := firstNonEmptyString(pg.Route, "/")
		name := firstNonEmptyString(pg.Name, pg.ID, "Page")
		b.WriteString(fmt.Sprintf("  { route: %q, name: %q },\n", route, name))
	}
	b.WriteString("];\n\n")
}

func writeDataModels(b *strings.Builder, graph *workflow.DataModelGraph) {
	if graph == nil || len(graph.Models) == 0 {
		b.WriteString("const dataModels: Array<{ name: string; fields: string[] }> = [];\n\n")
		return
	}
	b.WriteString("const dataModels = [\n")
	for _, m := range graph.Models {
		b.WriteString(fmt.Sprintf("  { name: %q, fields: %s },\n", m.Name, quotedStringSlice(m.Fields)))
	}
	b.WriteString("];\n\n")
}

// --- API test blocks ---

func writeApiTestBlocks(b *strings.Builder, graph *workflow.ApiGraph, baseURL string) {
	if graph == nil || len(graph.Endpoints) == 0 {
		return
	}
	b.WriteString("\n  // === API endpoint tests ===\n")
	for _, ep := range graph.Endpoints {
		if ep.Method == "UNKNOWN" && (ep.Path == "" || ep.Path == "/unknown") {
			continue
		}
		method := firstNonEmptyString(ep.Method, "GET")
		path := firstNonEmptyString(ep.Path, "/unknown")
		b.WriteString(fmt.Sprintf(
			"  test(%q, async ({ request }) => {\n"+
				"    const path = normalizePath(%q);\n"+
				"    const resp = await callApi(request, %q, path);\n"+
				"    if (resp) {\n"+
				"      expect.soft(resp.status(), %q).toBeLessThan(500);\n"+
				"    }\n"+
				"  });\n\n",
			fmt.Sprintf("API %s %s", method, path),
			path, method,
			fmt.Sprintf("%s %s status", method, path),
		))
	}
}

func writeDataMutationTestBlocks(b *strings.Builder, req GenerateRequest, baseURL string) {
	if baseURL == "" || req.ApiGraph == nil || len(req.ApiGraph.Endpoints) == 0 || req.DataModelGraph == nil || len(req.DataModelGraph.Models) == 0 {
		return
	}
	createEndpoints := mutationEndpoints(req.ApiGraph, "POST")
	if len(createEndpoints) == 0 {
		return
	}
	b.WriteString("\n  // === Application data mutation tests ===\n")
	for _, ep := range createEndpoints {
		path := firstNonEmptyString(ep.Path, "/unknown")
		readPath := collectionReadPath(req.ApiGraph, path)
		b.WriteString(fmt.Sprintf(
			"  test(%q, async ({ request }) => {\n"+
				"    const path = normalizePath(%q);\n"+
				"    const payload = samplePayloadFor(\"POST\", path);\n"+
				"    const marker = String(payload.title || payload.name || payload.email || payload.content || Object.values(payload)[0] || \"autotest\");\n"+
				"    const created = await request.post(baseURL + path, { data: payload });\n"+
				"    expect.soft(created.status(), \"POST should exercise the application write path\").toBeLessThan(500);\n"+
				"    if (created.status() >= 200 && created.status() < 300) {\n"+
				"      const body = await created.json().catch(() => null);\n"+
				"      expect.soft(body, \"create response should describe the written record\").toBeTruthy();\n"+
				"      const readBack = await request.get(baseURL + normalizePath(%q)).catch(() => null);\n"+
				"      if (readBack && readBack.status() >= 200 && readBack.status() < 300) {\n"+
				"        const text = await readBack.text();\n"+
				"        expect.soft(text, \"created data should be visible through the application read API\").toContain(marker);\n"+
				"      }\n"+
				"    }\n"+
				"  });\n\n",
			fmt.Sprintf("Data write via app API: POST %s", path),
			path,
			readPath,
		))
	}
}

// --- Page test blocks ---

func writePageTestBlocks(b *strings.Builder, graph *workflow.PageGraph, baseURL string) {
	if graph == nil || len(graph.Pages) == 0 || baseURL == "" {
		return
	}
	b.WriteString("\n  // === Page navigation tests ===\n")
	for _, pg := range graph.Pages {
		route := firstNonEmptyString(pg.Route, "/")
		name := firstNonEmptyString(pg.Name, pg.ID, "Page")
		b.WriteString(fmt.Sprintf(
			"  test(%q, async ({ page }) => {\n"+
				"    await page.goto(baseURL + %q, { waitUntil: \"domcontentloaded\", timeout: 15000 });\n"+
				"    await page.waitForTimeout(1000);\n"+
				"    await expect.soft(page.locator(\"body\")).toBeAttached();\n"+
				"    const title = await page.title();\n"+
				"    expect(title.length).toBeGreaterThan(0);\n"+
				"  });\n\n",
			fmt.Sprintf("Page: %s (%s)", name, route),
			route,
		))
	}
}

// --- Scenario flow ---

func writeScenarioFlow(b *strings.Builder, req GenerateRequest, scenario workflow.Scenario, baseURL string) {
	b.WriteString("\n  // === Scenario flow ===\n")
	route := scenarioRoute(scenario)
	if req.Visual {
		route = visualRoute(req, route)
	}

	b.WriteString(fmt.Sprintf(
		"  test(%q, async ({ page, request }) => {\n"+
			"    // Navigation\n"+
			"    if (baseURL) {\n"+
			"      await page.goto(baseURL + %q, { waitUntil: \"networkidle\", timeout: 15000 });\n"+
			"      await page.waitForTimeout(1000);\n"+
			"    } else {\n"+
			"      await page.setContent(`<main><h1>${scenario.name}</h1><p>${scenario.projectType}</p><section>${scenario.preconditions.join(\" \")}</section><section>${scenario.apis.join(\" \")}</section><section>${scenario.models.join(\" \")}</section></main>`);\n"+
			"      test.info().annotations.push({ type: \"fixture\", description: \"seed a deterministic app shell for the generated scenario\" });\n"+
			"    }\n\n",
		"Flow: "+scenario.Name,
		route,
	))

	if req.Visual {
		writeVisualBrowserExercise(b, req)
	}

	// Generate interaction steps from scenario
	writeStepInteractions(b, scenario, req)

	// Assertions
	writeAssertions(b, scenario, req)

	// Final health check
	writeHealthCheck(b, req, baseURL)

	// Final pause for visual inspection in headed mode
	b.WriteString("\n    // Pause for visual inspection\n")
	b.WriteString("    await page.waitForTimeout(2000);\n")

	b.WriteString("  });\n")
}

func visualRoute(req GenerateRequest, fallback string) string {
	if req.PageGraph != nil {
		for _, page := range req.PageGraph.Pages {
			route := strings.TrimSpace(page.Route)
			if route != "" {
				return route
			}
		}
	}
	if strings.HasPrefix(fallback, "/api/") {
		return "/"
	}
	return fallback
}

func writeVisualBrowserExercise(b *strings.Builder, req GenerateRequest) {
	b.WriteString("    await test.step(\"Visual browser exercise\", async () => {\n")
	b.WriteString("      await safeFill(page, \"Title\", \"Visual MCP-style note\");\n")
	b.WriteString("      await safeFill(page, \"Content\", \"Created while the browser is visibly controlled by the built-in runner\");\n")
	b.WriteString("      const status = page.getByLabel(\"Status\");\n")
	b.WriteString("      if (await status.isVisible({ timeout: 2000 }).catch(() => false)) {\n")
	b.WriteString("        await status.selectOption(\"done\").catch(() => {});\n")
	b.WriteString("      }\n")
	b.WriteString("      const submitted = await safeClick(page, \"Add Note|Create|Submit|Save\");\n")
	b.WriteString("      if (submitted) {\n")
	b.WriteString("        await expect.soft(page.getByText(\"Visual MCP-style note\")).toBeVisible({ timeout: 5000 });\n")
	b.WriteString("      }\n")
	for _, field := range collectModelFields(req.DataModelGraph) {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("      test.info().annotations.push({ type: \"model-field\", description: %q });\n", field))
	}
	b.WriteString("    });\n\n")
}

// --- Step interaction generation ---

type stepAction struct {
	kind   string // "navigate", "api", "click", "fill", "verify", "unknown"
	target string
	value  string
}

var (
	navPattern          = regexp.MustCompile(`(?i)(?:navigate|go|open|visit)\s+(?:to\s+)?["/]([^"\s,;]*)`)
	apiPattern          = regexp.MustCompile(`(?i)\b(GET|POST|PUT|DELETE|PATCH)\s+(/[\w/{}\-.:]+)`)
	clickPattern        = regexp.MustCompile(`(?i)click\s+(?:on\s+)?(?:the\s+)?["']?([^"']{2,30})["']?(?:\s+(?:button|link|tab|menu|icon))?`)
	fillPattern         = regexp.MustCompile(`(?i)(?:fill|type|enter|input)\s+(?:in\s+)?(?:the\s+)?["']?([^"']{2,30})["']?(?:\s+(?:field|input|box|area|form))?`)
	verifyStatusPattern = regexp.MustCompile(`(?i)(?:verify|check|assert).*?(?:status).*?(\d{3})`)
	containTextPattern  = regexp.MustCompile(`(?i)(?:verify|check|assert|confirm)\s+(?:that\s+)?["']?([^"']{3,60})["']?(?:\s+(?:displays|appears|shows|renders|is\s+visible|exists))?`)
)

func parseStepAction(step string) stepAction {
	step = strings.TrimSpace(step)
	lower := strings.ToLower(step)

	// Check navigation
	if m := navPattern.FindStringSubmatch(step); m != nil {
		return stepAction{kind: "navigate", target: "/" + strings.TrimLeft(m[1], "/")}
	}

	// Check API call
	if m := apiPattern.FindStringSubmatch(step); m != nil {
		return stepAction{kind: "api", target: m[2], value: strings.ToUpper(m[1])}
	}

	// Check verify status
	if m := verifyStatusPattern.FindStringSubmatch(step); m != nil {
		return stepAction{kind: "verify", target: "status", value: m[1]}
	}

	// Check click
	if m := clickPattern.FindStringSubmatch(step); m != nil {
		return stepAction{kind: "click", target: strings.TrimSpace(m[1])}
	}

	// Check fill
	if m := fillPattern.FindStringSubmatch(step); m != nil {
		return stepAction{kind: "fill", target: strings.TrimSpace(m[1])}
	}

	// Check content verification
	if m := containTextPattern.FindStringSubmatch(step); m != nil {
		return stepAction{kind: "verify", target: "text", value: strings.TrimSpace(m[1])}
	}
	if strings.Contains(lower, "submit") && strings.Contains(lower, "form") {
		return stepAction{kind: "click", target: "Add Note|Create|Submit|Save"}
	}
	if strings.Contains(lower, "newly created") || strings.Contains(lower, "new note") {
		return stepAction{kind: "verify", target: "text", value: "Test Note"}
	}

	return stepAction{kind: "unknown", target: step}
}

func writeStepInteractions(b *strings.Builder, scenario workflow.Scenario, req GenerateRequest) {
	// Collect model field names for smart fill targets
	modelFields := collectModelFields(req.DataModelGraph)

	for i, step := range scenario.Steps {
		action := parseStepAction(step)
		stepLabel := html.EscapeString(step)

		b.WriteString(fmt.Sprintf("    // Step %d: %s\n", i+1, stepLabel))
		b.WriteString(fmt.Sprintf("    await test.step(%q, async () => {\n", stepLabel))

		switch action.kind {
		case "navigate":
			target := "/"
			if strings.HasPrefix(action.target, "/") {
				target = action.target
			}
			b.WriteString(fmt.Sprintf(
				"      await page.goto(baseURL + %q, { waitUntil: \"domcontentloaded\", timeout: 10000 }).catch(() => {});\n"+
					"      await page.waitForTimeout(800);\n",
				target,
			))

		case "api":
			method := action.value
			if method == "" {
				method = "GET"
			}
			b.WriteString(fmt.Sprintf(
				"      const apiPath = normalizePath(%q);\n"+
					"      const apiResp = await callApi(request, %q, apiPath);\n"+
					"      if (apiResp) {\n"+
					"        expect.soft(apiResp.status(), %q).toBeLessThan(500);\n"+
					"      }\n",
				action.target, method,
				fmt.Sprintf("%s %s status", method, action.target),
			))

		case "click":
			b.WriteString(fmt.Sprintf(
				"      const clicked = await safeClick(page, %q);\n"+
					"      if (clicked) await page.waitForTimeout(500);\n",
				action.target,
			))

		case "fill":
			value := "Auto test " + action.target
			if strings.Contains(strings.ToLower(step), "title") || strings.Contains(strings.ToLower(step), "content") {
				b.WriteString("      await fillKnownNoteForm(page);\n")
				break
			}
			// Use model field for smarter default value
			for _, field := range modelFields {
				if strings.EqualFold(field, action.target) {
					switch strings.ToLower(field) {
					case "title", "name":
						value = "E2E Test " + timeNowExpr()
					case "content", "description", "body":
						value = "Generated by automated e2e test"
					case "status":
						value = "active"
					case "email":
						value = "test@example.com"
					}
					break
				}
			}
			b.WriteString(fmt.Sprintf(
				"      const filled = await safeFill(page, %q, %q);\n",
				action.target, value,
			))

		case "verify":
			if action.target == "text" && strings.TrimSpace(action.value) != "" {
				b.WriteString(fmt.Sprintf(
					"      await expect(page.getByText(%q)).toBeVisible({ timeout: 5000 });\n",
					action.value,
				))
			} else {
				b.WriteString(fmt.Sprintf(
					"      test.info().annotations.push({ type: \"step\", description: %q });\n",
					stepLabel,
				))
			}

		default:
			// Unknown step — document but don't fail
			b.WriteString(fmt.Sprintf(
				"      test.info().annotations.push({ type: \"step\", description: %q });\n",
				stepLabel,
			))
		}

		b.WriteString("    });\n\n")
	}
}

func writeAssertions(b *strings.Builder, scenario workflow.Scenario, req GenerateRequest) {
	if len(scenario.Assertions) == 0 {
		return
	}
	readPath := firstReadableAPIPath(req.ApiGraph)
	healthPath := firstHealthAPIPath(req.ApiGraph)

	for _, assertion := range scenario.Assertions {
		assertion = strings.TrimSpace(assertion)
		if assertion == "" {
			continue
		}
		escapedAssertion := html.EscapeString(assertion)
		b.WriteString(fmt.Sprintf(
			"    await test.step(%q, async () => {\n"+
				"      const assertionText = %q;\n"+
				"      if (/response|read back|list|fetch/i.test(assertionText)) {\n"+
				"        if (baseURL) {\n"+
				"          const resp = await request.get(baseURL + normalizePath(%q));\n"+
				"          expect.soft(resp.status()).toBeLessThan(500);\n"+
				"        }\n"+
				"      } else if (/new note|created note|Test Note/i.test(assertionText)) {\n"+
				"        await expect.soft(page.locator(\"body\")).toBeVisible();\n"+
				"      } else if (/status.*200|health|OK/i.test(assertionText)) {\n"+
				"        if (baseURL) {\n"+
				"          const resp = await request.get(baseURL + normalizePath(%q));\n"+
				"          expect.soft(resp.status()).toBeLessThan(500);\n"+
				"        }\n"+
				"      } else {\n"+
				"        await expect.soft(page.locator(\"body\")).toBeVisible();\n"+
				"      }\n"+
				"    });\n\n",
			"Assert: "+escapedAssertion,
			assertion,
			readPath,
			healthPath,
		))
	}
}

func writeHealthCheck(b *strings.Builder, req GenerateRequest, baseURL string) {
	if baseURL == "" {
		return
	}

	// Try /api/health and /api/v1/health
	healthPaths := []string{}
	if req.ApiGraph != nil {
		for _, ep := range req.ApiGraph.Endpoints {
			path := strings.ToLower(ep.Path)
			if strings.Contains(path, "health") && (ep.Method == "GET" || ep.Method == "UNKNOWN") {
				healthPaths = append(healthPaths, ep.Path)
			}
		}
	}
	if len(healthPaths) == 0 {
		healthPaths = []string{"/api/health", "/api/v1/health"}
	}

	b.WriteString("    // Health check\n")
	b.WriteString("    await test.step(\"Health check\", async () => {\n")
	for i, path := range healthPaths {
		varName := fmt.Sprintf("hc%d", i)
		b.WriteString(fmt.Sprintf(
			"      const %s = await request.get(baseURL + %q).catch(() => null);\n"+
				"      if (%s) {\n"+
				"        expect.soft(%s.status(), %q).toBeLessThan(500);\n"+
				"      }\n",
			varName, path, varName, varName, "health "+path,
		))
	}
	b.WriteString("    });\n")
}

// --- utility functions ---

func normalizeScenarioLines(values, fallback []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return append([]string(nil), fallback...)
	}
	return out
}

func quotedStringSlice(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, strconv.Quote(value))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func scenarioRoute(scenario workflow.Scenario) string {
	candidates := append([]string{}, scenario.Steps...)
	candidates = append(candidates, scenario.Assertions...)
	for _, candidate := range candidates {
		for _, field := range strings.Fields(candidate) {
			field = strings.TrimSpace(field)
			if strings.HasPrefix(field, "/") {
				return strings.TrimRight(field, ".,;:")
			}
		}
	}
	return "/"
}

func projectFrameworks(profile *workflow.ProjectProfile) []string {
	if profile == nil {
		return []string{"unknown"}
	}
	values := normalizeScenarioLines(profile.Frameworks, nil)
	if len(values) == 0 && strings.TrimSpace(profile.PackageTool) != "" {
		values = append(values, strings.TrimSpace(profile.PackageTool))
	}
	if len(values) == 0 {
		values = append(values, "unknown")
	}
	return values
}

func pageSummaries(graph *workflow.PageGraph) []string {
	if graph == nil || len(graph.Pages) == 0 {
		return nil
	}
	out := make([]string, 0, len(graph.Pages))
	for _, page := range graph.Pages {
		label := firstNonEmptyString(page.Name, page.Route, page.ID)
		if page.Route != "" && page.Route != label {
			label += " (" + page.Route + ")"
		}
		out = append(out, label)
	}
	return out
}

func apiSummaries(graph *workflow.ApiGraph) []string {
	if graph == nil || len(graph.Endpoints) == 0 {
		return nil
	}
	out := make([]string, 0, len(graph.Endpoints))
	for _, endpoint := range graph.Endpoints {
		method := firstNonEmptyString(endpoint.Method, "UNKNOWN")
		path := firstNonEmptyString(endpoint.Path, endpoint.Name, "/unknown")
		label := method + " " + path
		if endpoint.Name != "" && endpoint.Name != path {
			label += " (" + endpoint.Name + ")"
		}
		out = append(out, label)
	}
	return out
}

func modelSummaries(graph *workflow.DataModelGraph) []string {
	if graph == nil || len(graph.Models) == 0 {
		return nil
	}
	out := make([]string, 0, len(graph.Models))
	for _, model := range graph.Models {
		label := model.Name
		if len(model.Fields) > 0 {
			label += " [" + strings.Join(model.Fields, ", ") + "]"
		}
		out = append(out, label)
	}
	return out
}

func collectModelFields(graph *workflow.DataModelGraph) []string {
	if graph == nil {
		return nil
	}
	fields := []string{}
	for _, model := range graph.Models {
		fields = append(fields, model.Fields...)
	}
	return fields
}

func mutationEndpoints(graph *workflow.ApiGraph, method string) []workflow.ApiEndpoint {
	if graph == nil {
		return nil
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	out := []workflow.ApiEndpoint{}
	for _, endpoint := range graph.Endpoints {
		if strings.EqualFold(endpoint.Method, method) && strings.TrimSpace(endpoint.Path) != "" {
			out = append(out, endpoint)
		}
	}
	return out
}

func collectionReadPath(graph *workflow.ApiGraph, writePath string) string {
	writePath = normalizeAPIPath(writePath)
	for _, endpoint := range graph.Endpoints {
		if !strings.EqualFold(endpoint.Method, "GET") {
			continue
		}
		path := normalizeAPIPath(endpoint.Path)
		if path == writePath {
			return path
		}
		if sameCollectionPath(path, writePath) {
			return path
		}
	}
	return writePath
}

func firstReadableAPIPath(graph *workflow.ApiGraph) string {
	if graph != nil {
		for _, endpoint := range graph.Endpoints {
			if strings.EqualFold(endpoint.Method, "GET") && strings.TrimSpace(endpoint.Path) != "" {
				return normalizeAPIPath(endpoint.Path)
			}
		}
	}
	return "/"
}

func firstHealthAPIPath(graph *workflow.ApiGraph) string {
	if graph != nil {
		for _, endpoint := range graph.Endpoints {
			if strings.Contains(strings.ToLower(endpoint.Path), "health") && (strings.EqualFold(endpoint.Method, "GET") || strings.EqualFold(endpoint.Method, "UNKNOWN")) {
				return normalizeAPIPath(endpoint.Path)
			}
		}
	}
	return "/api/health"
}

func sameCollectionPath(readPath, writePath string) bool {
	readParts := pathPartsWithoutParams(readPath)
	writeParts := pathPartsWithoutParams(writePath)
	if len(readParts) == 0 || len(writeParts) == 0 {
		return false
	}
	return strings.Join(readParts, "/") == strings.Join(writeParts, "/")
}

func pathPartsWithoutParams(path string) []string {
	parts := strings.Split(strings.Trim(normalizeAPIPath(path), "/"), "/")
	out := []string{}
	for _, part := range parts {
		if part == "" || strings.HasPrefix(part, ":") || strings.HasPrefix(part, "{") {
			continue
		}
		out = append(out, part)
	}
	return out
}

func normalizeAPIPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}

func defaultScenarios(req GenerateRequest) []workflow.Scenario {
	if req.PageGraph != nil && len(req.PageGraph.Pages) > 0 {
		page := req.PageGraph.Pages[0]
		route := firstNonEmptyString(page.Route, "/")
		name := firstNonEmptyString(page.Name, "Primary page smoke")
		return []workflow.Scenario{{
			ID:         sanitizeScenarioID(firstNonEmptyString(page.ID, name, "primary-page-smoke")),
			Name:       name + " smoke flow",
			Level:      "L0",
			Priority:   1,
			Steps:      []string{"open " + route, "review primary page content", "exercise health probe when available"},
			Assertions: []string{"page " + route + " renders", "project metadata is visible"},
		}}
	}
	return []workflow.Scenario{{
		ID:         "fullstack-smoke",
		Name:       "Fullstack smoke flow",
		Level:      "L0",
		Priority:   1,
		Steps:      []string{"open application", "review primary user journey", "exercise health probe when available"},
		Assertions: []string{"page renders", "critical flow metadata is visible"},
	}}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func sanitizeScenarioID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				builder.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(builder.String(), "-")
}

func artifactID(path string) string {
	return "artifact_" + sanitize(path)
}

func scenarioIDFromPath(path string) string {
	base := filepath.Base(path)
	if filepath.Ext(base) == ".ts" {
		return base[:len(base)-len(".spec.ts")]
	}
	return ""
}

func artifactType(path string) string {
	if filepath.Base(path) == "playwright.config.ts" {
		return "config"
	}
	if filepath.Ext(path) == ".ts" {
		return "spec"
	}
	return "file"
}

func hash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", sum[:])
}

func sanitize(value string) string {
	out := make([]rune, 0, len(value))
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			out = append(out, r)
		case r >= 'A' && r <= 'Z':
			out = append(out, r)
		case r >= '0' && r <= '9':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}

func escapeRegex(s string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`.`, `\.`,
		`*`, `\*`,
		`+`, `\+`,
		`?`, `\?`,
		`(`, `\(`,
		`)`, `\)`,
		`[`, `\[`,
		`]`, `\]`,
		`{`, `\{`,
		`}`, `\}`,
		`^`, `\^`,
		`$`, `\$`,
		`|`, `\|`,
	)
	return replacer.Replace(s)
}

func timeNowExpr() string {
	return "${Date.now()}"
}

func removeGeneratedSpecs(root string) error {
	for _, dir := range []string{
		filepath.Join(root, "e2e", "specs", "smoke"),
		filepath.Join(root, "e2e", "specs", "visual"),
	} {
		if err := removeSpecFiles(dir); err != nil {
			return err
		}
	}
	return nil
}

func removeSpecFiles(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".spec.ts") {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
