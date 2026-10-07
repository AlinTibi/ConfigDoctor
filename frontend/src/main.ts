import "./style.css";
import {
  api,
  type Summary,
  type Settings,
  type Query,
  type Entry,
  type Issue,
} from "./api";

const icon = `<svg viewBox="0 0 48 48" aria-hidden="true"><path d="M12 5h17l8 8v30H12z" fill="none" stroke="currentColor" stroke-width="2.5"/><path d="M29 5v9h8M18 23h12M18 29h7" fill="none" stroke="currentColor" stroke-width="2.5"/><path d="m26 35 4 4 8-9" fill="none" stroke="#a5e7d5" stroke-width="3"/></svg>`;
const root = document.querySelector<HTMLDivElement>("#app")!;
let files: Summary[] = [],
  prefs: Settings,
  mode = "inspect",
  selected = "",
  actualID = "",
  baselineID = "",
  page = 0,
  search = "",
  filter = "all",
  reveal = false,
  busy = false,
  keepSafe = false,
  transpose = false,
  request = 0;
const esc = (v: unknown) =>
  String(v ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ]!,
  );
root.innerHTML = `<aside class="sidebar"><div class="brand">${icon}<div><strong>Config Doctor</strong><span>ALMARFELD</span></div></div><nav aria-label="Workspace"><button data-mode="inspect" class="active">Inspect</button><button data-mode="compare">Compare</button><button data-mode="baseline">Baseline</button><button data-mode="generate">Generate example</button><button data-mode="settings">Settings</button></nav><div class="sidebar-note"><span class="dot"></span> Local workspace<p>Files stay on your computer.<br>No account or uploads.</p><small>v1.1.0 · Windows x64</small></div></aside><div class="workspace"><header><div><span class="eyebrow">Configuration workspace</span><h1 id="title">Inspect files</h1></div><div class="top-actions"><button id="add-files" class="primary">＋ Add files</button><button id="add-folder">Add folder</button><button id="export" disabled>Export report</button><button id="clear" disabled>Clear</button></div></header><div id="notice" role="status" class="notice" hidden></div><div id="file-list" class="file-list" aria-label="Loaded files"></div><div id="tools" class="tools" hidden><input id="search" type="search" placeholder="Search key names…" aria-label="Search key names"><select id="filter" aria-label="Filter results"><option value="all">All results</option><option value="errors">Errors</option><option value="warnings">Warnings</option><option value="missing">Missing / extra</option><option value="extra">Extra actual keys</option><option value="present">Present baseline keys</option><option value="references">Unresolved / cyclic references</option><option value="different">Different values</option><option value="empty">Empty values</option><option value="possible secrets">Possible secrets</option></select><label class="toggle"><input type="checkbox" id="reveal"> Reveal values</label></div><main id="content"></main><footer><span id="status">Ready</span><span id="counts">0 files · 0 errors · 0 warnings</span></footer></div><dialog id="dialog"><div class="dialog-heading"><h2 id="dialog-title"></h2><button id="dialog-close" aria-label="Close preview">✕</button></div><div id="dialog-controls"></div><pre id="preview" tabindex="0"></pre><div class="dialog-actions"><button id="dialog-cancel">Cancel</button><button id="dialog-save" class="primary">Save new file…</button></div></dialog>`;
const $ = <T extends HTMLElement = HTMLElement>(id: string) =>
  document.getElementById(id) as T;
