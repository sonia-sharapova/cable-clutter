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
  for (const c of children.flat()) {
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
const statusClass = (s) => (s || "").replace(" ", "");
const statusTag = (s) => el("span", { class: "status " + statusClass(s) }, s);

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
  list.replaceChildren(
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
    a.toggleAttribute("aria-current", a.getAttribute("href") === "#" + (["connect", "systems", "wish"].includes(kind) ? kind : ""));
  }
  const view = $("#view");
  $("#search").hidden = kind !== "";
  view.hidden = kind === "";
  if (kind === "") {
    $("#q").focus();
    return;
  }
  const render = { part: showPart, connector: showConnector, device: showDevice, model: showDevice, system: showSystem, connect: showConnect, systems: showSystems, wish: showWish }[kind];
  if (!render) return;
  view.replaceChildren(el("p", { class: "meta" }, "…"));
  try {
    view.replaceChildren(el("a", { href: "#", class: "back meta" }, "← search"), ...(await render(arg)));
  } catch (err) {
    view.replaceChildren(el("a", { href: "#", class: "back meta" }, "← search"), el("p", { class: "warn" }, err.message));
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
    d.items.length
      ? el("table", {},
          el("tr", {}, ["id", "status", "name", "where", "for"].map((h) => el("th", {}, h))),
          d.items.map((it) => el("tr", {},
            el("td", { class: "meta" }, "#" + it.short_id),
            el("td", {}, statusTag(it.status), it.held_by ? el("span", { class: "meta" }, " " + it.held_by) : null),
            el("td", {}, it.nickname || ""), el("td", {}, it.location || ""), el("td", {}, it.for || ""))))
      : el("p", { class: "meta" }, "You don't own one. ", el("code", {}, `cbl wish add "${p.name}"`)),
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
            el("span", { class: "meta" }, ` ×${o.items.length} · ${o.free} free${o.dedicated ? " · dedicated" : ""}`))))
        : el("p", { class: "meta" }, "Nothing you own fits."),
      p.cant_yet.map((s) => el("p", { class: "cant" }, "Can't yet: ", sig(s.signals), " → ",
        el("a", { href: "#part/" + encodeURIComponent(s.part.key) }, s.part.name))),
      p.wrong_voltage.map((o) => el("p", { class: "warn" }, `⚠ Don't use ${o.part.name}: it's ${o.part.volts} V, this port needs ${p.volts} V`)),
      p.unknown_voltage.map((o) => el("p", { class: "warn" }, `? ${o.part.name}: voltage unknown; check the label (needs ${p.volts} V)`)),
    ]),
  ];
}

async function showSystem(name) {
  const d = await api("/api/system?name=" + encodeURIComponent(name));
  const msg = el("div", {});
  const toggle = async (sw) => {
    const res = await api(d.up ? "/api/system/down" : "/api/system/up", { name: d.name, switch: sw });
    await refresh();
    if (res.up === d.up) {
      // Didn't come up: say why, and offer to switch.
      msg.replaceChildren(
        ...(res.missing || []).map((m) => el("p", { class: "warn" }, "Missing: " + m)),
        ...(res.blocked || []).map((b) => el("p", { class: "warn" }, b)),
        res.blocked && res.blocked.length ? el("button", { onclick: () => toggle(true) }, "Take those down and bring this up") : null);
      return;
    }
    route();
  };
  const endpoint = (dev, port) => (dev ? dev + (port ? " · " + port : "") : "?");
  return [
    el("h2", {}, d.name, " ", statusTag(d.up ? "up" : "down")),
    el("div", { class: "row" }, el("button", { onclick: () => toggle(false) }, d.up ? "Take it down" : "Bring it up")),
    msg,
    el("table", {},
      el("tr", {}, ["", "from → to", "cable", "item"].map((h) => el("th", {}, h))),
      d.links.map((l, n) => el("tr", {},
        el("td", { class: "meta" }, n + 1),
        el("td", {}, endpoint(l.from_device, l.from_port), " → ", endpoint(l.to_device, l.to_port)),
        el("td", {}, el("a", { href: "#part/" + encodeURIComponent(l.part) }, d.part_names[l.part])),
        el("td", { class: "meta" }, l.assigned ? `#${d.short_ids[l.assigned]} in use` : l.preferred ? `pinned #${d.short_ids[l.preferred]}` : "any free")))),
    d.conflicts.map((c) => el("p", { class: "cant" }, `Can't be up with "${c.with}": `, c.port ? `both plug into ${c.port}` : `both need ${d.part_names[c.part] || c.part} (you own ${c.have})`)),
    d.missing.map((m) => el("p", { class: "warn" }, `Missing: needs ${m.need} × ${d.part_names[m.part] || m.part}, you own ${m.owned}`)),
  ];
}

