#!/usr/bin/env node
"use strict";

const fs = require("fs");
const path = require("path");

const root = path.join(__dirname, "..");
const destDir = path.join(root, "..", "static", "vendor");

const copies = [
  {
    src: path.join(root, "node_modules", "echarts", "dist", "echarts.min.js"),
    dest: path.join(destDir, "echarts.min.js"),
    label: "echarts.min.js",
  },
  {
    src: path.join(root, "node_modules", "lucide", "dist", "umd", "lucide.min.js"),
    dest: path.join(destDir, "lucide.min.js"),
    label: "lucide.min.js",
  },
];

fs.mkdirSync(destDir, { recursive: true });
for (const c of copies) {
  if (!fs.existsSync(c.src)) {
    console.error(c.label + " missing - run npm install in internal/web/ui first");
    process.exit(1);
  }
  fs.copyFileSync(c.src, c.dest);
  console.log("copied", path.relative(root, c.dest));
}