function notice(text: string) {
  $("notice").textContent = text;
  $("notice").hidden = !text;
}
function status(text: string) {
  $("status").textContent = text;
}
function loading(value: boolean) {
  busy = value;
  for (const id of ["add-files", "add-folder", "clear", "export"])
    $(id).toggleAttribute(
      "disabled",
      value || (["clear", "export"].includes(id) && files.length === 0),
    );
  $("content").setAttribute("aria-busy", String(value));
}
async function run(action: () => Promise<unknown>) {
  if (busy) return;
  loading(true);
  notice("");
  try {
    await action();
  } catch (e) {
    notice(String(e));
    status("Action failed");
  } finally {
    loading(false);
  }
}
function drawFiles() {
  const totals = files.reduce(
    (t, f) => ({
      errors: t.errors + f.errors,
      warnings: t.warnings + f.warnings,
    }),
    { errors: 0, warnings: 0 },
  );
  $("counts").textContent =
    `${files.length} files · ${totals.errors} errors · ${totals.warnings} warnings`;
  $("file-list").innerHTML = files
    .map(
      (f) =>
        `<div class="file-chip ${f.id === selected ? "selected" : ""}"><button class="file-select" data-id="${f.id}" title="${esc(f.path)}"><span class="file-type ${!f.valid ? "invalid" : ""}">${esc(f.format.toUpperCase() || "?")}</span><span><strong>${esc(f.name)}</strong><small>${f.keys.toLocaleString()} entries · ${f.valid ? "Valid syntax" : "Syntax error"}</small></span></button><button class="file-remove" data-remove="${f.id}" aria-label="Remove ${esc(f.name)} from workspace">×</button></div>`,
    )
    .join("");
  loading(busy);
}
async function imported(result: Summary[]) {
  files = result;
  reveal = false;
  $<HTMLInputElement>("reveal").checked = false;
  if (!files.some((f) => f.id === selected)) selected = files[0]?.id || "";
  const valid = files.filter((f) => f.valid);
  if (!valid.some((f) => f.id === actualID))
    actualID = valid.find((f) => f.name === ".env")?.id || valid[0]?.id || "";
  if (!valid.some((f) => f.id === baselineID) || baselineID === actualID)
    baselineID =
      valid.find((f) => f.id !== actualID && f.name === ".env.example")?.id ||
      valid.find((f) => f.id !== actualID)?.id ||
      "";
  page = 0;
  drawFiles();
  await render();
  status("Ready");
}
function paged(total: number, pages: number) {
  return `<div class="pagination"><span>${total.toLocaleString()} matching entries · Page ${page + 1} of ${pages}</span><div><button id="previous" ${page === 0 ? "disabled" : ""}>← Previous</button><button id="next" ${page + 1 >= pages ? "disabled" : ""}>Next →</button></div></div>`;
}
function bindPages() {
  $("previous")?.addEventListener("click", () => {
    page--;
    void render();
  });
  $("next")?.addEventListener("click", () => {
    page++;
    void render();
  });
}
function value(e: Entry) {
  return `<span class="value ${e.secret ? "sensitive" : ""}">${esc(e.empty ? "(empty)" : e.value)}</span>${e.secret ? '<span class="tag warning">Possible secret</span>' : ""}`;
}
function diagnostics(issues: Issue[]) {
  const list = issues.filter(
    (i) =>
      (filter !== "errors" || i.severity === "error") &&
      (filter !== "warnings" || i.severity === "warning") &&
      (filter !== "empty" || i.code === "empty") &&
      (filter !== "possible secrets" || i.code === "secret") &&
      (filter !== "references" ||
        ["unresolved reference", "cyclic reference"].includes(i.code)) &&
      (!search ||
        i.key.toLowerCase().includes(search.toLowerCase()) ||
        i.message.toLowerCase().includes(search.toLowerCase())),
  );
  return `<section class="diagnostics"><div class="section-label"><h2>Diagnostics</h2><span>${list.length.toLocaleString()} findings</span></div>${
    list.length
      ? `<ul>${list
          .slice(0, 200)
          .map(
            (i) =>
              `<li><span class="tag ${i.severity}">${esc(i.severity)}</span><div><strong>${esc(i.code === "summary" ? "" : i.key)}</strong><p>${esc(i.message)}${i.line ? ` <span class="muted">Line ${i.line}</span>` : ""}</p>${i.key ? `<code>${esc(i.key)}</code>` : ""}</div></li>`,
          )
          .join(
            "",
          )}</ul>${list.length > 200 ? '<p class="muted">Showing the first 200 findings. Search by key to narrow the list; exported reports contain all findings.</p>' : ""}`
      : '<p class="muted">No matching diagnostics.</p>'
  }</section>`;
}
async function render() {
  const token = ++request;
  $("title").textContent = (
    {
      inspect: "Inspect files",
      compare: "Compare configurations",
      baseline: "Compare against baseline",
      generate: "Generate .env.example",
      settings: "Settings",
    } as Record<string, string>
  )[mode];
  document
    .querySelectorAll("[data-mode]")
    .forEach((b) =>
      b.classList.toggle("active", (b as HTMLElement).dataset.mode === mode),
    );
  $("tools").hidden =
    !files.length || !["inspect", "compare", "baseline"].includes(mode);
  for (const o of $<HTMLSelectElement>("filter").options)
    o.disabled =
      (mode === "inspect" &&
        ["missing", "different", "extra", "present"].includes(o.value)) ||
      (mode !== "baseline" && ["extra", "present"].includes(o.value)) ||
      (mode === "compare" && o.value === "references") ||
      (mode === "baseline" && ["different", "errors"].includes(o.value));
  $<HTMLSelectElement>("filter").querySelector(
    'option[value="missing"]',
  )!.textContent =
    mode === "baseline" ? "Missing baseline keys" : "Missing / extra";
  if ($<HTMLSelectElement>("filter").selectedOptions[0]?.disabled) {
    filter = "all";
    $<HTMLSelectElement>("filter").value = filter;
  }
  if (mode === "settings") {
    renderSettings();
    return;
  }
  if (files.length === 0) {
    $("content").innerHTML =
      `<section class="empty-state">${icon}<h2>A clear view of your configuration</h2><p>Add files or drop them anywhere in this window.<br>Inspect syntax, compare keys and prepare a value-free example.</p><div class="format-list">.env <span>JSON</span><span>YAML</span><span>TOML</span><span>INI</span></div><p class="muted">Sources are read-only. Values are masked until you explicitly reveal them.</p><button id="empty-add" class="primary">Choose configuration files</button></section>`;
    $("empty-add").onclick = () => openLoad(false);
    return;
  }
  try {
    const q: Query = {
      page,
      size: mode === "compare" ? 40 : 100,
      search,
      filter,
      reveal,
    };
    if (mode === "inspect") {
      const v = await api.Inspect(selected, q);
      if (token !== request) return;
      $("content").innerHTML =
        `<div class="metrics"><div><span>Syntax</span><strong class="${v.summary.valid ? "good" : "error-text"}">${v.summary.valid ? "Valid" : "Invalid"}</strong></div><div><span>Normalized entries</span><strong>${v.summary.keys.toLocaleString()}</strong></div><div><span>Empty values</span><strong>${v.summary.empty}</strong></div><div><span>Possible secrets</span><strong>${v.summary.secrets}</strong></div></div><section><div class="section-label"><h2>Key tree</h2><span>Containers and indexed arrays are explicit</span></div><div class="table-scroll"><table><thead><tr><th>Key path</th><th>Type</th><th>Value</th></tr></thead><tbody>${v.entries.map((e) => `<tr><td><code>${esc(e.key)}</code></td><td><span class="tag">${esc(e.type)}</span></td><td>${value(e)}</td></tr>`).join("") || '<tr><td colspan="3" class="muted">No matching entries. Review diagnostics below.</td></tr>'}</tbody></table></div>${paged(v.total, v.pages)}</section>${diagnostics(v.diagnostics)}`;
      bindPages();
    } else if (mode === "baseline") {
      const options = (id: string) =>
        `<option value="">Choose a file…</option>${files
          .filter((f) => f.valid)
          .map(
            (f) =>
              `<option value="${f.id}" ${f.id === id ? "selected" : ""}>${esc(f.name)} (${esc(f.format)})</option>`,
          )
          .join("")}`;
      const controls = `<section class="baseline-controls"><label class="field-label">Actual configuration<select id="actual-file">${options(actualID)}</select></label><label class="field-label">Baseline / template<select id="baseline-file">${options(baselineID)}</select></label></section><p class="muted">Baseline keys indicate expected presence, not proven requiredness. Extra keys are informational. Empty template defaults do not make actual values missing. References use definitions in the actual file only.</p>`;
      let body = "";
      if (actualID && baselineID && actualID !== baselineID) {
        const c = await api.CompareBaseline(actualID, baselineID, q);
        if (token !== request) return;
        const n = c.baseline!.counts;
        body = `<div class="metrics baseline-metrics">${[
          ["Missing baseline", n.missing],
          ["Present", n.present],
          ["Extra", n.extra],
          ["Empty actual", n.empty],
          ["Unresolved references", n.unresolved],
          ["Reference cycles", n.cycles],
        ]
          .map(
            ([label, count]) =>
              `<div><span>${label}</span><strong>${count}</strong></div>`,
          )
          .join(
            "",
          )}</div><div class="table-scroll comparison"><table><thead><tr><th>Key path</th><th>Actual: ${esc(c.files[0].name)}</th><th>Baseline: ${esc(c.files[1].name)}</th><th>Status</th></tr></thead><tbody>${c.rows.map((r) => `<tr><th><code>${esc(r.key)}</code></th>${r.cells.map((cell) => `<td>${!cell.present ? '<span class="muted">Absent</span>' : value(cell.entry)}</td>`).join("")}<td>${r.status.map((s) => `<span class="tag ${s === "present" ? "good" : s === "extra" ? "info" : "warning"}">${esc(s)}</span>`).join("")}</td></tr>`).join("")}</tbody></table>${c.rows.length ? "" : '<p class="muted">No matching keys.</p>'}</div>${paged(c.total, c.pages)}`;
        const v = await api.Inspect(actualID, {
          ...q,
          filter: filter === "references" ? "references" : "all",
        });
        if (token !== request) return;
        body += diagnostics(v.diagnostics);
      } else
        body =
          '<p class="muted">Choose two different syntax-valid files. Inspect files with validation errors before using them as an actual configuration or baseline.</p>';
      $("content").innerHTML = controls + body;
      for (const id of ["actual-file", "baseline-file"])
        $(id).onchange = () => {
          actualID = $<HTMLSelectElement>("actual-file").value;
          baselineID = $<HTMLSelectElement>("baseline-file").value;
          page = 0;
          reveal = false;
          $<HTMLInputElement>("reveal").checked = false;
          void render();
        };
      bindPages();
    } else if (mode === "compare") {
      const c = await api.Compare(q);
      if (token !== request) return;
      let table = "";
      if (!transpose) {
        table = `<thead><tr><th>Key path</th>${c.files.map((f) => `<th title="${esc(f.path)}">${esc(f.name)}<small>${esc(f.format)}${f.valid ? "" : " · INVALID"}</small></th>`).join("")}<th>Status</th></tr></thead><tbody>${c.rows.map((r) => `<tr><th><code>${esc(r.key)}</code></th>${r.cells.map((cell) => `<td class="${cell.invalid ? "invalid-cell" : !cell.present ? "missing-cell" : ""}">${cell.invalid ? "Invalid file" : !cell.present ? "Missing" : value(cell.entry)}</td>`).join("")}<td>${r.status.map((s) => `<span class="tag ${s === "same" ? "good" : "warning"}">${esc(s)}</span>`).join("")}</td></tr>`).join("")}</tbody>`;
      } else {
        table = `<thead><tr><th>File / environment</th>${c.rows.map((r) => `<th><code>${esc(r.key)}</code><small>${esc(r.status.join(" · "))}</small></th>`).join("")}</tr></thead><tbody>${c.files
          .map(
            (f, i) =>
              `<tr><th>${esc(f.name)}</th>${c.rows
                .map((r) => {
                  const cell = r.cells[i];
                  return `<td class="${!cell.present ? "missing-cell" : ""}">${cell.invalid ? "Invalid file" : !cell.present ? "Missing" : value(cell.entry)}</td>`;
                })
                .join("")}</tr>`,
          )
          .join("")}</tbody>`;
      }
      $("content").innerHTML =
        `<div class="compare-intro"><p>${files.length < 2 ? "Add a second file to compare environments." : "Missing means a key exists in another valid file. Invalid files are not treated as empty configurations."}</p><button id="transpose">${transpose ? "Keys as rows" : "Files as rows"}</button></div><div class="table-scroll comparison"><table>${table}</table>${c.rows.length ? "" : '<p class="muted">No matching keys. Inspect invalid files for syntax errors.</p>'}</div>${paged(c.total, c.pages)}`;
      $("transpose").onclick = () => {
        transpose = !transpose;
        void render();
      };
      bindPages();
    } else if (mode === "generate") {
      const candidates = files.filter((f) => f.format === "env" && f.valid);
      $("content").innerHTML =
        `<section class="generator"><h2>Prepare an example without real values</h2><p>Keys and ordering are preserved. Original comment text is replaced with a placeholder because comments can contain secrets. Duplicate variables appear once.</p><label class="field-label">Source .env file<select id="env-source">${candidates.map((f) => `<option value="${f.id}" ${f.id === selected ? "selected" : ""}>${esc(f.name)}</option>`).join("")}</select></label><label class="toggle"><input type="checkbox" id="keep-safe" ${keepSafe ? "checked" : ""}> Keep only obvious safe defaults (ports, booleans and environment names)</label><p class="muted">All other values are removed, including URLs and possible secrets. Always review the preview before sharing.</p><button class="primary" id="generate-preview" ${candidates.length ? "" : "disabled"}>Preview .env.example</button>${candidates.length ? "" : '<p class="muted">Load a valid .env file to generate an example.</p>'}</section>`;
      $("keep-safe").onchange = () => {
        keepSafe = $<HTMLInputElement>("keep-safe").checked;
      };
      $("generate-preview").onclick = () =>
        void run(async () => {
          const id = $<HTMLSelectElement>("env-source").value;
          const safe = keepSafe;
          showDialog(
            "Example configuration",
            await api.PreviewExample(id, safe),
            destinationControls("env"),
            async () =>
              api.SaveExampleTo(
                id,
                safe,
                $<HTMLInputElement>("destination").value,
                $<HTMLInputElement>("reviewed").checked,
              ),
          );
          bindDestination("env");
        });
    }
  } catch (e) {
    if (token === request) {
      notice(String(e));
      $("content").innerHTML =
        "<p>Unable to display this view. Choose a loaded file or try again.</p>";
    }
  }
}
function renderSettings() {
  $("content").innerHTML =
    `<section class="settings"><h2>Workspace preferences</h2><label class="field-label">Theme<select id="theme"><option value="dark" ${prefs.theme === "dark" ? "selected" : ""}>Dark</option><option value="system" ${prefs.theme === "system" ? "selected" : ""}>System</option></select></label><label class="toggle"><input id="mask-setting" type="checkbox" ${!reveal ? "checked" : ""}> Mask values in this session</label><p class="muted">Revealing is temporary. Values are always masked on restart and after import.</p><label class="toggle"><input id="remember" type="checkbox" ${prefs.rememberRecent ? "checked" : ""}> Remember recent file paths locally</label><label class="field-label">Default export format<select id="default-format">${["html", "json", "txt"].map((f) => `<option value="${f}" ${prefs.exportFormat === f ? "selected" : ""}>${f.toUpperCase()}</option>`).join("")}</select></label><label class="toggle"><input id="recursive" type="checkbox" ${prefs.recursive ? "checked" : ""}> Recursive folder scan (confirmation required each time)</label><p class="muted">Default scans examine only the chosen folder. Imports are bounded to 500 files, 250,000 entries, 128 MiB total source text and 16 MiB per file.</p><button id="save-settings" class="primary">Save preferences</button><h2>Recent files</h2><p class="muted">Paths only. File contents and revealed values are never stored.</p><div class="recent">${prefs.recent?.map((p) => `<button data-recent="${esc(p)}">${esc(p)}</button>`).join("") || '<p class="muted">No recent paths saved.</p>'}</div><button id="clear-recent">Clear recent files</button><h2>Limitations</h2><p>Possible-secret detection uses key names and common token patterns. It can miss secrets or flag safe values. This is not a complete security scanner.</p><p>UTF-8 only. YAML merge keys, complex mapping keys, cyclic aliases and multi-document streams are rejected. JSON/YAML duplicates use the final value with a warning; TOML duplicates are errors. INI values remain strings. Cross-format comparisons preserve types and array indexes.</p></section>`;
  $("mask-setting").onchange = () =>
    void setReveal(!$<HTMLInputElement>("mask-setting").checked);
  $("save-settings").onclick = () =>
    void run(async () => {
      prefs = {
        ...prefs,
        theme: $<HTMLSelectElement>("theme").value,
        rememberRecent: $<HTMLInputElement>("remember").checked,
        recursive: $<HTMLInputElement>("recursive").checked,
        exportFormat: $<HTMLSelectElement>("default-format").value,
      };
      await api.SaveSettings(prefs);
      prefs = await api.GetSettings();
      applyTheme();
      status("Preferences saved");
      await render();
    });
  $("clear-recent").onclick = () =>
    void run(async () => {
      await api.ClearRecent();
      prefs = await api.GetSettings();
      await render();
    });
  document.querySelectorAll<HTMLButtonElement>("[data-recent]").forEach(
    (b) =>
      (b.onclick = () =>
        void run(async () => {
          await imported(await api.LoadPaths([b.dataset.recent!]));
          mode = "inspect";
          await render();
        })),
  );
}
function applyTheme() {
  document.documentElement.dataset.theme = prefs.theme;
}
function destinationControls(format: string) {
  return `<label class="field-label wide">New file destination<div class="destination-row"><input id="destination" aria-label="New file destination" placeholder="Choose a new file or paste its full path"><button id="browse-destination">Browse…</button></div></label><label class="toggle"><input type="checkbox" id="reviewed"> I reviewed this preview and confirm saving to the selected new file.</label>`;
}
function bindDestination(format: string) {
  $("browse-destination").onclick = () =>
    void run(async () => {
      const p = await api.ChooseDestination(
        $("report-format")
          ? $<HTMLSelectElement>("report-format").value
          : format,
      );
      if (p) $<HTMLInputElement>("destination").value = p;
    });
}
function openLoad(folder: boolean) {
  showDialog(
    folder ? "Add configuration folder" : "Add configuration files",
    folder
      ? "Scans supported configuration formats in the selected folder. Nonrecursive by default."
      : "One full file path per line. You can also use the native file picker below.",
    `<label class="field-label wide">${folder ? "Folder path" : "File paths"}<textarea id="load-paths" aria-label="${folder ? "Folder path" : "File paths"}" rows="4" spellcheck="false"></textarea></label><button id="browse-load">Browse…</button>`,
    async () => {
      const paths = $<HTMLTextAreaElement>("load-paths")
        .value.split("\n")
        .map((p) => p.trim().replace(/^"|"$/g, ""))
        .filter(Boolean);
      if (paths.length === 0)
        throw new Error("Enter a full file or folder path.");
      await imported(await api.LoadPaths(paths));
      return "Files loaded";
    },
  );
  $("dialog-save").textContent = "Load configurations";
  $("browse-load").onclick = () =>
    void run(async () => {
      const result = folder ? await api.AddFolder() : await api.AddFiles();
      await imported(result);
      $<HTMLDialogElement>("dialog").close();
      $("preview").textContent = "";
      $("dialog-controls").innerHTML = "";
    });
}
let saveAction: () => Promise<string>;
function showDialog(
  title: string,
  content: string,
  controls: string,
  save: () => Promise<string>,
) {
  $("dialog-title").textContent = title;
  $("preview").textContent = content;
  $("dialog-controls").innerHTML = controls;
  saveAction = save;
  $("dialog-save").textContent = "Save new file…";
  $<HTMLDialogElement>("dialog").showModal();
}
$("dialog-close").onclick = $("dialog-cancel").onclick = () => {
  $<HTMLDialogElement>("dialog").close();
  $("preview").textContent = "";
  $("dialog-controls").innerHTML = "";
  $<HTMLInputElement>("reveal").checked = reveal;
  if ($("mask-setting")) $<HTMLInputElement>("mask-setting").checked = !reveal;
};
$("dialog-save").onclick = () =>
  void run(async () => {
    const path = await saveAction();
    if (path) {
      $<HTMLDialogElement>("dialog").close();
      $("preview").textContent = "";
      $("dialog-controls").innerHTML = "";
      status(
        ["Session values revealed", "Files loaded"].includes(path)
          ? path
          : "Output saved to " + path,
      );
    }
  });
