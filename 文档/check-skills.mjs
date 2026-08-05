import fs from "node:fs";
import path from "node:path";

const root = process.cwd();
const skillsRoot = path.join(root, ".agents", "skills");
const errors = [];
const allowedTopLevel = new Set(["SKILL.md", "agents", "assets", "references", "scripts"]);
const forbiddenReferenceNames = /\b(?:XRain|AiInfra|LLM_backend|cnet_v2|Aigateway)\b/i;

function relative(file) {
  return path.relative(root, file).split(path.sep).join("/");
}

function walk(directory) {
  if (!fs.existsSync(directory)) return [];
  return fs.readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const full = path.join(directory, entry.name);
    return entry.isDirectory() ? walk(full) : [full];
  });
}

function unquote(value) {
  const trimmed = value.trim();
  if ((trimmed.startsWith('"') && trimmed.endsWith('"')) || (trimmed.startsWith("'") && trimmed.endsWith("'"))) {
    return trimmed.slice(1, -1);
  }
  return trimmed;
}

function parseFrontmatter(file, text) {
  const match = text.match(/^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/);
  if (!match) {
    errors.push(`${relative(file)}: missing YAML frontmatter`);
    return {};
  }
  const result = {};
  for (const line of match[1].split(/\r?\n/).filter((item) => item.trim())) {
    const field = line.match(/^([a-z]+):\s*(.+)$/);
    if (!field) {
      errors.push(`${relative(file)}: unsupported frontmatter line ${line}`);
      continue;
    }
    if (!new Set(["name", "description"]).has(field[1])) {
      errors.push(`${relative(file)}: frontmatter may contain only name and description`);
      continue;
    }
    if (Object.hasOwn(result, field[1])) errors.push(`${relative(file)}: duplicate frontmatter field ${field[1]}`);
    result[field[1]] = unquote(field[2]);
  }
  return result;
}

function quotedYamlValue(text, key) {
  return text.match(new RegExp(`^\\s*${key}:\\s*["']([^"']+)["']\\s*$`, "m"))?.[1];
}

function validateLocalLinks(file, text) {
  for (const match of text.matchAll(/\[[^\]]*\]\(([^)]+)\)/g)) {
    const raw = match[1].split("#", 1)[0].trim();
    if (!raw || /^(?:https?:|mailto:|data:)/i.test(raw)) continue;
    const target = path.resolve(path.dirname(file), decodeURIComponent(raw));
    if (target !== skillsRoot && !target.startsWith(`${skillsRoot}${path.sep}`)) {
      errors.push(`${relative(file)}: local link escapes .agents/skills (${raw})`);
    } else if (!fs.existsSync(target)) {
      errors.push(`${relative(file)}: broken local link ${raw}`);
    }
  }

  for (const match of text.matchAll(/`((?:\.\.\/[^`\s]+\/SKILL\.md|(?:agents|references|scripts)\/[^`\s]+))`/g)) {
    const target = path.resolve(path.dirname(file), ...match[1].split("/"));
    if (!fs.existsSync(target)) errors.push(`${relative(file)}: broken bundled resource ${match[1]}`);
  }
}

if (!fs.existsSync(skillsRoot)) {
  errors.push(".agents/skills: missing workspace skill root");
} else {
  const skillDirectories = fs.readdirSync(skillsRoot, { withFileTypes: true })
    .filter((entry) => entry.isDirectory())
    .sort((left, right) => left.name.localeCompare(right.name));
  if (skillDirectories.length === 0) errors.push(".agents/skills: no skills found");

  for (const entry of skillDirectories) {
    const skillRoot = path.join(skillsRoot, entry.name);
    const skillPath = path.join(skillRoot, "SKILL.md");
    if (!/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(entry.name) || entry.name.length > 64) {
      errors.push(`${relative(skillRoot)}: invalid skill directory name`);
    }
    for (const child of fs.readdirSync(skillRoot, { withFileTypes: true })) {
      if (!allowedTopLevel.has(child.name)) errors.push(`${relative(path.join(skillRoot, child.name))}: unsupported skill entry`);
    }
    for (const directory of fs.readdirSync(skillRoot, { withFileTypes: true }).filter((child) => child.isDirectory())) {
      if (walk(path.join(skillRoot, directory.name)).length === 0) errors.push(`${relative(path.join(skillRoot, directory.name))}: empty resource directory`);
    }
    for (const file of walk(skillRoot)) {
      if (/^readme\.md$/i.test(path.basename(file))) errors.push(`${relative(file)}: README is not allowed inside a skill`);
      const text = fs.readFileSync(file, "utf8");
      if (text.includes("\uFFFD")) errors.push(`${relative(file)}: contains Unicode replacement character`);
      if (forbiddenReferenceNames.test(text)) errors.push(`${relative(file)}: contains a reference-project name`);
      if (/\.md$/i.test(file)) validateLocalLinks(file, text);
    }
    if (!fs.existsSync(skillPath)) {
      errors.push(`${relative(skillPath)}: missing required SKILL.md`);
      continue;
    }

    const skillText = fs.readFileSync(skillPath, "utf8");
    const frontmatter = parseFrontmatter(skillPath, skillText);
    if (frontmatter.name !== entry.name) errors.push(`${relative(skillPath)}: name must match its directory`);
    if (!frontmatter.description || frontmatter.description.length < 24) errors.push(`${relative(skillPath)}: description is missing or too short`);
    if (!/^#\s+\S+/m.test(skillText.replace(/^---[\s\S]*?---\r?\n/, ""))) errors.push(`${relative(skillPath)}: body must contain an H1`);
    if (skillText.split(/\r?\n/).length > 500) errors.push(`${relative(skillPath)}: exceeds 500-line skill budget`);

    const metadataPath = path.join(skillRoot, "agents", "openai.yaml");
    if (!fs.existsSync(metadataPath)) {
      errors.push(`${relative(metadataPath)}: missing Codex UI metadata`);
    } else {
      const metadataText = fs.readFileSync(metadataPath, "utf8");
      for (const key of ["display_name", "short_description", "default_prompt"]) {
        if (!quotedYamlValue(metadataText, key)) errors.push(`${relative(metadataPath)}: missing quoted ${key}`);
      }
      const shortDescription = quotedYamlValue(metadataText, "short_description") ?? "";
      if (shortDescription.length < 25 || shortDescription.length > 64) errors.push(`${relative(metadataPath)}: short_description must be 25-64 characters`);
      const defaultPrompt = quotedYamlValue(metadataText, "default_prompt") ?? "";
      if (!defaultPrompt.includes(`$${entry.name}`)) errors.push(`${relative(metadataPath)}: default_prompt must mention $${entry.name}`);
      if (!/^\s*allow_implicit_invocation:\s*(?:true|false)\s*$/m.test(metadataText)) errors.push(`${relative(metadataPath)}: missing invocation policy`);
    }
  }
}

if (errors.length) {
  console.error(`Skill check failed (${errors.length}):`);
  for (const error of errors) console.error(`- ${error}`);
  process.exit(1);
}

const count = fs.readdirSync(skillsRoot, { withFileTypes: true }).filter((entry) => entry.isDirectory()).length;
console.log(`Skill check passed: ${count} workspace skills.`);
