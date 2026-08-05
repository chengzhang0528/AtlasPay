import fs from "node:fs";
import path from "node:path";

const root = process.cwd();
const docsRoot = path.join(root, "文档");
const humanDocsRoot = path.join(root, "人类-文档");
const rootAgent = path.join(root, "AGENTS.md");
const rootReadme = path.join(root, "README.md");
const packagePath = path.join(root, "package.json");
const structurePath = path.join(docsRoot, "WORKSPACE_STRUCTURE.md");
const ignoredDirectories = new Set([
  ".git",
  ".reports",
  ".agents",
  ".codex-build",
  ".venv",
  "bin",
  "dist",
  "logs",
  "node_modules",
  "obj",
  "target",
]);

function walk(directory, predicate = () => true) {
  if (!fs.existsSync(directory)) return [];
  return fs.readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    if (entry.isDirectory() && ignoredDirectories.has(entry.name)) return [];
    const full = path.join(directory, entry.name);
    if (entry.isDirectory()) return walk(full, predicate);
    return predicate(full) ? [full] : [];
  });
}

function relative(file, base = root) {
  return path.relative(base, file).split(path.sep).join("/");
}

function resolveReference(source, raw) {
  let value = raw.trim().replace(/^`|`$/g, "");
  const markdownLink = value.match(/^\[[^\]]*\]\(([^)]+)\)$/);
  if (markdownLink) value = markdownLink[1];
  value = value.split("#", 1)[0].trim();
  if (!value || /^(?:https?:|mailto:|data:)/i.test(value)) return null;
  if (value.startsWith("<WORKSPACE_ROOT>/")) value = value.slice(17);
  else value = value.replace(/^<|>$/g, "");
  try {
    value = decodeURIComponent(value);
  } catch {
    // Preserve malformed text so the broken reference remains observable.
  }
  value = value.replaceAll("\\", "/");
  if (value.startsWith("/")) value = value.slice(1);
  if (value === "AGENTS.md" || value === "README.md" || value === "package.json") {
    return path.join(root, value);
  }
  if (value.startsWith("文档/") || value.startsWith("人类-文档/")) {
    return path.resolve(root, ...value.split("/"));
  }
  if (/^(?:项目|工作空间|工作流)\//.test(value)) {
    return path.resolve(docsRoot, ...value.split("/"));
  }
  if (value === "TASK_CONTROL.md" || value === "WORKSPACE_STRUCTURE.md") {
    return path.join(docsRoot, value);
  }
  return path.resolve(path.dirname(source), value);
}

function metadata(text, key) {
  const head = text.split(/\r?\n/).slice(0, 24).join("\n");
  return head.match(new RegExp(`^${key}:\\s*(\\S+)\\s*$`, "m"))?.[1];
}

const errors = [];
const files = walk(root, (file) => /\.mdx?$/i.test(file));
const texts = new Map(files.map((file) => [file, fs.readFileSync(file, "utf8")]));
const formal = files.filter((file) => file.startsWith(`${docsRoot}${path.sep}`));

if (!fs.existsSync(rootAgent)) errors.push("AGENTS.md: missing root agent entry");
if (!fs.existsSync(structurePath)) errors.push("文档/WORKSPACE_STRUCTURE.md: missing structure contract");
if (!fs.existsSync(packagePath)) errors.push("package.json: missing workspace command entry");
if (errors.length) {
  console.error(`Documentation check failed (${errors.length}):`);
  for (const error of errors) console.error(`- ${error}`);
  process.exit(1);
}

let packageJson = {};
try {
  packageJson = JSON.parse(fs.readFileSync(packagePath, "utf8"));
} catch (error) {
  errors.push(`package.json: invalid JSON (${error.message})`);
}
if (packageJson.scripts?.["check:docs"] !== "node 文档/check-docs.mjs") {
  errors.push("package.json: check:docs must be the canonical documentation command");
}

