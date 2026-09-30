"use strict";

const GROUP_ORDER = ["Registration", "Agents & Add-ons", "Platform", "ACM Hub", "Backup & DR",
  "Observability", "Governance", "GitOps", "Fleet"];
let selected = null;

function el(tag, attrs = {}, ...children) {
  const e = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === "class") e.className = v; else if (k === "onclick") e.onclick = v; else e.setAttribute(k, v);
  }
  for (const c of children) e.append(c instanceof Node ? c : document.createTextNode(c ?? ""));
  return e;
}

function scoreClass(r) {
  if (!r.ready) return "t-fail";
  return r.score >= 90 ? "t-pass" : "t-warn";
}

function renderList(reports) {
  const nav = document.getElementById("clusters");
  nav.replaceChildren(...reports.map(r => el("div", {
    class: "cluster" + (r.cluster === selected ? " active" : ""),
    onclick: () => { selected = r.cluster; refresh(); },
  },
    el("div", { class: "row" }, el("span", { class: "name" }, r.cluster),
      el("span", { class: "score " + scoreClass(r) }, r.score.toFixed(0))),
    el("div", { class: "row" }, el("span", { class: "type" }, r.type),
      el("span", { class: "badge " + (r.ready ? "pass" : "fail") }, r.ready ? "READY" : "NOT READY")),
  )));
}

function renderDetail(r) {
  const sec = document.getElementById("detail");
  if (!r) { sec.replaceChildren(el("p", { class: "empty" }, "Waiting for the first validation…")); return; }
  const c = r.counts || {};
  const info = Object.entries(r.info || {}).filter(([, v]) => v).map(([k, v]) => `${k}: ${v}`).join(" · ");
  const head = el("div", { class: "head" },
    el("div", {}, el("h2", {}, r.cluster), el("div", { class: "meta" },
      `${r.type} · checked ${new Date(r.checkedAt).toLocaleTimeString()} · ${r.durationSeconds.toFixed(1)}s`),
      el("div", { class: "meta" }, info)),
    el("div", { class: "score " + scoreClass(r) }, `${r.score.toFixed(1)} / 100`),
    el("div", {}, el("span", { class: "badge pass" }, `${c.pass || 0} pass`), " ",
      el("span", { class: "badge warn" }, `${c.warn || 0} warn`), " ",
      el("span", { class: "badge fail" }, `${c.fail || 0} fail`), " ",
      el("span", { class: "badge skip" }, `${c.skip || 0} skip`)));

  const groups = {};
  for (const res of r.results) (groups[res.group] ||= []).push(res);
  const names = Object.keys(groups).sort((a, b) => GROUP_ORDER.indexOf(a) - GROUP_ORDER.indexOf(b));
  const blocks = names.map(g => el("div", { class: "group" },
    el("h3", {}, el("span", {}, g), el("span", {}, `${(r.groupScores[g] ?? 100).toFixed(0)}%`)),
    el("table", {}, ...groups[g].map(res => el("tr", {},
      el("td", { class: "state" }, el("span", { class: "badge " + res.state }, res.state.toUpperCase())),
      el("td", { class: "title" }, res.title),
      el("td", { class: "sev" }, res.severity),
      el("td", { class: "detail" }, res.detail || "", el("code", {}, res.command || "")),
    )))));
  sec.replaceChildren(head, ...blocks);
}

async function refresh() {
  try {
    const reports = await (await fetch("api/v1/clusters")).json();
    if (!selected && reports.length) selected = reports[0].cluster;
    renderList(reports);
    renderDetail(reports.find(r => r.cluster === selected));
  } catch (e) { console.error(e); }
}

fetch("api/v1/version").then(r => r.json()).then(v => {
  document.getElementById("version").textContent = v.version;
});
refresh();
setInterval(refresh, 15000);