async function setReveal(next: boolean) {
  if (next && !reveal) {
    showDialog(
      "Reveal configuration values?",
      "Real values may include credentials. They will be shown only in this session. Hide them before taking screenshots or sharing your screen.",
      "",
      async () => {
        reveal = true;
        $<HTMLInputElement>("reveal").checked = true;
        await render();
        return "Session values revealed";
      },
    );
    $("dialog-save").textContent = "Reveal values";
    $<HTMLInputElement>("reveal").checked = false;
    return;
  }
  reveal = next;
  $<HTMLInputElement>("reveal").checked = next;
  await render();
}
document.querySelectorAll<HTMLButtonElement>("[data-mode]").forEach(
  (b) =>
    (b.onclick = () => {
      mode = b.dataset.mode!;
      page = 0;
      filter = "all";
      $<HTMLSelectElement>("filter").value = filter;
      void render();
    }),
);
$("file-list").onclick = (e) => {
  const target = (e.target as HTMLElement).closest<HTMLElement>(
    "[data-id],[data-remove]",
  );
  if (!target) return;
  if (target.dataset.remove)
    void run(async () => {
      await api.Remove(target.dataset.remove!);
      await imported(await api.Files());
    });
  else {
    selected = target.dataset.id!;
    page = 0;
    drawFiles();
    void render();
  }
};
$("add-files").onclick = () => openLoad(false);
$("add-folder").onclick = () => openLoad(true);
$("clear").onclick = () =>
  void run(async () => {
    await api.Clear();
    await imported([]);
  });