const humanSurfaceExists = fs.existsSync(rootReadme) || fs.existsSync(humanDocsRoot);
if (humanSurfaceExists) {
  const humanEntry = path.join(humanDocsRoot, "README.md");
  if (!fs.existsSync(rootReadme)) errors.push("README.md: missing root human entry");
  if (!fs.existsSync(humanEntry)) errors.push("人类-文档/README.md: missing complete human entry");
  const packageScripts = new Set(Object.keys(packageJson.scripts ?? {}));
  const humanFiles = [
    ...(fs.existsSync(rootReadme) ? [rootReadme] : []),
    ...files.filter((file) => file.startsWith(`${humanDocsRoot}${path.sep}`)),
  ];
  for (const file of humanFiles) {
    const label = relative(file);
    const text = texts.get(file) ?? fs.readFileSync(file, "utf8");
    for (const match of text.matchAll(/\[[^\]]*\]\(([^)]+)\)/g)) {
      const target = resolveReference(file, match[1]);
      if (target && !fs.existsSync(target)) errors.push(`${label}: broken human link ${match[1]}`);
    }
    for (const match of text.matchAll(/\bnpm run ([A-Za-z0-9:_-]+)/g)) {
      if (!packageScripts.has(match[1])) errors.push(`${label}: unknown package script ${match[1]}`);
    }
    if (file.startsWith(`${humanDocsRoot}${path.sep}`) && path.basename(file) !== "README.md") {
      const stem = path.basename(file, path.extname(file));
      const h1 = text.match(/^#\s+(.+?)\s*$/m)?.[1];
      if (!h1 || h1 !== stem) errors.push(`${label}: filename must match its H1 title`);
    }
  }
}

const structureText = fs.readFileSync(structurePath, "utf8");
const placements = [...structureText.matchAll(/^- `([^`]+)` -> `([^`]+)`\s*$/gm)].map((match) => {
  const declaredPath = match[1].replaceAll("\\", "/");
  const isDirectory = declaredPath.endsWith("/");
  return {
    declaredPath,
    isDirectory,
    target: path.resolve(docsRoot, ...declaredPath.replace(/\/$/, "").split("/")),
    kinds: match[2].split(",").map((kind) => kind.trim()).filter(Boolean),
  };
});
const statusRules = new Map([...structureText.matchAll(/^- `([^`]+)`: `([^`]+)`\s*$/gm)].map((match) => [
  match[1].trim(),
  match[2].split(",").map((status) => status.trim()).filter(Boolean),
]));
if (placements.length === 0) errors.push("文档/WORKSPACE_STRUCTURE.md: no governed locations declared");
if (statusRules.size === 0) errors.push("文档/WORKSPACE_STRUCTURE.md: no status rules declared");

for (const placement of placements) {
  if (placement.target !== docsRoot && !placement.target.startsWith(`${docsRoot}${path.sep}`)) {
    errors.push(`文档/WORKSPACE_STRUCTURE.md: governed path escapes 文档/ (${placement.declaredPath})`);
  } else if (!fs.existsSync(placement.target)) {
    errors.push(`文档/WORKSPACE_STRUCTURE.md: declared path does not exist (${placement.declaredPath})`);
  }
  for (const kind of placement.kinds) {
    if (!statusRules.has(kind)) errors.push(`文档/WORKSPACE_STRUCTURE.md: Kind ${kind} has no status lifecycle`);
  }
}

const expectedRootDirectories = new Set(placements
  .map((placement) => placement.declaredPath.split("/")[0])
  .filter((entry) => entry && !/\.mdx?$/i.test(entry)));
const actualRootDirectories = new Set(fs.readdirSync(docsRoot, { withFileTypes: true })
  .filter((entry) => entry.isDirectory())
  .map((entry) => entry.name));
for (const directory of actualRootDirectories) {
  if (!expectedRootDirectories.has(directory)) errors.push(`文档/${directory}: undeclared documentation root`);
}
for (const directory of expectedRootDirectories) {
  if (!actualRootDirectories.has(directory)) errors.push(`文档/WORKSPACE_STRUCTURE.md: missing directory ${directory}`);
}

function placementFor(file) {
  return placements.filter((placement) => placement.isDirectory
    ? file.startsWith(`${placement.target}${path.sep}`)
    : file === placement.target)
    .sort((left, right) => right.target.length - left.target.length)[0];
}

