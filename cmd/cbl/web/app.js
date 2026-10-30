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

// searchField dresses a page's own search like the header's: a magnifying
// glass, then a fine, light input.
const searchField = (input) => el("label", { class: "searchbox" }, $(".searchbox svg").cloneNode(true), input);

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

// search fills the dropdown under the header's search box. It's on every
// page; picking a result opens that thing's page.
function search() {
  const q = $("#q").value;
  const t0 = performance.now();
  const hits = lookup(q);
  const ms = performance.now() - t0;
  selected = 0;
  $("#dropdown").hidden = !q.trim();
  $("#timing").textContent = `${hits.length} result${hits.length === 1 ? "" : "s"} in ${ms.toFixed(3)} ms · 1 hashmap lookup of ${index.keys.toLocaleString()} prefixes`;
  const list = $("#results");
  fill(list,
    ...hits.slice(0, 60).map((i, n) => {
      const e = index.entries[i];
      // mousedown, not click: it fires before the box loses focus and closes.
      return el("li", { "aria-selected": n === 0 ? "true" : "false", onmousedown: (ev) => { ev.preventDefault(); openEntry(e.id); } },
        el("span", { class: "kind" }, KIND_LABEL[e.kind]),
        el("span", { class: "title" }, e.title), " ",
        e.status ? statusTag(e.status) : null,
        el("span", { class: "sub" }, e.sub));
    }),
  );
  if (hits.length > 60) list.append(el("li", { class: "meta" }, `… and ${hits.length - 60} more; keep typing`));
  if (!hits.length) list.append(el("li", { class: "meta" }, "Nothing called that."));
}

function closeSearch() {
  $("#q").value = "";
  $("#dropdown").hidden = true;
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
  closeSearch();
  $("#q").blur();
  location.hash = "#" + id.slice(0, i) + "/" + encodeURIComponent(id.slice(i + 1));
}

// --- where things are: sections, their sub-pages, and the trail -----------

const SECTIONS = {
  route: { label: "Connect" },
  network: { label: "Your Network", subs: [["systems", "Systems"], ["inventory", "Inventory"], ["devices", "Devices"], ["wish", "Wishlist"]] },
  explore: { label: "Explore", subs: [["cables", "Cables"], ["adapters", "Adapters"], ["devices", "Devices"]] },
};
// Addresses from before the sections, kept working.
const MOVED = { systems: "network/systems", inventory: "network/inventory", wish: "network/wish", connect: "route",
  network: "network/inventory", explore: "explore/cables", home: "route" };

// trail draws the sub-navigation for a section and the path above the page:
// Home / Your network / Systems / Pi desk. Detail pages call it again once
// they know where they belong.
function trail(section, sub, leaf) {
  // Systems has its own tab; the rest of Your Network shares one.
  const tab = section === "network" && sub === "systems" ? "systems" : section;
  for (const a of document.querySelectorAll("nav a")) a.toggleAttribute("aria-current", a.dataset.tab === tab);
  const s = SECTIONS[section];
  const subnav = $("#subnav");
  subnav.hidden = !s?.subs;
  fill(subnav, (s?.subs || []).map(([id, label]) =>
    el("a", { href: `#${section}/${id}`, "aria-current": id === sub && !leaf ? "page" : null, class: id === sub ? "here" : null }, label)));
  const steps = [["Home", "#route"]];
  if (s) steps.push([s.label, s.subs ? `#${section}/${s.subs[0][0]}` : "#" + section]);
  const subLabel = s?.subs?.find(([id]) => id === sub)?.[1];
  if (subLabel) steps.push([subLabel, `#${section}/${sub}`]);
  if (leaf) steps.push([leaf, null]);
  // Connect is the home page: no trail above it.
  fill($("#crumbs"), section === "route" ? null : steps.map(([label, href], i) =>
    el("li", {}, href && i < steps.length - 1 ? el("a", { href }, label) : el("span", { "aria-current": "page" }, label))));
}

async function route() {
  const hash = decodeURIComponent(location.hash.slice(1));
  if (MOVED[hash]) return location.replace("#" + MOVED[hash]);
  const [kind = "route", ...rest] = (hash || "route").split("/"); // the site opens on Connect
  const arg = rest.join("/");
  const owned = (name) => index.devices.includes(name);
  // Each address: what draws it, and where it sits.
  const pages = {
    route: [showRoute, "route"],
    network: [{ systems: showSystems, inventory: showInventory, devices: showNetworkDevices, wish: showWish }[arg], "network", arg],
    explore: [{ cables: () => showExplore("cable"), adapters: () => showExplore("adapter"), devices: showExploreDevices }[arg], "explore", arg],
    system: [showSystem, "network", "systems", arg],
    device: [showDevice, owned(arg) ? "network" : "explore", "devices", arg],
    model: [showDevice, "explore", "devices", arg],
    part: [showPart, "explore", "cables", arg],
    connector: [showConnector, "explore", "cables", arg],
  };
  const [render, section, sub, leaf] = pages[kind] || [];
  trail(section, sub, leaf);
  const view = $("#view");
  if (!render) {
    fill(view, el("p", { class: "meta" }, "No page here. ", el("a", { href: "#route" }, "Go home")));
    return;
  }
  fill(view, el("p", { class: "meta" }, "…"));
  try {
    fill(view, await render(arg));
    window.scrollTo(0, 0);
  } catch (err) {
    fill(view, el("p", { class: "warn" }, err.message));
  }
}

