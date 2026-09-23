"use strict";

const fs = require("fs");
const path = require("path");

function platformName() {
  switch (process.platform) {
    case "darwin":
      return "darwin";
    case "linux":
      return "linux";
    case "freebsd":
      return "freebsd";
    default:
      throw new Error("unsupported platform: " + process.platform);
  }
}

function archName() {
  switch (process.arch) {
    case "x64":
      return "amd64";
    case "arm64":
      return "arm64";
    default:
      throw new Error("unsupported architecture: " + process.arch);
  }
}

function pkgVersion() {
  return require("../package.json").version;
}

function installRoot() {
  return path.join(__dirname, "..");
}

function binaryName() {
  return process.platform === "win32" ? "nomad.exe" : "nomad";
}

function binaryPath() {
  return path.join(installRoot(), "bin", "prebuilt", binaryName());
}

function hasBinary() {
  try {
    fs.accessSync(binaryPath(), fs.constants.X_OK);
    return true;
  } catch {
    return false;
  }
}

function resolveBinaryPath() {
  return hasBinary() ? binaryPath() : null;
}

module.exports = {
  platformName,
  archName,
  pkgVersion,
  installRoot,
  binaryName,
  binaryPath,
  hasBinary,
  resolveBinaryPath,
};
