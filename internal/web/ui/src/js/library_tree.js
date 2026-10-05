//! Browser library tree: expand/collapse per node (siblings stay open).
import { createLucideIcons } from "./dom_helpers.js";

function treeRoot() {
  return document.querySelector("[data-library-tree]");
}

function childrenSlot(node) {
  if (!node) return null;
  return node.querySelector(":scope > [data-tree-children]");
}

function expandBtn(node) {
  if (!node) return null;
  return node.querySelector(":scope > .library-tree-row [data-tree-expand]");
}

function collapseNode(node) {
  if (!node) return;
  const slot = childrenSlot(node);
  if (slot) slot.innerHTML = "";
  const btn = expandBtn(node);
  if (btn) {
    btn.setAttribute("aria-expanded", "false");
    const label = btn.getAttribute("aria-label") || "";
    btn.setAttribute("aria-label", label.replace(/^Collapse /, "Expand "));
  }
}

function markExpanded(btn) {
  if (!btn) return;
  btn.setAttribute("aria-expanded", "true");
  const label = btn.getAttribute("aria-label") || "";
  btn.setAttribute("aria-label", label.replace(/^Expand /, "Collapse "));
}

function onBeforeExpand(ev) {
  const btn = ev.target && ev.target.closest && ev.target.closest("[data-tree-expand]");
  if (!btn || !treeRoot() || !treeRoot().contains(btn)) return;
  const node = btn.closest(".library-tree-node");
  if (!node) return;
  if (btn.getAttribute("aria-expanded") === "true") {
    ev.preventDefault();
    collapseNode(node);
  }
}

function onAfterExpand(ev) {
  const root = treeRoot();
  if (!root) return;
  const target = ev.detail && ev.detail.target;
  if (!target || !root.contains(target)) return;
  const inChildren =
    target.hasAttribute("data-tree-children") ||
    target.closest("[data-tree-children]") ||
    target.hasAttribute("data-tree-load-more-wrap") ||
    target.closest("[data-tree-load-more-wrap]");
  if (!inChildren) return;
  createLucideIcons(target);
  const slot = target.hasAttribute("data-tree-children")
    ? target
    : target.closest("[data-tree-children]");
  if (!slot) return;
  if (target.hasAttribute("data-tree-load-more-wrap") || target.closest("[data-tree-load-more-wrap]")) {
    return;
  }
  markExpanded(expandBtn(slot.closest(".library-tree-node")));
}

export function bootLibraryTree() {
  document.body.addEventListener("htmx:beforeRequest", (ev) => {
    const elt = ev.detail && ev.detail.elt;
    if (!elt) return;
    if (elt.matches && elt.matches("[data-tree-expand]")) onBeforeExpand(ev);
  });
  document.body.addEventListener("htmx:afterSwap", onAfterExpand);
}
