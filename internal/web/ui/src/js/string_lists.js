import { createLucideIcons } from "./dom_helpers.js";
import { maybeAutosaveSettingsEditor } from "./settings_autosave.js";

export function moveListRow(row, dir) {
  if (!row || !row.parentElement) return;
  if (row.hasAttribute("data-string-list-managed-row")) return;
  if (dir < 0) {
    const prev = row.previousElementSibling;
    if (prev) row.parentElement.insertBefore(row, prev);
    return;
  }
  const next = row.nextElementSibling;
  if (next) row.parentElement.insertBefore(next, row);
}

export function actorRowHTML(name, role) {
  const esc = (s) =>
    String(s).replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;");
  const roleLabel = role ? esc(role) : "-";
  return (
    '<div class="flex gap-1 items-center" data-actor-row>' +
    '<input type="hidden" name="actor_name" value="' +
    esc(name) +
    '" />' +
    '<input type="hidden" name="actor_role" value="' +
    esc(role) +
    '" />' +
    '<span class="tabular-nums text-xs opacity-60 w-4 shrink-0 text-right" data-list-ord aria-hidden="true"></span>' +
    '<div class="join shrink-0">' +
    '<button type="button" class="btn btn-ghost btn-sm btn-square join-item" data-actor-up aria-label="Move actor up"><i data-lucide="chevron-up" class="size-4"></i></button>' +
    '<button type="button" class="btn btn-ghost btn-sm btn-square join-item" data-actor-down aria-label="Move actor down"><i data-lucide="chevron-down" class="size-4"></i></button>' +
    "</div>" +
    '<div class="grid grid-cols-2 gap-2 grow min-w-0 text-sm">' +
    '<span class="truncate">' +
    esc(name) +
    "</span>" +
    '<span class="truncate opacity-60">' +
    roleLabel +
    "</span>" +
    "</div>" +
    '<button type="button" class="btn btn-ghost btn-sm btn-square shrink-0" data-actor-remove aria-label="Remove actor"><i data-lucide="minus" class="size-4"></i></button>' +
    "</div>"
  );
}

export function syncActorsEmpty(list) {
  if (!list) return;
  const empty = list.querySelector("[data-actors-empty]");
  const hasRows = list.querySelector("[data-actor-row]");
  if (hasRows) {
    if (empty) empty.remove();
    return;
  }
  if (!empty) {
    const editor = list.closest("[data-actors-editor]");
    const label =
      (editor && editor.getAttribute("data-actors-empty-text")) || "none";
    const p = document.createElement("p");
    p.className = "text-sm opacity-60";
    p.setAttribute("data-actors-empty", "");
    p.textContent = label;
    list.insertAdjacentElement("afterbegin", p);
  }
}

export function commitActorDraft(editor) {
  if (!editor) return false;
  const nameEl = editor.querySelector("[data-actor-draft-name]");
  const roleEl = editor.querySelector("[data-actor-draft-role]");
  const list = editor.querySelector("[data-actors-list]");
  if (!nameEl || !list) return false;
  const name = String(nameEl.value || "").trim();
  if (!name) return false;
  const existing = Array.from(list.querySelectorAll('input[name="actor_name"]')).map((el) =>
    String(el.value || "").trim().toLowerCase()
  );
  if (existing.includes(name.toLowerCase())) {
    nameEl.value = "";
    if (roleEl) roleEl.value = "";
    nameEl.focus();
    return false;
  }
  const role = String((roleEl && roleEl.value) || "").trim();
  list.insertAdjacentHTML("beforeend", actorRowHTML(name, role));
  createLucideIcons(list.lastElementChild);
  syncActorsEmpty(list);
  nameEl.value = "";
  if (roleEl) roleEl.value = "";
  nameEl.focus();
  return true;
}

function managedListValues(editor) {
  if (!editor) return [];
  const raw = editor.getAttribute("data-string-list-managed") || "";
  if (!raw) return [];
  return raw.split("|").map((s) => s.trim()).filter(Boolean);
}

function isManagedListValue(editor, value) {
  const fold = String(value || "").trim().toLowerCase();
  if (!fold) return false;
  return managedListValues(editor).some((m) => m.toLowerCase() === fold);
}

