#!/usr/bin/env node
"use strict";

const { resolveBinaryPath } = require("../lib/paths.js");
const { spawnSync } = require("child_process");

const bin = resolveBinaryPath();
if (!bin) {
  console.error(
    "nomad binary is not installed. Reinstall the package or run install.js."
  );
  process.exit(1);
}

const result = spawnSync(bin, process.argv.slice(2), { stdio: "inherit" });
if (result.error) {
  console.error(result.error.message);
  process.exit(1);
}
process.exit(result.status === null ? 0 : result.status);