$("reveal").onchange = () =>
  void setReveal($<HTMLInputElement>("reveal").checked);
let debounce: ReturnType<typeof setTimeout>;
$("search").oninput = () => {
  clearTimeout(debounce);
  debounce = setTimeout(() => {
    search = $<HTMLInputElement>("search").value;
    page = 0;
    void render();
  }, 180);
};
$("filter").onchange = () => {
  filter = $<HTMLSelectElement>("filter").value;
  page = 0;
  void render();
};
$("export").onclick = () =>
  void run(async () => {
    const format = prefs.exportFormat;
    const baselineExport = mode === "baseline";
    const exportActual = actualID,
      exportBaseline = baselineID;
    const previewReport = (f: string) =>
      baselineExport
        ? api.PreviewBaselineReport(exportActual, exportBaseline, f)
        : api.PreviewReport(f, false);
    const saveReport = (
      f: string,
      include: boolean,
      path: string,
      reviewed: boolean,
      confirmed: boolean,
    ) =>
      baselineExport
        ? api.SaveBaselineReportTo(
            exportActual,
            exportBaseline,
            f,
            include,
            path,
            reviewed,
            confirmed,
          )
        : api.SaveReportTo(f, include, path, reviewed, confirmed);
    showDialog(
      "Export " + format.toUpperCase() + " report",
      await previewReport(format),
      `<label class="field-label">Report format<select id="report-format">${["html", "json", "txt"].map((f) => `<option value="${f}" ${f === format ? "selected" : ""}>${f.toUpperCase()}</option>`).join("")}</select></label>${destinationControls(format)}<label class="toggle"><input id="include-values" type="checkbox"> Include values (may expose secrets)</label><p class="muted">Preview is masked. Including real values requires a second confirmation. File names and keys remain visible.</p><label class="toggle" id="values-warning" hidden><input id="confirm-values" type="checkbox"> I understand this report will contain real values, including possible secrets.</label>`,
      async () =>
        saveReport(
          $<HTMLSelectElement>("report-format").value,
          $<HTMLInputElement>("include-values").checked,
          $<HTMLInputElement>("destination").value,
          $<HTMLInputElement>("reviewed").checked,
          $<HTMLInputElement>("confirm-values").checked,
        ),
    );
    bindDestination(format);
    $("report-format").onchange = () =>
      void run(async () => {
        const f = $<HTMLSelectElement>("report-format").value;
        $("dialog-title").textContent = "Export " + f.toUpperCase() + " report";
        $("preview").textContent = await previewReport(f);
        $<HTMLInputElement>("destination").value = "";
        $<HTMLInputElement>("reviewed").checked = false;
        bindDestination(f);
      });
    $("include-values").onchange = () => {
      $("values-warning").hidden =
        !$<HTMLInputElement>("include-values").checked;
      $<HTMLInputElement>("confirm-values").checked = false;
    };
    $("dialog-save").textContent = "Save reviewed report…";
  });
window.runtime.EventsOn("progress", (text) => status(String(text)));
window.runtime.EventsOn("notice", (text) => notice(String(text)));
window.runtime.OnFileDrop(
  (_x, _y, paths) => void run(async () => imported(await api.LoadPaths(paths))),
  false,
);
void (async () => {
  try {
    prefs = await api.GetSettings();
    applyTheme();
    files = await api.Files();
    selected = files[0]?.id || "";
    drawFiles();
    await render();
  } catch (e) {
    notice(String(e));
  }
})();
