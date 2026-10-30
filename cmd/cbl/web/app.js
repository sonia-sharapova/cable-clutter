// Cable Clutter in the browser. The server sends every searchable thing once
// (/api/index); this page builds a hashmap from every prefix of every word
// start to the things it names, so each keystroke is one Map lookup.
"use strict";

const MAX_PREFIX = 24;
const KIND_ORDER = ["device", "cable", "adapter", "system", "type", "model", "connector"];
const KIND_LABEL = { device: "your device", cable: "your cable", adapter: "your adapter", system: "system", type: "cable type", model: "device model", connector: "connector" };

let index = null; // { entries, prefix: Map<string, number[]>, exact: Map<string, number[]>, keys, ms, devices, counts }
let selected = 0;

// --- tiny DOM helpers ------------------------------------------------------

const $ = (sel) => document.querySelector(sel);

// el("p", {class: "meta"}, "text", child…): text is always set as text.
function el(tag, attrs, ...children) {
  const n = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v === false || v == null) continue;
    if (k.startsWith("on")) n.addEventListener(k.slice(2), v);
    else n.setAttribute(k, v === true ? "" : v);
  }
  return fill(n, ...children);
}

// fill replaces a node's children. Children may be nested arrays; null and
// false are skipped. Always use this, never replaceChildren directly: that
// would print nested arrays as "[object HTMLElement]".
function fill(n, ...children) {
  n.replaceChildren();
  for (const c of children.flat(Infinity)) {
    if (c == null || c === false) continue;
    n.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
  return n;
}

async function api(path, body) {
  const opt = body ? { method: "POST", headers: { "Content-Type": "application/json", "X-CBL": "1" }, body: JSON.stringify(body) } : {};
  const res = await fetch(path, opt);
  const data = await res.json();
  if (!res.ok) throw new Error(data.error || res.statusText);
  return data;
}

const shortId = (id) => id.replaceAll("-", "").slice(26);
// What each status means, in the words the page shows and on hover.
const STATUS = {
  free: ["available", "Not used by any setup that's switched on: you can use it"],
  reserved: ["reserved", "Pinned to a setup that's switched off: free now, but that setup expects it"],
  "in use": ["in use", "Plugged into a setup that's switched on"],
  up: ["on", "This setup is switched on: its cables are in use"],
  down: ["off", "This setup is switched off"],
};
const ID_HINT = "Your item's ID: tells identical cables apart. Use it in commands, e.g. cbl show ";
const statusClass = (s) => (s || "").replace(" ", "");
const statusTag = (s) => el("span", { class: "status " + statusClass(s), title: (STATUS[s] || [])[1] }, (STATUS[s] || [s])[0]);
const idTag = (id) => el("span", { class: "meta", title: ID_HINT + shortId(id) }, "your #" + shortId(id));

// --- the index -------------------------------------------------------------

// Same folding as the CLI: lower case; spaces, hyphens, underscores,
// slashes, brackets and commas dropped; "×" read as "x".
const norm = (s) => s.toLowerCase().replace(/[\s\-_/(),]+/g, "").replaceAll("×", "x");
const words = (s) => s.toLowerCase().split(/[\s\-_/(),]+/).filter(Boolean);

function rank(entries, i) {
  return KIND_ORDER.indexOf(entries[i].kind) * 1e6 + i;
}

function buildIndex(data) {
  const t0 = performance.now();
  const prefix = new Map();
  const exact = new Map();
  const add = (map, key, i) => {
    const list = map.get(key);
    if (!list) map.set(key, [i]);
    else if (list[list.length - 1] !== i) list.push(i);
  };
  data.entries.forEach((e, i) => {
    for (const term of e.terms) {
      add(exact, norm(term), i);
      const ws = words(term);
      for (let k = 0; k < ws.length; k++) {
        const suffix = norm(ws.slice(k).join(""));
        for (let n = 1; n <= Math.min(suffix.length, MAX_PREFIX); n++) add(prefix, suffix.slice(0, n), i);
      }
    }
  });
  // Rank each list once, so a lookup returns results already in order.
  const byRank = (a, b) => rank(data.entries, a) - rank(data.entries, b);
  for (const list of prefix.values()) {
    const unique = [...new Set(list)].sort(byRank);
    list.length = 0;
    list.push(...unique);
  }
  return { ...data, prefix, exact, keys: prefix.size, ms: performance.now() - t0 };
}

// lookup: exact name matches first, then everything with a word starting
// with q. One Map.get each; longer queries filter the 24-character list.
function lookup(q) {
  const key = norm(q);
  if (!key) return [];
  const exact = index.exact.get(key) || [];
  let hits = index.prefix.get(key.slice(0, MAX_PREFIX)) || [];
  if (key.length > MAX_PREFIX) {
    hits = hits.filter((i) => index.entries[i].terms.some((t) => words(t).some((_, k, ws) => norm(ws.slice(k).join("")).startsWith(key))));
  }
  const seen = new Set(exact);
  return exact.concat(hits.filter((i) => !seen.has(i)));
}

// --- search view -----------------------------------------------------------

function search() {
  const q = $("#q").value;
  const t0 = performance.now();
  const hits = lookup(q);
  const ms = performance.now() - t0;
  selected = 0;
  $("#timing").textContent = q
    ? `${hits.length} result${hits.length === 1 ? "" : "s"} in ${ms.toFixed(3)} ms · 1 hashmap lookup of ${index.keys.toLocaleString()} prefixes`
    : `${index.entries.length} things indexed as ${index.keys.toLocaleString()} prefixes in ${index.ms.toFixed(1)} ms`;
  const list = $("#results");
  fill(list,
    ...hits.slice(0, 60).map((i, n) => {
      const e = index.entries[i];
      return el("li", { "aria-selected": n === 0 ? "true" : "false", onclick: () => openEntry(e.id) },
        el("span", { class: "kind" }, KIND_LABEL[e.kind]),
        el("span", { class: "title" }, e.title), " ",
        e.status ? statusTag(e.status) : null,
        el("span", { class: "sub" }, e.sub));
    }),
  );
  if (hits.length > 60) list.append(el("li", { class: "meta" }, `… and ${hits.length - 60} more; keep typing`));
}

function move(by) {
  const items = [...$("#results").children].filter((li) => li.hasAttribute("aria-selected"));
  if (!items.length) return;
  selected = (selected + by + items.length) % items.length;
  items.forEach((li, n) => li.setAttribute("aria-selected", n === selected ? "true" : "false"));
  items[selected].scrollIntoView({ block: "nearest" });
}

// --- routing ---------------------------------------------------------------

// open shows an entry: "part:usb-a:m|usb-c:m" → #part/usb-a%3Am%7Cusb-c%3Am
function openEntry(id) {
  const i = id.indexOf(":");
  location.hash = "#" + id.slice(0, i) + "/" + encodeURIComponent(id.slice(i + 1));
}

async function route() {
  const hash = decodeURIComponent(location.hash.slice(1));
  const [kind, ...rest] = hash.split("/");
  const arg = rest.join("/");
  for (const a of document.querySelectorAll("nav a")) {
    a.toggleAttribute("aria-current", a.getAttribute("href") === "#" + (["inventory", "connect", "systems", "wish"].includes(kind) ? kind : ""));
  }
  const view = $("#view");
  $("#search").hidden = kind !== "";
  view.hidden = kind === "";
  if (kind === "") {
    $("#q").focus();
    return;
  }
  const render = { part: showPart, connector: showConnector, device: showDevice, model: showDevice, system: showSystem, inventory: showInventory, connect: showConnect, systems: showSystems, wish: showWish }[kind];
  if (!render) return;
  fill(view, el("p", { class: "meta" }, "…"));
  try {
    fill(view, el("a", { href: "#", class: "back meta" }, "← search"), await render(arg));
  } catch (err) {
    fill(view, el("a", { href: "#", class: "back meta" }, "← search"), el("p", { class: "warn" }, err.message));
  }
}

// --- detail views ----------------------------------------------------------

async function showPart(key) {
  const d = await api("/api/part?key=" + encodeURIComponent(key));
  const p = d.part;
  return [
    el("h2", {}, p.name),
    el("p", { class: "meta" }, p.key, p.active ? " · active (has electronics)" : "", p.volts ? ` · ${p.volts} V` : ""),
    el("pre", {}, d.outline),
    el("p", {}, "Carries: ", d.signals.join("; ")),
    p.note ? el("p", { class: "note" }, p.note) : null,
    el("div", { class: "row" },
      el("button", { onclick: async () => { await api("/api/item/add", { part: p.key }); await refresh(); route(); } },
        d.items.length ? "I have another" : "I have one"),
      wishButton([{ key: p.key, name: p.name }], "")),
    d.items.length
      ? el("table", {},
          el("tr", {}, ["your id", "status", "name", "where", "for"].map((h) => el("th", {}, h))),
          d.items.map((it) => el("tr", {},
            el("td", { class: "meta", title: ID_HINT + it.short_id }, "#" + it.short_id),
            el("td", {}, statusTag(it.status), it.held_by ? el("span", { class: "meta" }, " " + it.held_by) : null),
            el("td", {}, it.nickname || ""), el("td", {}, it.location || ""), el("td", {}, it.for || ""))))
      : el("p", { class: "meta" }, "You don't own one yet."),
  ];
}

// --- inventory: everything you own, filtered in the page ------------------

// --- inventory: everything you own, filtered and searched in the page -----

const invFilter = { kind: "all", category: "all", connector: "", status: "all", query: "", open: false };

// wordStarts lists every word-start suffix of some text, folded like the
// search index: "USB-A to Lightning" → usbatolightning, tolightning, lightning.
const wordStarts = (text) => words(text).map((_, k, ws) => norm(ws.slice(k).join("")));

async function showInventory() {
  const rows = await api("/api/inventory");
  for (const r of rows) {
    r.search = [r.name, r.kind, ...r.ends, ...r.categories, ...r.items.flatMap((it) => [it.nickname, it.location, it.for, it.short_id])]
      .filter(Boolean).flatMap(wordStarts);
  }
  const list = el("div", {});
  const select = (key, options) => el("select", { onchange: (e) => { invFilter[key] = e.target.value; draw(); } },
    options.map(([value, label]) => el("option", { value, selected: invFilter[key] === value }, label)));

  // Only offer categories and connectors that occur in what you own.
  const categories = [...new Set(rows.flatMap((r) => r.categories))].sort();
  const connectors = new Map();
  for (const r of rows) for (const c of r.connectors) connectors.set(c.id, { name: c.name, n: (connectors.get(c.id)?.n || 0) + r.items.length });
  const panel = el("div", { class: "filter-panel" },
    el("label", {}, "kind", select("kind", [["all", "all"], ["cables", "cables"], ["adapters", "adapters"]])),
    el("label", {}, "carries", select("category", [["all", "anything"], ...categories.map((c) => [c, c])])),
    el("label", {}, "connector", select("connector", [["", "any connector"],
      ...[...connectors].sort((a, b) => a[1].name.localeCompare(b[1].name)).map(([id, c]) => [id, `${c.name} (${c.n})`])])),
    el("label", {}, "status", select("status", [["all", "any"], ["available", "available"], ["in use", "in use"], ["reserved", "reserved"]])),
    el("button", { class: "quiet", onclick: () => { Object.assign(invFilter, { kind: "all", category: "all", connector: "", status: "all" }); route(); } }, "clear filters"));
  // Hovering the trigger opens the panel; a click pins it open (for touch
  // screens and keyboards).
  const bar = el("div", { class: "filter-bar" + (invFilter.open ? " open" : "") });
  const filterButton = el("button", { class: "trigger", "aria-expanded": String(invFilter.open), onclick: () => {
    invFilter.open = !invFilter.open;
    bar.classList.toggle("open", invFilter.open);
    filterButton.setAttribute("aria-expanded", String(invFilter.open));
  } });
  const filterLabel = () => {
    const n = ["kind", "category", "status"].filter((k) => invFilter[k] !== "all").length + (invFilter.connector ? 1 : 0);
    fill(filterButton, `Filter${n ? ` (${n})` : ""}`);
  };
  const searchBox = el("input", { type: "search", class: "inline", placeholder: "search your inventory", value: invFilter.query,
    oninput: (e) => { invFilter.query = e.target.value; draw(); } });

  function draw() {
    filterLabel();
    const q = norm(invFilter.query);
    const shown = [];
    for (const r of rows) {
      if (invFilter.kind !== "all" && r.kind + "s" !== invFilter.kind) continue;
      if (invFilter.category !== "all" && !r.categories.includes(invFilter.category)) continue;
      if (invFilter.connector && !r.connectors.some((c) => c.id === invFilter.connector)) continue;
      if (q && !r.search.some((w) => w.startsWith(q))) continue;
      const items = invFilter.status === "all" ? r.items : r.items.filter((it) => STATUS[it.status][0] === invFilter.status);
      if (items.length) shown.push({ ...r, items });
    }
    const n = shown.reduce((sum, r) => sum + r.items.length, 0);
    fill(list,
      el("p", { class: "meta" }, `Showing ${shown.length} type${shown.length === 1 ? "" : "s"} · ${n} item${n === 1 ? "" : "s"}`),
      shown.length
        ? el("table", { class: "inventory" },
            el("tr", {}, ["item", "ends", "you own", "available"].map((h) => el("th", {}, h))),
            shown.map((r) => {
              const free = r.items.filter((it) => it.status === "free").length;
              return el("tr", {},
                el("td", {}, el("a", { href: "#part/" + encodeURIComponent(r.key) }, r.name),
                  el("span", { class: "sub" }, r.kind, " · ", r.categories.join(", "))),
                el("td", {}, el("pre", { class: "mini", title: r.ends.join(" ↔ ") }, r.outline)),
                el("td", {}, r.items.length),
                el("td", { class: free ? "can" : "cant", title: `${free} of ${r.items.length} available` }, free ? "yes" : "no"));
            }))
        : el("p", {}, "Nothing matches."));
  }
  draw();
  const total = rows.reduce((sum, r) => sum + r.items.length, 0);
  return [
    el("h2", {}, "Inventory"),
    el("p", { class: "meta" }, `${total} items of ${rows.length} types.`),
    fill(bar, filterButton, searchBox, panel),
    list,
  ];
}

async function showConnector(id) {
  const d = await api("/api/connector?id=" + encodeURIComponent(id));
  return [
    el("h2", {}, d.connector.name),
    el("p", { class: "meta" }, d.connector.id, d.connector.aliases ? " · also: " + d.connector.aliases.join(", ") : ""),
    el("pre", {}, d.glyph),
    el("h3", {}, "Cables and adapters with this end"),
    el("ul", {}, d.parts.map((p) => el("li", {},
      el("a", { href: "#part/" + encodeURIComponent(p.key) }, p.name), " ",
      p.owned ? el("span", { class: "meta" }, `· you own ${p.owned}`) : null))),
  ];
}

async function showDevice(name) {
  const d = await api("/api/device?name=" + encodeURIComponent(name));
  const sig = (ids) => ids.map((s) => d.signal_names[s] || s).join(", ");
  return [
    el("h2", {}, d.nickname || d.name),
    el("p", { class: "meta" }, d.name, " · ", d.model),
    d.assumption ? el("p", { class: "note" }, "Ports not confirmed: assumes ", d.assumption, ".") : null,
    d.hint ? el("p", { class: "note" }, d.hint) : null,
    d.ports.length ? null : el("p", {}, "No cable ports."),
    d.ports.map((p) => [
      el("h3", {}, p.label, p.qty > 1 ? ` ×${p.qty}` : "", " ", el("span", { class: "meta" }, `${p.end} · ${p.dir}`)),
      el("p", { class: "meta" }, "Does: ", sig(p.signals)),
      p.have.length
        ? el("ul", {}, p.have.map((o) => el("li", { class: "can" }, "✓ ",
            el("a", { href: "#part/" + encodeURIComponent(o.part.key) }, o.part.name),
            el("span", { class: "meta" }, ` ×${o.items.length} · ${o.free} available${o.dedicated ? " · belongs to this device" : ""}`))))
        : el("p", { class: "meta" }, "Nothing you own fits."),
      p.cant_yet.map((s) => el("p", { class: "cant" }, "Can't yet: ", sig(s.signals), " → ",
        el("a", { href: "#part/" + encodeURIComponent(s.part.key) }, s.part.name))),
      p.wrong_voltage.map((o) => el("p", { class: "warn" }, `⚠ Don't use ${o.part.name}: it's ${o.part.volts} V, this port needs ${p.volts} V`)),
      p.unknown_voltage.map((o) => el("p", { class: "warn" }, `? ${o.part.name}: voltage unknown; check the label (needs ${p.volts} V)`)),
    ]),
  ];
}

// --- systems ---------------------------------------------------------------

// linkEditor picks cables for a system, one row per cable: device · port →
// cable (yours first, then common types, uncommon on request) and which of
// yours → device · port. The far-side ports are those the cable fits.
function linkEditor(b, { rows: startRows = 1 } = {}) {
  const editor = el("div", { class: "links" });
  let showUncommon = false;
  const allFits = [...new Set(b.devices.flatMap((d) => d.ports.flatMap((p) => p.fits)))];
  const device = (name) => b.devices.find((d) => d.name === name);
  const port = (dev, label) => (device(dev)?.ports || []).find((p) => p.label === label);

  function row() {
    const r = el("div", { class: "link-row" });
    const fromDev = el("select", {}), fromPort = el("select", {}), cable = el("select", {}), item = el("select", {});
    const toDev = el("select", {}), toPort = el("select", {});
    const options = (sel, opts, keep) => {
      const old = keep ? sel.value : "";
      fill(sel, opts);
      if ([...sel.options].some((o) => o.value === old)) sel.value = old;
    };
    const devOptions = () => [el("option", { value: "" }, "device"), b.devices.map((d) => el("option", { value: d.name }, d.name))];
    const portOptions = (dev, key) => {
      const ports = (device(dev)?.ports || []).filter((p) => !key || p.fits.includes(key));
      return [el("option", { value: "" }, ports.length ? "port" : dev ? "no port fits" : "port"),
        ports.map((p) => el("option", { value: p.label }, `${p.label} · ${p.end}`))];
    };
    function update() {
      options(fromPort, portOptions(fromDev.value), true);
      const keys = (port(fromDev.value, fromPort.value)?.fits || allFits).slice()
        .sort((x, y) => b.parts[x].name.localeCompare(b.parts[y].name));
      const yours = keys.filter((k) => b.parts[k].items.length);
      const common = keys.filter((k) => !b.parts[k].items.length && !b.parts[k].uncommon);
      const rare = keys.filter((k) => !b.parts[k].items.length && b.parts[k].uncommon);
      const opt = (k) => el("option", { value: k }, b.parts[k].name + (b.parts[k].items.length ? ` (${b.parts[k].available} available)` : ""));
      options(cable, [el("option", { value: "" }, "cable or adapter"),
        yours.length ? el("optgroup", { label: "yours" }, yours.map(opt)) : null,
        common.length ? el("optgroup", { label: "common" }, common.map(opt)) : null,
        showUncommon && rare.length ? el("optgroup", { label: "uncommon" }, rare.map(opt)) : null], true);
      const part = b.parts[cable.value];
      options(item, [el("option", { value: "" }, part && part.items.length ? "any of yours" : "—"),
        (part ? part.items : []).map((it) => el("option", { value: it.id }, `your #${it.short_id}${it.nickname ? " " + it.nickname : ""} · ${STATUS[it.status][0]}`))], true);
      item.disabled = !part || !part.items.length;
      options(toPort, portOptions(toDev.value, cable.value), true);
    }
    options(fromDev, devOptions());
    options(toDev, devOptions());
    for (const sel of [fromDev, fromPort, cable, item, toDev, toPort]) sel.addEventListener("change", update);
    update();
    r.value = () => cable.value && { part: cable.value, item: item.value, from_device: fromDev.value, from_port: fromPort.value, to_device: toDev.value, to_port: toPort.value };
    r.refresh = update;
    return fill(r, fromDev, fromPort, el("span", { class: "arrow" }, "→"), cable, item, el("span", { class: "arrow" }, "→"), toDev, toPort,
      el("button", { class: "quiet", title: "Remove this cable", onclick: () => r.remove() }, "×"));
  }

  for (let n = 0; n < startRows; n++) editor.append(row());
  const controls = el("div", { class: "row" },
    el("button", { class: "quiet", onclick: () => editor.append(row()) }, "+ another cable"),
    el("label", { class: "meta check" }, el("input", { type: "checkbox", onchange: (e) => {
      showUncommon = e.target.checked;
      for (const r of editor.children) r.refresh();
    } }), " show uncommon cables (SCART, VGA, FireWire-era…)"));
  const node = el("div", {}, editor, controls);
  node.value = () => [...editor.children].map((r) => r.value()).filter(Boolean);
  return node;
}

async function showSystem(name) {
  const [d, b] = await Promise.all([api("/api/system?name=" + encodeURIComponent(name)), api("/api/builder")]);
  const msg = el("div", {});
  const toggle = async (sw) => {
    const res = await api(d.up ? "/api/system/down" : "/api/system/up", { name: d.name, switch: sw });
    await refresh();
    if (res.up === d.up) {
      // Didn't come up: say why, and offer to switch.
      fill(msg,
        (res.missing || []).map((m) => el("p", { class: "warn" }, "Missing: " + m)),
        (res.blocked || []).map((x) => el("p", { class: "warn" }, x)),
        res.blocked && res.blocked.length ? el("button", { onclick: () => toggle(true) }, "Switch those off and this on") : null);
      return;
    }
    route();
  };
  const post = async (path, body) => {
    try {
      await api(path, body);
      await refresh();
      route();
    } catch (err) {
      fill(msg, el("p", { class: "warn" }, err.message));
    }
  };
  const endpoint = (dev, port) => (dev ? dev + (port ? " · " + port : "") : "?");
  const adder = linkEditor(b);
  const del = el("button", { class: "quiet danger", onclick: () => {
    // Two clicks, no dialog: the first arms it for a few seconds.
    if (del.dataset.armed) return post("/api/system/delete", { name: d.name }).then(() => (location.hash = "#systems"));
    del.dataset.armed = "1";
    fill(del, "Click again to delete");
    setTimeout(() => { delete del.dataset.armed; fill(del, "Delete system"); }, 3000);
  } }, "Delete system");
  return [
    el("a", { href: "#systems", class: "meta" }, "← systems"),
    el("h2", {}, d.name, " ", statusTag(d.up ? "up" : "down")),
    el("div", { class: "row" }, el("button", { onclick: () => toggle(false) }, d.up ? "Switch off" : "Switch on")),
    msg,
    d.links.length
      ? el("table", {},
          el("tr", {}, ["", "from → to", "cable", "item", ""].map((h) => el("th", {}, h))),
          d.links.map((l, n) => el("tr", {},
            el("td", { class: "meta" }, n + 1),
            el("td", {}, endpoint(l.from_device, l.from_port), " → ", endpoint(l.to_device, l.to_port)),
            el("td", {}, el("a", { href: "#part/" + encodeURIComponent(l.part) }, d.part_names[l.part])),
            el("td", { class: "meta" }, l.assigned ? `your #${d.short_ids[l.assigned]}, in use` : l.preferred ? `your #${d.short_ids[l.preferred]}` : "open: any available"),
            el("td", {}, d.up ? null : el("button", { class: "quiet", title: "Remove this cable", onclick: () => post("/api/system/unlink", { id: l.id }) }, "×")))))
      : el("p", { class: "meta" }, "No cables yet."),
    d.conflicts.map((c) => el("p", { class: "cant" }, `Can't be on with "${c.with}": `, c.port ? `both plug into ${c.port}` : `both need ${d.part_names[c.part] || c.part} (you own ${c.have})`)),
    d.missing.map((m) => el("p", { class: "warn" }, `Missing: needs ${m.need} × ${d.part_names[m.part] || m.part}, you own ${m.owned}`)),
    d.up ? null : el("details", { class: "box" }, el("summary", {}, "Add a cable"),
      adder,
      el("button", { onclick: async () => {
        for (const l of adder.value()) await post("/api/system/link", { system: d.name, ...l });
      } }, "Add")),
    el("div", { class: "row" }, del),
  ];
}

async function showSystems() {
  const b = await api("/api/builder");
  const systems = index.entries.filter((e) => e.kind === "system");
  const msg = el("div", {});
  const name = el("input", { type: "text", class: "inline", placeholder: "e.g. Retro corner" });
  const chain = el("div", { class: "row" });
  const devSelect = (value) => el("select", {}, el("option", { value: "" }, "device"),
    b.devices.filter((d) => d.model).map((d) => el("option", { value: d.name, selected: d.name === value }, d.name)));
  chain.append(devSelect(), el("span", { class: "arrow" }, "→"), devSelect());
  const addDevice = el("button", { class: "quiet", onclick: () => addDevice.before(el("span", { class: "arrow" }, "→"), devSelect()) }, "+ device");
  chain.append(addDevice);
  const editor = linkEditor(b);
  const details = el("details", { class: "box" },
    el("summary", {}, "Details: choose the cables and ports yourself"),
    el("p", { class: "meta" }, "Hidden, the cables are left open: the system is planned for you, and any available cable of the right type is used when it's switched on."),
    editor);
  const create = async () => {
    const links = details.open ? editor.value() : [];
    const devices = [...chain.querySelectorAll("select")].map((s) => s.value).filter(Boolean);
    try {
      const res = await api("/api/system/new", { name: name.value, devices, links });
      await refresh();
      location.hash = "#system/" + encodeURIComponent(res.name);
    } catch (err) {
      fill(msg, el("p", { class: "warn" }, err.message));
    }
  };
  const toggle = async (s) => {
    await api(s.status === "up" ? "/api/system/down" : "/api/system/up", { name: s.title });
    await refresh();
    route();
  };
  return [
    el("h2", {}, "Systems"),
    el("p", { class: "meta" }, "A system is a setup you switch on and off. Its cables are in use while it's on."),
    systems.length
      ? el("table", {},
          el("tr", {}, ["", "system", "", ""].map((h) => el("th", {}, h))),
          systems.map((s) => el("tr", {},
            el("td", {}, statusTag(s.status)),
            el("td", {}, el("a", { href: "#system/" + encodeURIComponent(s.title) }, s.title)),
            el("td", { class: "meta" }, s.sub),
            el("td", {}, el("button", { class: "quiet", onclick: () => toggle(s) }, s.status === "up" ? "Switch off" : "Switch on")))))
      : el("p", { class: "meta" }, "No systems yet."),
    el("h3", {}, "New system"),
    el("label", { class: "field" }, "name", name),
    el("div", { class: "field" }, el("span", { class: "meta" }, "devices, in order"), chain),
    details,
    msg,
    el("div", { class: "row" }, el("button", { onclick: create }, "Create system")),
  ];
}

async function showConnect() {
  const out = el("div", {});
  const pick = () => el("select", {}, index.devices.map((d) => el("option", {}, d)));
  const from = pick(), to = pick();
  if (index.devices.length > 1) to.selectedIndex = 1;
  const go = async () => {
    fill(out, el("p", { class: "meta" }, "…"));
    const d = await api(`/api/connect?from=${encodeURIComponent(from.value)}&to=${encodeURIComponent(to.value)}`);
    fill(out, connectResult(d));
  };
  from.addEventListener("change", () => fill(out));
  to.addEventListener("change", () => fill(out));
  return [
    el("h2", {}, "Connect"),
    el("p", { class: "meta" }, "From one device's outputs to the other's inputs: your available cables first, then ones in use, then what to buy."),
    el("div", { class: "row" }, from, el("span", {}, "→"), to, el("button", { onclick: go }, "Find a way")),
    out,
  ];
}

function connectResult(d) {
  const kinds = ["video", "audio", "data"].filter((k) => d.routes[k]);
  if (!kinds.length) {
    return el("p", { class: "summary" }, `No way to connect ${d.from} to ${d.to} within 3 cables and adapters, even buying parts.`);
  }
  const buy = d.to_buy || [];
  const summary = el("p", { class: "summary" },
    `${d.from} → ${d.to}: `, kinds.join(", "), ". ",
    buy.length ? el("span", { class: "cant" }, `Needs ${buy.length} purchase${buy.length > 1 ? "s" : ""}.`) : el("span", { class: "can" }, "Everything needed is yours."));

  const shown = [];
  const sections = kinds.map((k) => {
    const r = d.routes[k];
    const head = el("h3", { class: "kind-head" }, k.toUpperCase(), " ", el("span", { class: "meta" }, d.signal_names[r.signal]));
    const same = shown.find((s) => sameSteps(s.route, r));
    if (same) return el("section", { class: "route" }, head, el("p", { class: "meta" }, `Same cables as ${same.kind}.`));
    shown.push({ kind: k, route: r });
    return el("section", { class: "route" }, head,
      el("ol", { class: "chain" },
        el("li", { class: "end" }, el("strong", {}, d.from), el("span", { class: "meta" }, " · " + r.from_port)),
        r.steps.map((s) => el("li", { class: s.buy ? "step need" : "step" },
          el("a", { href: "#part/" + encodeURIComponent(s.part.key) }, s.part.name), " ",
          s.buy ? el("span", { class: "tag need", title: "You don't own one" }, "need")
            : [idTag(s.item), " ", statusTag(s.busy_in ? "in use" : "free"), s.busy_in ? el("span", { class: "meta" }, ` by ${s.busy_in}`) : null],
          d.outlines[s.part.key] ? el("details", { open: true }, el("summary", { class: "meta" }, "outline"), el("pre", {}, d.outlines[s.part.key])) : null)),
        el("li", { class: "end" }, el("strong", {}, d.to), el("span", { class: "meta" }, " · " + r.to_port))));
  });

  const shop = buy.length
    ? el("div", { class: "shop" },
        el("p", {}, "To buy:"),
        el("ul", {}, buy.map((p) => el("li", {}, "★ ", el("a", { href: "#part/" + encodeURIComponent(p.key) }, p.name)))),
        wishButton(buy, `${d.from} → ${d.to}`))
    : null;
  return [summary, sections, shop];
}

const sameSteps = (a, b) => a.from_port === b.from_port && a.to_port === b.to_port &&
  a.steps.length === b.steps.length && a.steps.every((s, i) => s.part.key === b.steps[i].part.key);

function wishButton(parts, note) {
  const button = el("button", {
    onclick: async () => {
      const res = await api("/api/wish/add", { parts: parts.map((p) => p.key), note });
      fill(button.parentNode.querySelector(".wished") || button.parentNode.appendChild(el("p", { class: "meta wished" })),
        res.added.length ? `Added ${res.added.length} to your wishlist.` : "Already on your wishlist.");
      button.disabled = true;
    },
  }, "Add to wishlist");
  return button;
}

async function showWish() {
  const wishes = await api("/api/wish");
  const results = el("ul", { class: "wish-results" });
  const add = async (key) => {
    await api("/api/wish/add", { parts: [key], note: "" });
    route();
  };
  const searchBox = el("input", { type: "search", class: "inline", placeholder: "find a cable or adapter to add",
    oninput: (e) => {
      const hits = lookup(e.target.value).map((i) => index.entries[i]).filter((x) => ["cable", "adapter", "type"].includes(x.kind)).slice(0, 8);
      fill(results, hits.map((x) => el("li", {},
        el("button", { class: "quiet", onclick: () => add(x.id.slice(5)) }, "+ add"), " ", x.title, " ",
        el("span", { class: "meta" }, x.kind === "type" ? "" : "· you own some"))));
    } });
  const done = async (id) => {
    await api("/api/wish/done", { id });
    await refresh();
    route();
  };
  return [
    el("h2", {}, "Wishlist"),
    el("div", { class: "toolbar" }, searchBox),
    results,
    wishes.length
      ? el("table", { class: "wishlist" },
          el("tr", {}, ["acquired", "item", "qty", "use case"].map((h) => el("th", {}, h))),
          wishes.map((w) => el("tr", { class: w.done ? "done" : "" },
            el("td", {}, el("input", { type: "checkbox", checked: w.done, disabled: w.done,
              title: w.done ? "Acquired: it's in your inventory" : "Tick when you have it: it moves into your inventory",
              onchange: () => done(w.id) })),
            el("td", {}, el("a", { href: "#part/" + encodeURIComponent(w.part) }, w.name)),
            el("td", {}, w.qty),
            el("td", { class: "meta" }, w.use_case))))
      : el("p", { class: "meta" }, "Empty. Search above, or connect two devices to see what you're missing."),
  ];
}

// --- footer: the cable meter and page size, as Low-tech Magazine shows its
// battery and page weight --------------------------------------------------

function footer(bytes) {
  const c = index.counts;
  const total = (c.cable || 0) + (c.adapter || 0);
  const used = c["in use"] || 0;
  const cells = 24;
  const n = total ? Math.round((used / total) * cells) : 0;
  fill($("#meter"),
    "Cables in use ",
    el("span", { class: "cells" }, el("span", { class: "used" }, "▮".repeat(n)), "▯".repeat(cells - n)),
    ` ${used} of ${total} · ${c.devices || 0} devices`);
  $("#stats").textContent = `Page size ${(bytes / 1024).toFixed(1)} KiB · catalog ${index.catalog} · index served in ${index.server_ms} ms, built in ${index.ms.toFixed(1)} ms`;
}

function pageBytes() {
  return performance.getEntriesByType("navigation").concat(performance.getEntriesByType("resource"))
    .reduce((sum, e) => sum + (e.transferSize || e.encodedBodySize || 0), 0);
}

async function refresh() {
  index = buildIndex(await api("/api/index"));
  search();
  footer(pageBytes());
}

// --- start -----------------------------------------------------------------

document.addEventListener("keydown", (e) => {
  const typing = document.activeElement === $("#q");
  if (e.key === "/" && !typing && !(document.activeElement instanceof HTMLSelectElement)) {
    e.preventDefault();
    location.hash = "";
    $("#q").focus();
  } else if (e.key === "Escape") {
    if (location.hash) location.hash = "";
    else { $("#q").value = ""; search(); }
  } else if (typing && e.key === "ArrowDown") { e.preventDefault(); move(1); }
  else if (typing && e.key === "ArrowUp") { e.preventDefault(); move(-1); }
  else if (typing && e.key === "Enter") {
    const li = $("#results").children[selected];
    if (li) li.click();
  }
});
$("#q").addEventListener("input", search);
window.addEventListener("hashchange", route);

refresh().then(route);
