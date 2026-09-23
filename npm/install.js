"use strict";

// Postinstall: download the platform-specific prebuilt nomad binary from
// the matching GitHub Release. No third-party dependencies so it runs in
// restricted npm environments too. NOMAD_DOWNLOAD_BASE_URL overrides the
// release base (useful for mirrors/offline hosting).

const fs = require("fs");
const path = require("path");
const http = require("http");
const httpx = require("https");
const {
  platformName,
  archName,
  pkgVersion,
  installRoot,
  binaryName,
} = require("./lib/paths.js");
const { untarGz } = require("./lib/extract.js");

const DEFAULT_BASE =
  "https://github.com/chyroc/nomad/releases/download";

function log(msg) {
  if (process.env.NOMAD_INSTALL_QUIET) return;
  console.log("[nomad] " + msg);
}

function get(url, redirectsLeft) {
  const client = url.startsWith("https:") ? httpx : http;
  return new Promise((resolve, reject) => {
    client
      .get(url, (res) => {
        if (
          [301, 302, 303, 307, 308].includes(res.statusCode) &&
          res.headers.location &&
          redirectsLeft > 0
        ) {
          res.resume();
          return resolve(get(res.headers.location, redirectsLeft - 1));
        }
        if (res.statusCode !== 200) {
          res.resume();
          return reject(
            new Error("HTTP " + res.statusCode + " for " + url)
          );
        }
        const chunks = [];
        res.on("data", (c) => chunks.push(c));
        res.on("end", () => resolve(Buffer.concat(chunks)));
        res.on("error", reject);
      })
      .on("error", reject);
  });
}

async function main() {
  if (process.env.NOMAD_SKIP_DOWNLOAD === "1") {
    log("skipping binary download (NOMAD_SKIP_DOWNLOAD=1)");
    return;
  }

  let goos, goarch;
  try {
    goos = platformName();
    goarch = archName();
  } catch (e) {
    log(e.message + " — skipping native binary install");
    return;
  }

  const version = pkgVersion();
  const tag = "v" + version;
  const base =
    process.env.NOMAD_DOWNLOAD_BASE_URL ||
    DEFAULT_BASE;
  const archiveName = "nomad_" + version + "_" + goos + "_" + goarch + ".tar.gz";
  const url = base.replace(/\/$/, "") + "/" + tag + "/" + archiveName;

  const dest = path.join(installRoot(), "bin", "prebuilt");
  fs.mkdirSync(dest, { recursive: true });
  const destBin = path.join(dest, binaryName());

  log("fetching " + archiveName);
  const gz = await get(url, 5);
  untarGz(gz, dest);

  let produced = destBin;
  if (!fs.existsSync(produced)) {
    const find = (dir) => {
      for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
        const full = path.join(dir, entry.name);
        if (entry.isFile() && entry.name === binaryName()) return full;
        if (entry.isDirectory()) {
          const hit = find(full);
          if (hit) return hit;
        }
      }
      return null;
    };
    produced = find(dest);
  }
  if (!produced) {
    throw new Error("binary not found in archive " + archiveName);
  }
  if (produced !== destBin) {
    fs.copyFileSync(produced, destBin);
  }
  fs.chmodSync(destBin, 0o755);
  log("installed " + binaryName() + " " + version + " (" + goos + "/" + goarch + ")");
}

main().catch((err) => {
  console.error("[nomad] install failed: " + err.message);
  console.error(
    "[nomad] build from source instead: go install github.com/chyroc/nomad/cmd/nomad@latest"
  );
  process.exit(1);
});