for (const file of formal) {
  const label = relative(file);
  if (path.basename(file).toLowerCase() === "readme.md") {
    errors.push(`${label}: README.md is not allowed in the governed tree`);
  }
  const placement = placementFor(file);
  if (!placement) {
    errors.push(`${label}: file is outside every governed location in WORKSPACE_STRUCTURE.md`);
    continue;
  }
  const text = texts.get(file);
  const kind = metadata(text, "Kind");
  const status = metadata(text, "Status");
  if (!kind) errors.push(`${label}: missing Kind metadata in first 24 lines`);
  else if (!placement.kinds.includes(kind)) errors.push(`${label}: Kind ${kind} is not allowed by ${placement.declaredPath}`);
  else if (status && !statusRules.get(kind)?.includes(status)) errors.push(`${label}: invalid Status ${status} for Kind ${kind}`);
}

const workflowContracts = formal.filter((file) => metadata(texts.get(file), "Kind") === "WorkflowContract");
if (workflowContracts.length !== 1) errors.push(`文档/工作流/: expected one WorkflowContract, found ${workflowContracts.length}`);
const workflowContractText = workflowContracts.length === 1 ? texts.get(workflowContracts[0]) : "";
const declaredActionList = [...workflowContractText.matchAll(/^\| `(ACT-[A-Z0-9-]+)` \|/gm)].map((match) => match[1]);
const declaredActions = new Set(declaredActionList);
if (declaredActions.size === 0) errors.push("文档/工作流/WORKFLOW_CONTRACT.md: no Action declarations");
if (declaredActions.size !== declaredActionList.length) errors.push("文档/工作流/WORKFLOW_CONTRACT.md: duplicate Action ID");

const workflows = new Map();
for (const file of formal.filter((candidate) => metadata(texts.get(candidate), "Kind") === "Workflow")) {
  const label = relative(file);
  const text = texts.get(file);
  const workflowId = metadata(text, "Workflow ID");
  if (!workflowId || !/^WF-\d{4}$/.test(workflowId)) errors.push(`${label}: missing or invalid Workflow ID`);
  else if (workflows.has(workflowId)) errors.push(`${label}: duplicate Workflow ID ${workflowId}`);
  else workflows.set(workflowId, file);
  for (const section of ["Trigger", "Input", "Output", "Actions", "Key Constraints", "Stop When"]) {
    if (!new RegExp(`^## ${section}\\s*$`, "m").test(text)) errors.push(`${label}: missing section ${section}`);
  }
  const actionReferences = new Set(text.match(/\bACT-[A-Z0-9-]+\b/g) ?? []);
  if (actionReferences.size === 0) errors.push(`${label}: no Action reference`);
  for (const actionId of actionReferences) {
    if (!declaredActions.has(actionId)) errors.push(`${label}: undeclared Action ${actionId}`);
  }
  if (workflowContractText && !workflowContractText.includes(path.basename(file))) {
    errors.push(`${label}: not routed by WORKFLOW_CONTRACT.md`);
  }
}

