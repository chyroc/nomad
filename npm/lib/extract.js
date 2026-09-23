"use strict";

const fs = require("fs");
const path = require("path");
const zlib = require("zlib");

// Minimal POSIX untar for the flat archives GoReleaser produces (regular
// files/directories, gzip-compressed). No third-party dependencies.
function untarGz(gzBuffer, destDir) {
  const buffer = zlib.gunzipSync(gzBuffer);
  let offset = 0;
  const files = new Map();
  while (offset + 512 <= buffer.length) {
    const block = buffer.subarray(offset, offset + 512);
    if (block.every((b) => b === 0)) {
      offset += 512;
      continue;
    }
    const name = block
      .subarray(0, 100)
      .toString("utf8")
      .replace(/\0.*$/, "");
    const sizeOctal = block
      .subarray(124, 136)
      .toString("utf8")
      .replace(/[^0-7]/g, "");
    const size = parseInt(sizeOctal || "0", 8);
    const type = String.fromCharCode(block[156]);
    offset += 512;
    const data = buffer.subarray(offset, offset + size);
    offset += Math.ceil(size / 512) * 512;
    if (type === "0" || type === "\0") {
      files.set(name, data);
    }
  }
  for (const [name, data] of files) {
    const target = path.join(destDir, name);
    if (!target.startsWith(destDir + path.sep) && target !== destDir) {
      throw new Error("unsafe tar entry: " + name);
    }
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, data);
  }
}

module.exports = { untarGz };
