import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";

const workspaceRoot = process.cwd();

function runCheck(cwd) {
  return spawnSync(process.execPath, ["文档/check-docs.mjs"], {
    cwd,
    encoding: "utf8",
  });
}

function createFixture(t) {
  const fixture = fs.mkdtempSync(path.join(os.tmpdir(), "atlaspay-docs-"));
  t.after(() => fs.rmSync(fixture, { recursive: true, force: true }));
  fs.cpSync(workspaceRoot, fixture, {
    recursive: true,
    filter: (source) => path.basename(source) !== ".git" && path.basename(source) !== "node_modules",
  });
  return fixture;
}

test("accepts the current control plane", () => {
  const result = runCheck(workspaceRoot);
  assert.equal(result.status, 0, `${result.stdout}\n${result.stderr}`);
  assert.match(result.stdout, /Documentation check passed/);
});

test("rejects a formal document outside declared locations", (t) => {
  const fixture = createFixture(t);
  const unmanagedDirectory = path.join(fixture, "文档", "未声明");
  fs.mkdirSync(unmanagedDirectory, { recursive: true });
  fs.writeFileSync(path.join(unmanagedDirectory, "INVALID.md"), "# Invalid\n", "utf8");

  const result = runCheck(fixture);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /outside every governed location|undeclared documentation root/);
});

test("rejects an invalid task status", (t) => {
  const fixture = createFixture(t);
  const taskPath = path.join(fixture, "文档", "TASK_CONTROL.md");
  const taskText = fs.readFileSync(taskPath, "utf8");
  assert.match(taskText, /^## 当前队列$/m);
  const invalidTask = "| TEST-INVALID-STATUS | Done | workspace | validation fixture | `AGENTS.md` |";
  fs.writeFileSync(taskPath, taskText.replace(/(## 当前队列\r?\n)/, `$1\n${invalidTask}\n`), "utf8");

  const result = runCheck(fixture);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /invalid task status/);
});
