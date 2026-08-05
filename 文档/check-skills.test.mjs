import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";

const workspaceRoot = process.cwd();

function runCheck(cwd) {
  return spawnSync(process.execPath, ["文档/check-skills.mjs"], { cwd, encoding: "utf8", windowsHide: true });
}

function createFixture(t) {
  const fixture = fs.mkdtempSync(path.join(os.tmpdir(), "atlaspay-skills-"));
  t.after(() => fs.rmSync(fixture, { recursive: true, force: true }));
  fs.cpSync(workspaceRoot, fixture, {
    recursive: true,
    filter: (source) => ![".git", "node_modules"].includes(path.basename(source)),
  });
  return fixture;
}

test("accepts the current workspace skills", () => {
  const result = runCheck(workspaceRoot);
  assert.equal(result.status, 0, `${result.stdout}\n${result.stderr}`);
  assert.match(result.stdout, /Skill check passed: 9 workspace skills/);
});

test("rejects a SKILL name that differs from its directory", (t) => {
  const fixture = createFixture(t);
  const skillPath = path.join(fixture, ".agents", "skills", "document-governance", "SKILL.md");
  const text = fs.readFileSync(skillPath, "utf8").replace("name: document-governance", "name: wrong-name");
  fs.writeFileSync(skillPath, text, "utf8");
  const result = runCheck(fixture);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /name must match its directory/);
});

test("rejects a broken bundled reference", (t) => {
  const fixture = createFixture(t);
  const skillPath = path.join(fixture, ".agents", "skills", "technical-solution", "SKILL.md");
  fs.appendFileSync(skillPath, "\n[Missing](references/missing.md)\n", "utf8");
  const result = runCheck(fixture);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /broken local link/);
});

test("rejects stale Codex UI invocation metadata", (t) => {
  const fixture = createFixture(t);
  const metadataPath = path.join(fixture, ".agents", "skills", "document-governance", "agents", "openai.yaml");
  const text = fs.readFileSync(metadataPath, "utf8").replace("$document-governance", "$wrong-skill");
  fs.writeFileSync(metadataPath, text, "utf8");
  const result = runCheck(fixture);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /default_prompt must mention/);
});