export function stringListRowHTML(editor, value, managed) {
  const name = editor.getAttribute("data-item-name") || "item";
  const singular = editor.getAttribute("data-item-singular") || name;
  const unordered = editor.hasAttribute("data-string-list-unordered");
  const esc = (s) =>
    String(s).replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;");
  if (managed) {
    if (unordered) {
      return (
        '<span class="badge badge-lg gap-1 pr-0.5 max-w-full" data-string-list-row data-string-list-managed-row>' +
        '<input type="hidden" name="' +
        esc(name) +
        '" value="' +
        esc(value) +
        '" />' +
        '<span class="tooltip tooltip-top max-w-48" data-tip="Managed by Creatorr - this value is locked here and cannot be edited">' +
        '<span class="block truncate text-base-content/50">' +
        esc(value) +
        "</span></span></span>"
      );
    }
    return (
      '<div class="flex gap-2 items-center" data-string-list-row data-string-list-managed-row>' +
      '<input type="hidden" name="' +
      esc(name) +
      '" value="' +
      esc(value) +
      '" />' +
      '<span class="tabular-nums text-xs opacity-60 w-4 shrink-0 text-right" data-list-ord aria-hidden="true"></span>' +
      '<span class="tooltip tooltip-top grow min-w-0" data-tip="Managed by Creatorr - this value is locked here and cannot be edited">' +
      '<span class="block truncate text-sm text-base-content/50">' +
      esc(value) +
      "</span></span></div>"
    );
  }
  if (unordered) {
    return (
      '<span class="badge badge-lg gap-1 pr-0.5 max-w-full" data-string-list-row>' +
      '<input type="hidden" name="' +
      esc(name) +
      '" value="' +
      esc(value) +
      '" />' +
      '<span class="truncate max-w-48">' +
      esc(value) +
      "</span>" +
      '<button type="button" class="btn btn-ghost btn-xs btn-square h-4 min-h-4 w-4 p-0 shrink-0" data-string-list-remove aria-label="Remove ' +
      esc(singular) +
      '"><i data-lucide="x" class="size-3"></i></button>' +
      "</span>"
    );
  }
  return (
    '<div class="flex gap-1 items-center" data-string-list-row>' +
    '<input type="hidden" name="' +
    esc(name) +
    '" value="' +
    esc(value) +
    '" />' +
    '<span class="tabular-nums text-xs opacity-60 w-4 shrink-0 text-right" data-list-ord aria-hidden="true"></span>' +
    '<div class="join shrink-0">' +
    '<button type="button" class="btn btn-ghost btn-sm btn-square join-item" data-string-list-up aria-label="Move ' +
    esc(singular) +
    ' up"><i data-lucide="chevron-up" class="size-4"></i></button>' +
    '<button type="button" class="btn btn-ghost btn-sm btn-square join-item" data-string-list-down aria-label="Move ' +
    esc(singular) +
    ' down"><i data-lucide="chevron-down" class="size-4"></i></button>' +
    "</div>" +
    '<span class="grow min-w-0 text-sm truncate">' +
    esc(value) +
    "</span>" +
    '<button type="button" class="btn btn-ghost btn-sm btn-square shrink-0" data-string-list-remove aria-label="Remove ' +
    esc(singular) +
    '"><i data-lucide="minus" class="size-4"></i></button>' +
    "</div>"
  );
}

export function syncStringListEmptyLabel(editor) {
  if (!editor) return;
  const list = editor.querySelector("[data-string-list]");
  const emptyText = editor.getAttribute("data-string-list-empty");
  if (!list || !emptyText) return;
  const hasRows = !!list.querySelector("[data-string-list-row]");
  let label = list.querySelector("[data-string-list-empty-label]");
  if (hasRows) {
    if (label) label.remove();
    return;
  }
  if (!label) {
    label = document.createElement("p");
    const unordered = editor.hasAttribute("data-string-list-unordered");
    label.className = "text-sm opacity-60" + (unordered ? " w-full" : "");
    label.setAttribute("data-string-list-empty-label", "");
    label.textContent = emptyText;
    list.prepend(label);
  }
}

export function commitStringListDraft(editor, value) {
  if (!editor) return false;
  const name = editor.getAttribute("data-item-name") || "item";
  const input = editor.querySelector("[data-string-list-draft-value]");
  const list = editor.querySelector("[data-string-list]");
  if (!input || !list) return false;
  const item = String(value != null ? value : input.value || "").trim();
  if (!item) return false;
  if (item.toLowerCase() === "unknown") {
    input.value = "";
    input.focus();
    return false;
  }
  const existing = Array.from(list.querySelectorAll('input[name="' + name + '"]')).map((el) =>
    String(el.value || "").trim().toLowerCase()
  );
  if (existing.includes(item.toLowerCase())) {
    input.value = "";
    input.focus();
    return false;
  }
  if (isManagedListValue(editor, item)) {
    input.value = "";
    input.focus();
    return false;
  }
  list.insertAdjacentHTML("beforeend", stringListRowHTML(editor, item, false));
  createLucideIcons(list.lastElementChild);
  syncStringListEmptyLabel(editor);
  input.value = "";
  input.focus();
  maybeAutosaveSettingsEditor(editor);
  return true;
}