async function showSystems() {
  const systems = index.entries.filter((e) => e.kind === "system");
  return [
    el("h2", {}, "Systems"),
    systems.length
      ? el("ul", {}, systems.map((s) => el("li", {},
          statusTag(s.status), " ", el("a", { href: "#system/" + encodeURIComponent(s.title) }, s.title), " ", el("span", { class: "meta" }, s.sub))))
      : el("p", { class: "meta" }, "No systems yet. ", el("code", {}, 'cbl plan PS2 "CRT TV" --save "Retro corner"')),
  ];
}

async function showConnect() {
  const out = el("div", {});
  const pick = (id) => el("select", { id }, index.devices.map((d) => el("option", {}, d)));
  const from = pick("from"), to = pick("to");
  if (index.devices.length > 1) to.selectedIndex = 1;
  const go = async () => {
    out.replaceChildren(el("p", { class: "meta" }, "…"));
    const d = await api(`/api/connect?from=${encodeURIComponent(from.value)}&to=${encodeURIComponent(to.value)}`);
    const kinds = ["video", "audio", "data"].filter((k) => d.routes[k]);
    out.replaceChildren(
      kinds.length ? null : el("p", {}, `No way to connect ${d.from} and ${d.to} within 3 cables and adapters.`),
      kinds.map((k) => {
        const r = d.routes[k];
        return [
          el("h3", {}, k.toUpperCase(), " ", el("span", { class: "meta" }, d.signal_names[r.signal])),
          el("ul", {},
            el("li", { class: "meta" }, `${d.from} · ${r.from_port}`),
            r.steps.map((s) => el("li", { class: s.buy ? "cant" : "can" },
              s.buy ? "★ need: " : "→ ", el("a", { href: "#part/" + encodeURIComponent(s.part.key) }, s.part.name),
              el("span", { class: "meta" }, s.buy ? "" : ` #${shortId(s.item)}${s.busy_in ? ` (in use by ${s.busy_in})` : " free"}`))),
            el("li", { class: "meta" }, `${d.to} · ${r.to_port}`)),
        ];
      }),
      d.to_buy && d.to_buy.length ? el("p", {}, "To buy: ", d.to_buy.join(", ")) : kinds.length ? el("p", { class: "can" }, "Everything needed is in your inventory.") : null,
    );
  };
  return [
    el("h2", {}, "Connect"),
    el("div", { class: "row" }, from, el("span", {}, "→"), to, el("button", { onclick: go }, "Find a way")),
    out,
  ];
}

async function showWish() {
  const wishes = await api("/api/wish");
  const done = async (id) => {
    await api("/api/wish/done", { id });
    await refresh();
    route();
  };
  return [
    el("h2", {}, "Wishlist"),
    wishes.length
      ? el("table", {},
          el("tr", {}, ["item", "qty", "for", ""].map((h) => el("th", {}, h))),
          wishes.map((w) => el("tr", {},
            el("td", {}, el("a", { href: "#part/" + encodeURIComponent(w.part) }, w.name), w.notes ? el("span", { class: "sub" }, w.notes) : null),
            el("td", {}, w.qty), el("td", {}, w.for || ""),
            el("td", {}, el("button", { onclick: () => done(w.id) }, "Bought")))))
      : el("p", { class: "meta" }, "Empty. Connect two devices to see what you're missing."),
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
  $("#meter").replaceChildren(
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