const taskControlPath = path.join(docsRoot, "TASK_CONTROL.md");
const taskControlText = texts.get(taskControlPath) ?? "";
const allowedTaskStatuses = new Set(["InProgress", "Ready", "Review", "Blocked", "Backlog"]);
const taskIds = [];
let taskSection = "";
for (const line of taskControlText.split(/\r?\n/)) {
  if (line === "## 当前队列") taskSection = "current";
  else if (line === "## 候选积压") taskSection = "backlog";
  else if (line === "## 长期触发") taskSection = "trigger";
  else if (line.startsWith("## ")) taskSection = "";
  if (!/^\|\s*[A-Z][A-Z0-9-]+\s*\|/.test(line)) continue;
  const cells = line.split("|").slice(1, -1).map((cell) => cell.trim());
  const [taskId, status] = cells;
  if (taskId === "ID") continue;
  if (cells.length !== 5) errors.push(`文档/TASK_CONTROL.md: invalid row shape ${taskId}`);
  taskIds.push(taskId);
  if (!allowedTaskStatuses.has(status)) errors.push(`文档/TASK_CONTROL.md: invalid task status ${taskId} -> ${status}`);
  if (!taskSection) errors.push(`文档/TASK_CONTROL.md: task outside managed section ${taskId}`);
  else if (taskSection === "current" && status === "Backlog") errors.push(`文档/TASK_CONTROL.md: Backlog in current queue ${taskId}`);
  else if (["backlog", "trigger"].includes(taskSection) && status !== "Backlog") errors.push(`文档/TASK_CONTROL.md: active task outside current queue ${taskId}`);
}
for (const taskId of new Set(taskIds)) {
  if (taskIds.filter((candidate) => candidate === taskId).length > 1) errors.push(`文档/TASK_CONTROL.md: duplicate task ID ${taskId}`);
}
for (const match of taskControlText.matchAll(/`([^`\r\n]+)`/g)) {
  const reference = match[1];
  if (!reference.endsWith(".md") && !reference.startsWith("文档/") && reference !== "AGENTS.md") continue;
  const target = resolveReference(taskControlPath, reference);
  if (target && !fs.existsSync(target)) errors.push(`文档/TASK_CONTROL.md: broken task entry ${reference}`);
}

const durableKinds = new Set(["AgentEntry", "CurrentDesign", "Decision", "ProductContract", "Runbook", "StructureContract", "Workflow", "WorkflowContract"]);
const formalKinds = new Map(formal.map((file) => [path.normalize(file), metadata(texts.get(file), "Kind")]));
for (const file of formal) {
  const kind = metadata(texts.get(file), "Kind");
  if (!["ChangePlan", "Issue"].includes(kind)) continue;
  const label = relative(file);
  if (!taskControlText.includes(path.basename(file))) errors.push(`${label}: active document is not referenced by TASK_CONTROL.md`);
  const workflowId = metadata(texts.get(file), "Workflow");
  if (!workflowId || !workflows.has(workflowId)) errors.push(`${label}: missing or unknown Workflow binding`);
  else if (kind === "ChangePlan" && workflowId !== "WF-0002") errors.push(`${label}: ChangePlan must bind WF-0002`);
  else if (kind === "Issue" && workflowId !== "WF-0003") errors.push(`${label}: Issue must bind WF-0003`);

  if (kind === "ChangePlan") {
    const lines = texts.get(file).split(/\r?\n/);
    const dependsIndex = lines.findIndex((line) => line.startsWith("Depends On:"));
    let hasDurableOwner = false;
    for (let index = dependsIndex + 1; dependsIndex >= 0 && index < lines.length; index += 1) {
      const dependency = lines[index].match(/^-\s+(.+)$/)?.[1];
      if (!dependency) break;
      const target = resolveReference(file, dependency);
      if (target && durableKinds.has(formalKinds.get(path.normalize(target)))) hasDurableOwner = true;
    }
    if (!hasDurableOwner) errors.push(`${label}: ChangePlan must depend on a durable formal owner`);
  }
}

const governed = [rootAgent, ...formal];
for (const file of governed) {
  const label = relative(file);
  const text = texts.get(file) ?? fs.readFileSync(file, "utf8");
  const lines = text.split(/\r?\n/);
  const head = lines.slice(0, 24).join("\n");
  if (!lines.find((line) => line.trim())?.startsWith("# ")) errors.push(`${label}: first content line must be H1`);
  for (const key of ["Status", "Scope", "Owner", "Updated", "Depends On"]) {
    if (!new RegExp(`^${key}:`, "m").test(head)) errors.push(`${label}: missing ${key} metadata`);
  }
  if (file !== rootAgent && !/^Kind:\s*\S+/m.test(head)) errors.push(`${label}: missing Kind metadata`);
  for (const key of ["Status", "Kind", "Workflow", "Workflow ID", "Scope", "Owner", "Updated", "Depends On"]) {
    if ((head.match(new RegExp(`^${key}:`, "gm")) ?? []).length > 1) errors.push(`${label}: duplicate ${key} metadata`);
  }
  if (/^Status:\s*(?:Done|Completed)\s*$/im.test(head)) errors.push(`${label}: completed status is forbidden`);

  const dependsIndex = lines.findIndex((line) => line.startsWith("Depends On:"));
  for (let index = dependsIndex + 1; dependsIndex >= 0 && index < lines.length; index += 1) {
    const dependency = lines[index].match(/^-\s+(.+)$/)?.[1];
    if (!dependency) break;
    if (/^(?:none|无)$/i.test(dependency.trim())) continue;
    const target = resolveReference(file, dependency);
    if (target && !fs.existsSync(target)) errors.push(`${label}: missing dependency ${dependency}`);
    const targetKind = target ? formalKinds.get(path.normalize(target)) : undefined;
    if (durableKinds.has(metadata(text, "Kind")) && ["ChangePlan", "Issue"].includes(targetKind)) {
      errors.push(`${label}: durable document must not depend on temporary ${targetKind}`);
    }
  }
  for (const match of text.matchAll(/\[[^\]]*\]\(([^)]+\.md(?:#[^)]+)?)\)/gi)) {
    const target = resolveReference(file, match[1]);
    if (target && !fs.existsSync(target)) errors.push(`${label}: broken Markdown link ${match[1]}`);
  }
  if (text.includes("\uFFFD")) errors.push(`${label}: contains Unicode replacement character`);
}

const governedSet = new Set(governed.map((file) => path.normalize(file)));
const dependencyGraph = new Map();
for (const file of governed) {
  const lines = (texts.get(file) ?? fs.readFileSync(file, "utf8")).split(/\r?\n/);
  const dependsIndex = lines.findIndex((line) => line.startsWith("Depends On:"));
  const targets = [];
  for (let index = dependsIndex + 1; dependsIndex >= 0 && index < lines.length; index += 1) {
    const dependency = lines[index].match(/^-\s+(.+)$/)?.[1];
    if (!dependency) break;
    const target = resolveReference(file, dependency);
    if (target && governedSet.has(path.normalize(target))) targets.push(path.normalize(target));
  }
  dependencyGraph.set(path.normalize(file), targets);
}
const visitState = new Map();
function visit(file, stack) {
  if (visitState.get(file) === 2) return;
  if (visitState.get(file) === 1) {
    const start = stack.indexOf(file);
    errors.push(`Depends On cycle: ${[...stack.slice(start), file].map((item) => relative(item)).join(" -> ")}`);
    return;
  }
  visitState.set(file, 1);
  for (const target of dependencyGraph.get(file) ?? []) visit(target, [...stack, file]);
  visitState.set(file, 2);
}
for (const file of dependencyGraph.keys()) visit(file, []);

const budgets = new Map([
  ["AGENTS.md", 120],
  ["文档/WORKSPACE_STRUCTURE.md", 180],
  ["文档/TASK_CONTROL.md", 120],
]);
for (const file of formal) if (!budgets.has(relative(file))) budgets.set(relative(file), 200);
for (const [file, limit] of budgets) {
  const text = texts.get(path.join(root, ...file.split("/")));
  if (text && text.split(/\r?\n/).length > limit) errors.push(`${file}: exceeds ${limit}-line hot-path budget`);
}

for (const file of files.filter((item) => {
  const label = relative(item);
  return label === "AGENTS.md" || label === "README.md" || label.startsWith("人类-文档/") || label.startsWith("文档/");
})) {
  const text = texts.get(file);
  if (/\b[A-Za-z]:[\\/](?:[^\\/\r\n`]+[\\/])*[^\\/\r\n`]+/.test(text)) errors.push(`${relative(file)}: contains a machine-specific workspace path`);
  if (/Password=(?!<|\$\{|%|\$env:|__)[^;\s"'`]+/i.test(text)) errors.push(`${relative(file)}: contains a literal password`);
  if (/(?:API_KEY|JWT_SECRET|POSTGRES_PASSWORD)=(?!<|\$\{|%|\$env:)[^\s`]+/i.test(text)) errors.push(`${relative(file)}: contains a literal secret`);
}

const lineCount = [...texts.values()].reduce((sum, text) => sum + text.split(/\r?\n/).length, 0);
if (errors.length) {
  console.error(`Documentation check failed (${errors.length}):`);
  for (const error of errors) console.error(`- ${error}`);
  process.exit(1);
}
console.log(`Documentation check passed: ${files.length} Markdown files, ${lineCount} lines.`);