// --- detail views ----------------------------------------------------------

async function showPart(key) {
  const d = await api("/api/part?key=" + encodeURIComponent(key));
  const p = d.part;
  trail("explore", p.kind === "adapter" ? "adapters" : "cables", p.name);
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

// --- explore: every cable and adapter in the catalog, filed by port type ----

async function showExplore(kind) {
  // Only this kind's parts, and the connectors that still have some.
  const families = (await api("/api/explore")).map((f) => ({ ...f, connectors: f.connectors
    .map((c) => ({ ...c, parts: c.parts.filter((p) => p.kind === kind) })).filter((c) => c.parts.length) }))
    .filter((f) => f.connectors.length);
  const list = el("div", {});
  const input = el("input", { type: "search", placeholder: "filter, e.g. usb", oninput: () => draw() });
  const total = new Set(families.flatMap((f) => f.connectors.flatMap((c) => c.parts.map((p) => p.key)))).size;
  function draw() {
    const q = input.value.trim().toLowerCase();
    const has = (text) => text.toLowerCase().includes(q);
    const shown = families.map((f) => {
      // A family or connector that matches shows everything under it.
      if (!q || has(f.family)) return f;
      const connectors = f.connectors.map((c) => has(c.name) || has(c.id) ? c : { ...c, parts: c.parts.filter((p) => has(p.name)) })
        .filter((c) => c.parts.length);
      return { ...f, connectors };
    }).filter((f) => f.connectors.length);
    fill(list, shown.length ? shown.map((f) => el("section", { class: "family" },
      el("h3", { class: "kind-head" }, f.family.toUpperCase()),
      f.connectors.map((c) => el("details", { class: "connector", open: !!q },
        el("summary", {}, el("a", { href: "#connector/" + encodeURIComponent(c.id) }, c.name), " ",
          el("span", { class: "meta" }, `${c.parts.length}`)),
        el("ul", {}, c.parts.map((p) => el("li", {},
          el("a", { href: "#part/" + encodeURIComponent(p.key) }, p.name), " ",
          el("span", { class: "meta" }, p.owned ? `you own ${p.owned}` : "")))))))) : el("p", {}, "Nothing matches."));
  }
  draw();
  return [
    el("h2", {}, kind === "adapter" ? "Adapters" : "Cables"),
    el("p", { class: "meta" }, `Catalogue of ${total} common ${kind} types, by port type. Open a connector to see its details.`),
    el("div", { class: "toolbar catalogue" }, searchField(input)),
    list,
  ];
}

// --- inventory: everything you own, filtered in the page ------------------

// --- inventory: everything you own, filtered and searched in the page -----

// abbreviate shortens a part name for the inventory's narrow column; the
// full name shows above it on hover. "USB-A 3.0 to Micro-USB (charge/data)"
// → "USB-A 3.0 → Micro-USB".
function abbreviate(name) {
  const short = name.replace(/\s*\(.*?\)\s*/g, " ").replace(/ to /g, " → ").replace(/\bcable\b/gi, "").replace(/\s+/g, " ").trim();
  return short.length > 26 ? short.slice(0, 25).trimEnd() + "…" : short;
}

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
  // Clicking the trigger opens and closes the panel.
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
  const searchBox = searchField(el("input", { type: "search", placeholder: "search your inventory", value: invFilter.query,
    oninput: (e) => { invFilter.query = e.target.value; draw(); } }));

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
            el("colgroup", {}, ["ends", "item", "count", "free"].map((c) => el("col", { class: c }))),
            el("tr", {}, ["ends", "item", "count", "available"].map((h) => el("th", {}, h))),
            shown.map((r) => {
              const free = r.items.filter((it) => it.status === "free").length;
              return el("tr", {},
                el("td", {}, el("pre", { class: "mini", title: r.ends.join(" ↔ ") }, r.outline)),
                el("td", {}, el("a", { href: "#part/" + encodeURIComponent(r.key), class: "abbr", "data-full": r.name, "aria-label": r.name }, abbreviate(r.name)),
                  el("span", { class: "sub" }, r.kind, " · ", r.categories.join(", "))),
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
    addNewForm(),
  ];
}

async function showNetworkDevices() {
  return [
    el("h2", {}, "Devices"),
    el("p", { class: "meta" }, "The devices you own, with make, model and owner. Open one for its ports and what you can plug into it."),
    await yourDevices(),
    addNewForm("device"),
  ];
}

// showExploreDevices is every device model in the catalog, by category:
// named models with their make, and generic types for when you don't know.
async function showExploreDevices() {
  const d = await api("/api/devices");
  const input = el("input", { type: "search", placeholder: "filter, e.g. mouse", oninput: () => draw() });
  const list = el("div", {});
  const categories = [...new Set(d.models.map((m) => m.category || "other"))].sort();
  function draw() {
    const q = input.value.trim().toLowerCase();
    const has = (m) => !q || [m.name, m.make, m.category].some((t) => (t || "").toLowerCase().includes(q));
    const shown = categories.map((c) => [c, d.models.filter((m) => (m.category || "other") === c && has(m))]).filter(([, ms]) => ms.length);
    fill(list, shown.length ? shown.map(([c, ms]) => el("section", { class: "family" },
      el("h3", { class: "kind-head" }, c.toUpperCase()),
      el("ul", { class: "models" }, ms.map((m) => el("li", {},
        el("a", { href: "#model/" + encodeURIComponent(m.id) }, m.name), " ",
        el("span", { class: "meta" }, m.generic ? "generic type" : m.make || "")))))) : el("p", {}, "Nothing matches."));
  }
  draw();
  return [
    el("h2", {}, "Devices"),
    el("p", { class: "meta" }, `Catalogue of ${d.models.length} common devices, by category. Open one to see its ports; generic types stand in when you don't know the model.`),
    el("div", { class: "toolbar catalogue" }, searchField(input)),
    list,
  ];
}

// yourDevices lists the devices you own with make, model and owner, and adds
// one from the catalog: a named model, or a generic type when you don't know.
async function yourDevices() {
  const d = await api("/api/devices");
  const pick = el("select", {}, el("option", { value: "" }, "pick a model"),
    el("optgroup", { label: "models" }, d.models.filter((m) => !m.generic).map((m) => el("option", { value: m.id }, m.name))),
    el("optgroup", { label: "generic: when you don't know the model" }, d.models.filter((m) => m.generic).map((m) => el("option", { value: m.id }, m.name))));
  const nickname = el("input", { type: "text", class: "inline", placeholder: "what you call it (optional)" });
  const owner = el("input", { type: "text", class: "inline", placeholder: "owner (optional)" });
  const msg = el("div", {});
  const add = async () => {
    try {
      const res = await api("/api/device/add", { model: pick.value, nickname: nickname.value, owner: owner.value });
      await refresh();
      location.hash = "#device/" + encodeURIComponent(res.name);
    } catch (err) {
      fill(msg, el("p", { class: "warn" }, err.message));
    }
  };
  const sorted = d.devices.toSorted((a, b) => a.name.localeCompare(b.name));
  return el("section", { class: "your-devices" },
    el("table", { class: "devices" },
      el("tr", {}, ["name", "make", "model", "owner"].map((h) => el("th", {}, h))),
      sorted.map((x) => el("tr", {},
        el("td", {}, el("a", { href: "#device/" + encodeURIComponent(x.name) }, x.name)),
        el("td", {}, x.model.make || el("span", { class: "meta" }, x.model.generic ? "generic" : "—")),
        el("td", { class: "meta" }, x.model.name || "no model yet"),
        el("td", {}, x.owner || el("span", { class: "meta" }, "—"))))),
    el("details", { class: "box" }, el("summary", {}, "+ Add a device you own"),
      el("div", { class: "row" }, pick, nickname, owner, el("button", { onclick: () => pick.value && add() }, "Add device")),
      el("p", { class: "meta" }, "Not listed? Add it as something new below, with its ports or as a typical one of its kind."),
      msg));
}

// addNewForm adds a cable, adapter or device the catalog doesn't have, with
// its ends or ports. It becomes your own catalog entry, so routes and
// systems can use it like any other. Connectors and signals load on open.
function addNewForm(start = "cable") {
  const box = el("details", { class: "box add-new" },
    el("summary", {}, start === "device"
      ? "+ Add a new device: a model the catalog doesn't have, like a Logitech mouse"
      : "+ Add something new: a cable, adapter or device the catalog doesn't have"));
  box.addEventListener("toggle", async () => {
    if (!box.open || box.dataset.ready) return;
    box.dataset.ready = "1";
    const kit = await api("/api/parts-kit");
    box.append(...[addNewBody(kit, start)].flat());
  });
  return box;
}

const GENDERS = [["m", "plug"], ["f", "socket"]];

function addNewBody(kit, start) {
  const option = ([value, label], selected) => el("option", { value, selected: value === selected }, label);
  const connectorSelect = () => el("select", { class: "connector" }, el("option", { value: "" }, "connector"),
    kit.connectors.map((c) => el("option", { value: c.id }, c.name)));
  const genderSelect = (g) => el("select", {}, GENDERS.map((o) => option(o, g)));
  const text = (placeholder, attrs = {}) => el("input", { type: "text", class: "inline", placeholder, ...attrs });
  const number = (value, attrs = {}) => el("input", { type: "number", class: "inline num", value, min: 1, ...attrs });

  // A drop-down of checkboxes for what an end or port carries.
  function signalPicker() {
    const boxes = kit.signals.map((sg) => el("input", { type: "checkbox", value: sg.id }));
    const summary = el("summary", {}, "carries…");
    const label = () => {
      const on = kit.signals.filter((_, i) => boxes[i].checked).map((sg) => sg.name.replace(/ \(.*\)$/, ""));
      fill(summary, on.length ? on.join(", ") : "carries…");
    };
    const kinds = [...new Set(kit.signals.map((sg) => sg.kind))];
    const picker = el("details", { class: "signals" }, summary,
      el("div", { class: "signal-grid" }, kinds.map((k) => el("div", {},
        el("span", { class: "kind-head meta" }, k.toUpperCase()),
        kit.signals.map((sg, i) => sg.kind === k ? el("label", { class: "check" }, boxes[i], " ", sg.name) : null)))));
    picker.addEventListener("change", label);
    picker.value = () => boxes.filter((b) => b.checked).map((b) => b.value);
    return picker;
  }

  // Cable and adapter ends: connector, how many plugs (3 for RCA), gender.
  const ends = el("div", { class: "ends" });
  const endRow = () => {
    const conn = connectorSelect(), count = number(1, { title: "how many plugs on this end, e.g. 3 for red/white/yellow RCA" }), g = genderSelect("m");
    const r = el("div", { class: "link-row" }, conn, el("span", { class: "meta" }, "×"), count, g,
      el("button", { class: "quiet", title: "Remove this end", onclick: () => r.remove() }, "×"));
    r.value = () => conn.value && `${conn.value}${count.value > 1 ? "*" + count.value : ""}:${g.value}`;
    return r;
  };
  ends.append(endRow(), endRow());
  const partSignals = signalPicker();
  const variant = text("only if the catalog has these ends, e.g. 12v");
  const volts = number("", { min: 0, step: "any", placeholder: "V" }), amps = number("", { min: 0, step: "any", placeholder: "A" });
  const qty = number(1), where = text("e.g. drawer 2");

  // Device ports: a label, the socket, which way signals go, what it carries.
  const ports = el("div", { class: "ports" });
  const portRow = () => {
    const label = text("label, e.g. HDMI out", { class: "inline port-label" }), conn = connectorSelect(), g = genderSelect("f");
    const dir = el("select", {}, [["out", "out"], ["in", "in"], ["both", "both ways"]].map((o) => option(o, "out")));
    const n = number(1, { title: "how many identical ports" }), carries = signalPicker();
    const r = el("div", { class: "link-row" }, label, conn, g, dir, el("span", { class: "meta" }, "×"), n, carries,
      el("button", { class: "quiet", title: "Remove this port", onclick: () => r.remove() }, "×"));
    r.value = () => conn.value && { label: label.value.trim() || conn.selectedOptions[0].text, end: `${conn.value}:${g.value}`,
      qty: Number(n.value) || 1, dir: dir.value, signals: carries.value() };
    return r;
  };
  ports.append(portRow());

  const kind = el("select", {}, [["cable", "cable"], ["adapter", "adapter"], ["device", "device"]].map((o) => option(o, start)));
  const name = text("e.g. Grey sync cable");
  const nickname = text("what you call it (optional)");
  const partFields = el("div", {},
    el("div", { class: "field" }, el("span", { class: "meta" }, "ends"), ends,
      el("div", { class: "row" }, el("button", { class: "quiet", onclick: () => ends.append(endRow()) }, "+ another end"))),
    el("div", { class: "field" }, el("span", { class: "meta" }, "carries"), partSignals),
    el("div", { class: "row" },
      el("label", { class: "field" }, "variant", variant),
      el("label", { class: "field" }, "volts", volts),
      el("label", { class: "field" }, "amps", amps)),
    el("div", { class: "row" },
      el("label", { class: "field" }, "how many", qty),
      el("label", { class: "field" }, "where it's kept", where)));
  // A device's ports: list them, or say it's a typical one of a kind and
  // take that type's ports as an assumption.
  const make = text("e.g. Logitech"), owner = text("whose it is (optional)"), wireless = text("e.g. Bluetooth (optional)");
  const generic = el("select", {}, el("option", { value: "" }, "I'll list its ports"),
    el("optgroup", { label: "I don't know: assume a typical…" }, kit.generics.map((g) => el("option", { value: g.id }, g.name))));
  const portEditor = el("div", { class: "field" }, el("span", { class: "meta" }, "ports"), ports,
    el("div", { class: "row" }, el("button", { class: "quiet", onclick: () => ports.append(portRow()) }, "+ another port")));
  generic.addEventListener("change", () => { portEditor.hidden = !!generic.value; });
  const deviceFields = el("div", {},
    el("div", { class: "row" },
      el("label", { class: "field grow" }, "make", make),
      el("label", { class: "field grow" }, "owner", owner),
      el("label", { class: "field grow" }, "connects wirelessly by", wireless)),
    el("label", { class: "field" }, "ports", generic),
    portEditor);
  const showKind = () => {
    partFields.hidden = kind.value === "device";
    deviceFields.hidden = kind.value !== "device";
  };
  kind.addEventListener("change", showKind);
  showKind();

  const msg = el("div", {});
  const add = async () => {
    const body = kind.value === "device"
      ? { kind: "device", name: name.value, nickname: nickname.value, make: make.value, owner: owner.value, wireless: wireless.value,
          generic: generic.value, ports: generic.value ? [] : [...ports.children].map((r) => r.value()).filter(Boolean) }
      : { kind: kind.value, name: name.value, nickname: nickname.value, ends: [...ends.children].map((r) => r.value()).filter(Boolean),
          signals: partSignals.value(), variant: variant.value.trim(), volts: Number(volts.value) || 0, amps: Number(amps.value) || 0,
          qty: Number(qty.value) || 1, location: where.value };
    try {
      const res = await api("/api/custom/add", body);
      await refresh();
      await route();
      // route() redrew the page: report on the fresh form.
      const fresh = document.querySelector(".add-new");
      if (fresh) fresh.after(el("p", { class: "can" }, res.kind === "device"
        ? [`Added your ${res.name}. `, el("a", { href: "#device/" + encodeURIComponent(res.name) }, "See its ports")]
        : [`Added ${res.added} × ${res.name}. `, el("a", { href: "#part/" + encodeURIComponent(res.key) }, "See it")]));
    } catch (err) {
      fill(msg, el("p", { class: "warn" }, err.message));
    }
  };

  return [
    el("div", { class: "row" },
      el("label", { class: "field" }, "it's a", kind),
      el("label", { class: "field grow" }, "name", name),
      el("label", { class: "field grow" }, "nickname", nickname)),
    partFields,
    deviceFields,
    msg,
    el("div", { class: "row" }, el("button", { onclick: add }, "Add to inventory")),
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
  const owner = d.nickname ? el("input", { type: "text", class: "inline", value: d.owner || "", placeholder: "whose it is",
    onchange: async () => { await api("/api/device/owner", { name: d.nickname, owner: owner.value }); } }) : null;
  return [
    el("h2", {}, d.nickname || d.name),
    el("p", { class: "meta" }, [d.make, d.name, d.category, d.generic ? "generic type" : null].filter(Boolean).join(" · ")),
    d.docs ? el("p", { class: "meta" }, el("a", { href: d.docs, target: "_blank", rel: "noopener" }, "Maker's page and documentation ↗")) : null,
    d.wireless ? el("p", {}, "Connects wirelessly via ", d.wireless, ": no cable needed for that.") : null,
    owner ? el("label", { class: "field owner" }, "owner", owner) : null,
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

let systemFlash = null; // said once on the next system page

async function showSystem(name) {
  const flash = systemFlash;
  systemFlash = null;
  const [d, b] = await Promise.all([api("/api/system?name=" + encodeURIComponent(name)), api("/api/builder")]);
  const msg = el("div", {});
  const toggle = async (sw) => {
    const res = await api(d.up ? "/api/system/down" : "/api/system/up", { name: d.name, switch: sw });
    await refresh();
    if (res.up === d.up) {
      // Didn't come up: say why, and offer to switch.
      // Offer what the plan promises: switch the others off, or buy another.
      fill(msg,
        (res.missing || []).map((m) => el("p", { class: "warn" }, "Missing: " + m)),
        (res.blocked || []).map((x) => el("p", { class: "warn" }, x)),
        el("div", { class: "row" },
          res.blocked && res.blocked.length ? el("button", { onclick: () => toggle(true) }, "Switch those off and this on") : null,
          res.wish && res.wish.length ? wishButton(res.wish, `so ${d.name} can be on`) : null));
      return;
    }
    route();
  };
  const post = async (path, body) => {
    try {
      const res = await api(path, body);
      // An edit to a system that was on: it went off and tried to come back.
      if (res && res.up === false) systemFlash = ["Switched off after the change: ", (res.off_because || []).join("; ")];
      await refresh();
      route();
    } catch (err) {
      fill(msg, el("p", { class: "warn" }, err.message));
    }
  };
  const endpoint = (dev, port) => (dev ? dev + (port ? " · " + port : "") : "?");
  const adder = linkEditor(b);

  // The devices in it, each with its ports and, while it's off, a way out.
  const members = [...new Set(d.links.flatMap((l) => [l.from_device, l.to_device]).filter(Boolean))];
  const dir = { in: "in", out: "out", both: "in/out" };
  const devices = members.map((name) => {
    const dev = b.devices.find((x) => x.name === name);
    return el("details", { class: "member" },
      el("summary", {}, el("a", { href: "#device/" + encodeURIComponent(name) }, name), " ",
        el("span", { class: "meta" }, dev ? `${dev.ports.length} port${dev.ports.length === 1 ? "" : "s"}` : ""), " ",
        el("button", { class: "quiet", title: `Take ${name} out, with the cables that reach it`,
          onclick: (e) => { e.preventDefault(); post("/api/system/remove-device", { system: d.name, device: name }); } }, "remove")),
      dev && dev.ports.length
        ? el("ul", {}, dev.ports.map((p) => el("li", {}, p.label, " ", el("span", { class: "meta" }, `${p.end} · ${dir[p.dir] || p.dir}`))))
        : el("p", { class: "meta" }, "No ports listed."));
  });
  const others = b.devices.filter((x) => x.model && !members.includes(x.name));
  const newDevice = el("select", {}, el("option", { value: "" }, "device"), others.map((x) => el("option", { value: x.name }, x.name)));
  const joinTo = el("select", {}, members.map((m) => el("option", { value: m }, m)));
  const addDevice = !members.length ? null : el("div", { class: "row" },
    el("span", { class: "meta" }, "Add"), newDevice, el("span", { class: "meta" }, "connected to"), joinTo,
    el("button", { onclick: () => newDevice.value && post("/api/system/add-device", { system: d.name, device: newDevice.value, to: joinTo.value }) }, "Add device"));
  const del = el("button", { class: "quiet danger", onclick: () => {
    // Two clicks, no dialog: the first arms it for a few seconds.
    if (del.dataset.armed) return post("/api/system/delete", { name: d.name }).then(() => (location.hash = "#network/systems"));
    del.dataset.armed = "1";
    fill(del, "Click again to delete");
    setTimeout(() => { delete del.dataset.armed; fill(del, "Delete system"); }, 3000);
  } }, "Delete system");
  // Switching off frees its cables: said beside the button on hover.
  const power = d.up
    ? el("div", { class: "row with-hint" }, el("button", { onclick: () => toggle(false) }, "Switch off"),
        el("span", { class: "hint meta" }, "Switching off a system will mark associated cables as available (not in use)."))
    : el("div", { class: "row" }, el("button", { onclick: () => toggle(false) }, "Switch on"));
  const act = (l) => d.activity[l.id] || {};
  const activityClass = (st) => st === "available" ? "can" : st === "in use" ? "inuse" : st && st.startsWith("its own") ? "meta" : "warn";
  return [
    el("h2", {}, d.name, " ", statusTag(d.up ? "up" : "down")),
    power,
    d.up ? el("p", { class: "meta" }, "It's on: a change switches it off and back on, picking its cables again.") : null,
    flash ? el("p", { class: "warn" }, flash) : null,
    msg,
    el("h3", {}, "Your cables in it"),
    d.links.length
      ? el("table", { class: "system-cables" },
          el("tr", {}, ["from", "to", "cable", "item #", "activity", ""].map((h) => el("th", {}, h))),
          d.links.map((l) => el("tr", {},
            el("td", {}, endpoint(l.from_device, l.from_port)),
            el("td", {}, endpoint(l.to_device, l.to_port)),
            el("td", {}, l.part ? el("a", { href: "#part/" + encodeURIComponent(l.part) }, d.part_names[l.part]) : el("span", { class: "meta" }, "plugs straight in")),
            el("td", { class: "meta" }, act(l).item ? "#" + act(l).item : ""),
            el("td", { class: activityClass(act(l).status) }, act(l).status || ""),
            el("td", {}, el("button", { class: "quiet", title: "Remove this cable", onclick: () => post("/api/system/unlink", { id: l.id }) }, "×")))))
      : el("p", { class: "meta" }, "No cables yet."),
    d.conflicts.map((c) => el("p", { class: "cant" }, `Can't be on with "${c.with}": `, c.port ? `both plug into ${c.port}` : `both need ${d.part_names[c.part] || c.part} (you own ${c.have})`)),
    d.missing.map((m) => el("p", { class: "warn" }, `Missing: needs ${m.need} × ${d.part_names[m.part] || m.part}, you own ${m.owned}`)),
    el("details", { class: "box" }, el("summary", {}, "Add a cable"),
      adder,
      el("button", { onclick: async () => {
        for (const l of adder.value()) await post("/api/system/link", { system: d.name, ...l });
      } }, "Add")),
    el("h3", {}, "Devices"),
    members.length ? devices : el("p", { class: "meta" }, "None yet: add a cable above."),
    addDevice,
    el("div", { class: "row end" }, del),
  ];
}

// demoButton does what `cbl demo` does: two example setups, a Raspberry Pi
// desk and a laptop desk, with the devices and cables they need.
function demoButton() {
  const msg = el("span", { class: "meta" });
  const button = el("button", { class: "quiet", onclick: async () => {
    const res = await api("/api/demo", {});
    await refresh();
    if (res.added.length) return route();
    button.disabled = true;
    fill(msg, "They're already there.");
  } }, "+ Load the example setups");
  return el("div", { class: "row" }, button, el("span", { class: "meta" }, "a Raspberry Pi desk and a laptop desk"), msg);
}

async function showSystems() {
  const [b, bookmarks] = await Promise.all([api("/api/builder"), api("/api/bookmarks")]);
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
    demoButton(),
    systems.length
      ? el("table", {},
          el("tr", {}, ["", "system", "", ""].map((h) => el("th", {}, h))),
          systems.map((s) => el("tr", {},
            el("td", {}, statusTag(s.status)),
            el("td", {}, el("a", { href: "#system/" + encodeURIComponent(s.title) }, s.title)),
            el("td", { class: "meta" }, s.sub),
            el("td", {}, el("button", { class: "quiet", onclick: () => toggle(s) }, s.status === "up" ? "Switch off" : "Switch on")))))
      : el("p", { class: "meta" }, "No systems yet."),
    el("h3", {}, "Bookmarks"),
    bookmarks.length
      ? el("table", {},
          el("tr", {}, ["route", "", ""].map((h) => el("th", {}, h))),
          bookmarks.map((m) => el("tr", {},
            el("td", {}, el("a", { href: routeHash(m.from, m.to) }, m.name || `${m.from} → ${m.to}`)),
            el("td", { class: "meta" }, m.name ? `${m.from} → ${m.to}` : ""),
            el("td", {}, el("button", { class: "quiet", onclick: async () => { await api("/api/bookmark/delete", { id: m.id }); route(); } }, "Remove")))))
      : el("p", { class: "meta" }, "None yet. Find a route on the Route page and bookmark it to keep it here."),
    el("h3", {}, "New system"),
    el("label", { class: "field" }, "name", name),
    el("div", { class: "field" }, el("span", { class: "meta" }, "devices, in order"), chain),
    details,
    msg,
    el("div", { class: "row" }, el("button", { onclick: create }, "Create system")),
    el("p", { class: "meta" }, "A device you own isn't in the lists? Add it here, then pick it above."),
    addNewForm("device"),
  ];
}

// A route's address: #route/From→To, so a bookmark reopens it.
const routeHash = (from, to) => "#route/" + encodeURIComponent(from + "→" + to);

async function showRoute(arg) {
  const out = el("div", {});
  const pick = () => el("select", {}, index.devices.map((d) => el("option", {}, d)));
  const from = pick(), to = pick();
  const [a, b] = arg.split("→");
  if (index.devices.includes(a) && index.devices.includes(b)) {
    from.value = a;
    to.value = b;
  } else {
    // Start on the usual job: your Mac (or computer) to a monitor.
    const first = (re) => index.devices.find((d) => re.test(d));
    from.value = first(/\bmac|computer|laptop|thinkpad|\bpc\b/i) || index.devices[0] || "";
    to.value = first(/monitor|display|screen|\btv\b/i) || index.devices.find((d) => d !== from.value) || "";
  }
  const go = async () => {
    fill(out, el("p", { class: "meta" }, "…"));
    history.replaceState(null, "", routeHash(from.value, to.value));
    const d = await api(`/api/connect?from=${encodeURIComponent(from.value)}&to=${encodeURIComponent(to.value)}`);
    fill(out, connectResult(d), systemForm(d, from.value, to.value), bookmarkForm(from.value, to.value));
  };
  from.addEventListener("change", () => fill(out));
  to.addEventListener("change", () => fill(out));
  if (from.value && from.value === a && to.value === b) go();
  return [
    el("h2", { class: "page-title" }, "Find your connection:"),
    el("p", { class: "intro" }, "Find the right cables for your devices. Select the equipment you want to connect, and we'll check your system configuration to tell you what you need or what you're missing."),
    el("div", { class: "connect-pick" }, from, el("span", { class: "long-arrow", "aria-hidden": "true" }), to),
    el("div", { class: "connect-go" }, el("button", { class: "primary", onclick: go }, "Connect")),
    out,
  ];
}

// systemForm saves the route on show as one of your systems. Cables you
// don't own yet are previewed and, if ticked, added to your inventory first
// so the system can use them; untick any you'd rather add yourself.
function systemForm(d, from, to) {
  const kinds = ["video", "audio", "data"].filter((k) => d.routes[k]);
  if (!kinds.length) return null;
  const yours = new Map();
  for (const k of kinds) for (const s of d.routes[k].steps) if (!s.buy) yours.set(s.part.key, s.part.name);
  const buy = d.to_buy || [];
  const ticks = buy.map((p) => el("input", { type: "checkbox", checked: true, value: p.key }));
  const name = el("input", { type: "text", class: "inline", value: `${from} → ${to}` });
  const msg = el("div", {});
  const save = async () => {
    try {
      // The system first: if its name is taken, nothing is added. Its links
      // are open, so the new cables are used when it's switched on.
      const res = await api("/api/system/new", { name: name.value, devices: [from, to] });
      for (const t of ticks.filter((t) => t.checked)) await api("/api/item/add", { part: t.value, qty: 1 });
      await refresh();
      location.hash = "#system/" + encodeURIComponent(res.name);
    } catch (err) {
      fill(msg, el("p", { class: "warn" }, err.message));
    }
  };
  return el("div", { class: "box save-system" },
    el("h3", {}, "Add to your systems"),
    yours.size ? el("p", { class: "meta" }, "Uses from your inventory: ", [...yours.values()].join(", "), ".") : null,
    buy.length ? [
      el("p", { class: "meta" }, "Adds to your inventory (untick any you'd rather add yourself later):"),
      el("ul", {}, buy.map((p, i) => el("li", {}, el("label", { class: "check" }, ticks[i], " ", p.name)))),
    ] : null,
    el("div", { class: "row" }, name, el("button", { onclick: save }, "Add system")),
    msg);
}

// bookmarkForm saves the route on show to Your network → Systems →
// Bookmarks. Only the two devices are kept: the cables are worked out again
// when it opens.
function bookmarkForm(from, to) {
  const name = el("input", { type: "text", class: "inline", placeholder: `name (optional), e.g. ${from} → ${to}` });
  const msg = el("span", { class: "meta" });
  const save = el("button", { onclick: async () => {
    try {
      await api("/api/bookmark/add", { name: name.value, from, to });
      save.disabled = true;
      fill(msg, "Saved to ", el("a", { href: "#network/systems" }, "Your network → Systems"), ".");
    } catch (err) {
      fill(msg, el("span", { class: "warn" }, err.message));
    }
  } }, "Bookmark");
  return el("div", { class: "row bookmark" }, name, save, msg);
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
        r.steps.length ? null : el("li", { class: "step" }, el("span", { class: "meta" }, "plugs straight in: no cable needed")),
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

// Common things to shop for, offered when the wishlist search is empty.
const COMMON_SEARCHES = ["USB-C", "USB-C to HDMI", "USB-A to USB-C", "HDMI", "DisplayPort", "Lightning",
  "Micro-USB", "Ethernet", "3.5 mm aux", "RCA", "SATA", "power"];

// suggestions drops a list under a search box: common searches on click,
// then, as you type, those and catalog names containing what you typed.
// Picking one fills the box and runs its search.
function suggestions(input) {
  const list = el("ol", { class: "suggestions", hidden: true });
  let at = 0;
  const names = [...new Set(index.entries.filter((e) => ["cable", "adapter", "type"].includes(e.kind)).map((e) => e.title))];
  const mark = (text, q) => {
    const i = q ? text.toLowerCase().indexOf(q) : -1;
    return i < 0 ? text : [text.slice(0, i), el("strong", {}, text.slice(i, i + q.length)), text.slice(i + q.length)];
  };
  const pick = (text) => {
    input.value = text;
    list.hidden = true;
    input.dispatchEvent(new Event("input"));
  };
  const show = () => {
    const q = input.value.trim().toLowerCase();
    const common = COMMON_SEARCHES.filter((c) => c.toLowerCase().includes(q) && lookup(c).length);
    const found = q ? names.filter((n) => n.toLowerCase().includes(q) && !common.includes(n)).sort((a, b) =>
      a.toLowerCase().indexOf(q) - b.toLowerCase().indexOf(q) || a.localeCompare(b)) : [];
    const options = [...common.map((t) => ["common", t]), ...found.map((t) => ["catalog", t])].slice(0, 10);
    at = 0;
    fill(list, options.map(([kind, t], n) => el("li", { "aria-selected": String(n === 0),
      onmousedown: (ev) => { ev.preventDefault(); pick(t); } },
      el("span", { class: "kind" }, kind), el("span", { class: "title" }, mark(t, q)))));
    list.hidden = !options.length || (q && options.length === 1 && options[0][1].toLowerCase() === q);
  };
  const move = (by) => {
    const items = list.children;
    if (!items.length) return;
    items[at].setAttribute("aria-selected", "false");
    at = (at + by + items.length) % items.length;
    items[at].setAttribute("aria-selected", "true");
    items[at].scrollIntoView({ block: "nearest" });
  };
  input.addEventListener("focus", show);
  input.addEventListener("click", show);
  input.addEventListener("input", show);
  input.addEventListener("blur", () => { list.hidden = true; });
  input.addEventListener("keydown", (e) => {
    if (list.hidden) return;
    if (e.key === "ArrowDown") { e.preventDefault(); move(1); }
    else if (e.key === "ArrowUp") { e.preventDefault(); move(-1); }
    else if (e.key === "Enter" && list.children[at]) { e.preventDefault(); pick(list.children[at].querySelector(".title").textContent); }
    else if (e.key === "Escape") list.hidden = true;
  });
  return list;
}

async function showWish() {
  const wishes = await api("/api/wish");
  const results = el("ul", { class: "wish-results" });
  const add = async (key) => {
    await api("/api/wish/add", { parts: [key], note: "" });
    route();
  };
  const input = el("input", { type: "search", placeholder: "find a cable or adapter to add",
    oninput: () => {
      const hits = lookup(input.value).map((i) => index.entries[i]).filter((x) => ["cable", "adapter", "type"].includes(x.kind)).slice(0, 8);
      fill(results, hits.map((x) => el("li", {},
        el("button", { class: "quiet", onclick: () => add(x.id.slice(5)) }, "+ add"), " ", x.title, " ",
        el("span", { class: "meta" }, x.kind === "type" ? "" : "· you own some"))));
    } });
  const searchBox = el("div", { class: "suggest-wrap" }, searchField(input), suggestions(input));
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
          el("colgroup", {}, ["check", "item", "qty", "use"].map((c) => el("col", { class: c }))),
          el("tr", {}, el("th", { title: "acquired" }, "✓"), el("th", {}, "item"), el("th", { class: "num" }, "qty"), el("th", {}, "use case")),
          // Still wanted first, then what you've got, each oldest first.
          wishes.toSorted((a, b) => a.done - b.done).map((w) => el("tr", { class: w.done ? "done" : "" },
            el("td", {}, el("input", { type: "checkbox", checked: w.done, disabled: w.done,
              title: w.done ? "Acquired: it's in your inventory" : "Tick when you have it: it moves into your inventory",
              onchange: () => done(w.id) })),
            el("td", {}, el("a", { href: "#part/" + encodeURIComponent(w.part) }, w.name)),
            el("td", { class: "num" }, w.qty),
            el("td", { class: "meta" }, w.use_case))))
      : el("p", { class: "meta" }, "Empty. Search above, or find a route between two devices to see what you're missing."),
  ];
}

// --- footer: the cable meter and page size, as Low-tech Magazine shows its
// battery and page weight --------------------------------------------------

function footer(bytes) {
  const c = index.counts;
  const count = (n, noun) => `${n} ${noun}${n === 1 ? "" : "s"}`;
  $("#meter").textContent = `${count(c.cable || 0, "cable")}, ${count(c.devices || 0, "device")}`;
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
  orderTabs();
}

// orderTabs puts Your Network first once there's something in it: cables,
// adapters, devices or systems. An empty library starts on Connect.
function orderTabs() {
  const c = index.counts;
  const owned = (c.cable || 0) + (c.adapter || 0) + (c.devices || 0) + index.entries.filter((e) => e.kind === "system").length;
  const nav = $("nav");
  const network = nav.querySelector('[data-tab="network"]');
  if (owned && nav.firstElementChild !== network) nav.prepend(network);
  else if (!owned && nav.lastElementChild !== network) nav.append(network);
}

// --- start -----------------------------------------------------------------

document.addEventListener("keydown", (e) => {
  const typing = document.activeElement === $("#q");
  const inField = ["INPUT", "SELECT", "TEXTAREA"].includes(document.activeElement.tagName);
  if (e.key === "/" && !inField) {
    e.preventDefault();
    $("#q").focus();
  } else if (typing && e.key === "Escape") {
    closeSearch();
    $("#q").blur();
  } else if (typing && e.key === "ArrowDown") { e.preventDefault(); move(1); }
  else if (typing && e.key === "ArrowUp") { e.preventDefault(); move(-1); }
  else if (typing && e.key === "Enter") {
    const li = $("#results").children[selected];
    if (li) li.click();
  }
});
$("#q").addEventListener("input", search);
$("#q").addEventListener("focus", search);
$("#q").addEventListener("blur", () => { $("#dropdown").hidden = true; });
window.addEventListener("hashchange", route);

refresh().then(route);
