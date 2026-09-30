#!/usr/bin/env python3
"""Generate the two fleet-validator Grafana dashboards (MCO / ACM Observability Grafana).

  deploy/grafana/acm-hub-validation-dashboard.json          one hub (local-cluster)
  deploy/grafana/managed-cluster-validation-dashboard.json  one managed cluster at a time

Sections follow the checklist groups in internal/checks/catalog.go. Every query is wrapped in
`max by (...)` so series from a restarted pod (still inside the lookback window) never
double up.  Edit this file, then run ./hack/render-grafana-configmap.sh and
./hack/render-acm-policy.sh.
"""
import json
import os

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OUT = os.path.join(ROOT, "deploy", "grafana")
DS = {"type": "prometheus", "uid": "${datasource}"}

HUB_GROUPS = ["Platform", "ACM Hub", "Backup & DR", "Observability", "Governance", "GitOps", "Fleet"]
MANAGED_GROUPS = ["Registration", "Agents & Add-ons", "Platform", "Governance"]

STATE_MAPPINGS = [{
    "type": "value",
    "options": {
        "1": {"text": "PASS", "color": "green", "index": 0},
        "0.5": {"text": "WARN", "color": "orange", "index": 1},
        "0": {"text": "FAIL", "color": "red", "index": 2},
        "-1": {"text": "SKIP", "color": "text", "index": 3},
    },
}]

GROUP_HELP = {
    "Platform": "OpenShift baseline: ClusterVersion, ClusterOperators, MachineConfigPools, nodes, CSRs, etcd, API latency.",
    "ACM Hub": "MultiClusterHub / MultiClusterEngine health, ACM operator CSV and hub pods.",
    "Backup & DR": "cluster-backup, OADP, BackupStorageLocation, BackupSchedule and the backup-restore-enabled policy.",
    "Observability": "MultiClusterObservability, observability pods and User Workload Monitoring.",
    "Governance": "Policies placed on the cluster: minimum OperatorPolicies / ConfigurationPolicies, all Compliant.",
    "GitOps": "Argo CD instances and Applications (the hub GitOps loop).",
    "Fleet": "Hub view of every ManagedCluster, local-cluster add-ons and fleet-wide policy compliance.",
    "Registration": "Hub-side view of the klusterlet: accepted, joined, available, import, clock, lease.",
    "Agents & Add-ons": "Required add-ons present and Available; klusterlet agent deployments ready (probe).",
}


class Ids:
    def __init__(self):
        self.n = 0

    def next(self):
        self.n += 1
        return self.n


def target(expr, ref="A", legend="", instant=True, fmt="time_series"):
    return {"datasource": DS, "expr": expr, "refId": ref, "legendFormat": legend,
            "instant": instant, "range": not instant, "format": fmt}


def stat(ids, title, expr, x, y, w=4, h=4, unit="short", steps=None, mappings=None, legend="",
         text_mode="auto", desc=""):
    return {
        "id": ids.next(), "type": "stat", "title": title, "description": desc, "datasource": DS,
        "gridPos": {"h": h, "w": w, "x": x, "y": y},
        "targets": [target(expr, legend=legend)],
        "fieldConfig": {"defaults": {
            "unit": unit, "mappings": mappings or [],
            "thresholds": {"mode": "absolute", "steps": steps or [{"color": "blue", "value": None}]},
            "color": {"mode": "thresholds"}}, "overrides": []},
        "options": {"reduceOptions": {"calcs": ["lastNotNull"], "fields": "", "values": False},
                    "colorMode": "background" if mappings else "value", "graphMode": "none",
                    "justifyMode": "center", "textMode": text_mode, "orientation": "auto"},
    }


def gauge(ids, title, expr, x, y, w=4, h=8):
    return {
        "id": ids.next(), "type": "gauge", "title": title, "datasource": DS,
        "gridPos": {"h": h, "w": w, "x": x, "y": y},
        "targets": [target(expr)],
        "fieldConfig": {"defaults": {
            "unit": "none", "min": 0, "max": 100, "decimals": 1,
            "thresholds": {"mode": "absolute", "steps": [
                {"color": "red", "value": None}, {"color": "orange", "value": 70}, {"color": "green", "value": 90}]},
            "color": {"mode": "thresholds"}}, "overrides": []},
        "options": {"reduceOptions": {"calcs": ["lastNotNull"], "fields": "", "values": False},
                    "showThresholdLabels": False, "showThresholdMarkers": True},
    }


def bargauge(ids, title, expr, legend, x, y, w=8, h=8):
    return {
        "id": ids.next(), "type": "bargauge", "title": title, "datasource": DS,
        "gridPos": {"h": h, "w": w, "x": x, "y": y},
        "targets": [target(expr, legend=legend)],
        "fieldConfig": {"defaults": {
            "unit": "none", "min": 0, "max": 100, "decimals": 0,
            "thresholds": {"mode": "absolute", "steps": [
                {"color": "red", "value": None}, {"color": "orange", "value": 70}, {"color": "green", "value": 90}]},
            "color": {"mode": "thresholds"}}, "overrides": []},
        "options": {"reduceOptions": {"calcs": ["lastNotNull"], "fields": "", "values": False},
                    "orientation": "horizontal", "displayMode": "gradient", "showUnfilled": True},
    }


def timeseries(ids, title, expr, legend, x, y, w=8, h=8):
    return {
        "id": ids.next(), "type": "timeseries", "title": title, "datasource": DS,
        "gridPos": {"h": h, "w": w, "x": x, "y": y},
        "targets": [target(expr, legend=legend, instant=False)],
        "fieldConfig": {"defaults": {"unit": "none", "min": 0, "max": 100,
                                     "custom": {"lineWidth": 2, "fillOpacity": 10, "spanNulls": True}},
                        "overrides": []},
        "options": {"legend": {"displayMode": "list", "placement": "bottom"}, "tooltip": {"mode": "multi"}},
    }


def row(ids, title, y, collapsed=False, panels=None):
    return {"id": ids.next(), "type": "row", "title": title, "collapsed": collapsed,
            "gridPos": {"h": 1, "w": 24, "x": 0, "y": y}, "panels": panels or []}


def check_table(ids, group, sel, y, h=9):
    """Checklist table for one group: Result | Check | Severity | Detail, failures first."""
    g = f'group="{group}"'
    return {
        "id": ids.next(), "type": "table", "title": f"{group} checks", "description": GROUP_HELP.get(group, ""),
        "datasource": DS, "gridPos": {"h": h, "w": 24, "x": 0, "y": y},
        "targets": [
            target(f"max by (check, title, severity) (fleetvalidator_check_state{{{sel}, {g}}})", "A", fmt="table"),
            target(f"max by (check, detail) (fleetvalidator_check_detail{{{sel}, {g}}})", "B", fmt="table"),
        ],
        "transformations": [
            {"id": "merge", "options": {}},
            {"id": "organize", "options": {
                "excludeByName": {"Time": True, "Value #B": True, "check": True},
                "indexByName": {"Value #A": 0, "title": 1, "severity": 2, "detail": 3},
                "renameByName": {"Value #A": "Result", "title": "Check", "severity": "Severity", "detail": "Detail"},
            }},
        ],
        "fieldConfig": {
            "defaults": {"custom": {"align": "left", "filterable": False}},
            "overrides": [
                {"matcher": {"id": "byName", "options": "Result"}, "properties": [
                    {"id": "mappings", "value": STATE_MAPPINGS},
                    {"id": "custom.width", "value": 90},
                    {"id": "custom.align", "value": "center"},
                    {"id": "custom.displayMode", "value": "color-background"},
                    {"id": "custom.cellOptions", "value": {"type": "color-background"}},
                ]},
                {"matcher": {"id": "byName", "options": "Check"}, "properties": [{"id": "custom.width", "value": 380}]},
                {"matcher": {"id": "byName", "options": "Severity"}, "properties": [{"id": "custom.width", "value": 100}]},
            ],
        },
        "options": {"showHeader": True, "cellHeight": "sm",
                    "sortBy": [{"displayName": "Result", "desc": False}]},
    }


def overview(ids, sel, y, extra_version):
    """Score gauge, ready flag, per-state counts, freshness and versions."""
    cnt = lambda st: f'max(fleetvalidator_cluster_checks{{{sel}, state="{st}"}}) or vector(0)'
    ready_map = [{"type": "value", "options": {
        "1": {"text": "READY", "color": "green", "index": 0},
        "0": {"text": "NOT READY", "color": "red", "index": 1}}}]
    panels = [
        gauge(ids, "Readiness score", f"max(fleetvalidator_cluster_score{{{sel}}})", 0, y),
        stat(ids, "Production ready", f"max(fleetvalidator_cluster_ready{{{sel}}})", 4, y, mappings=ready_map,
             steps=[{"color": "red", "value": None}, {"color": "green", "value": 1}],
             desc="READY when no critical check fails."),
        stat(ids, "Failing", cnt("fail"), 8, y, w=2, steps=[{"color": "green", "value": None}, {"color": "red", "value": 1}]),
        stat(ids, "Warnings", cnt("warn"), 10, y, w=2, steps=[{"color": "green", "value": None}, {"color": "orange", "value": 1}]),
        stat(ids, "Passing", cnt("pass"), 12, y, w=2, steps=[{"color": "green", "value": None}]),
        stat(ids, "Skipped", cnt("skip"), 14, y, w=2, steps=[{"color": "text", "value": None}],
             desc="Not applicable or no data (API not installed, probe policy not placed). Not scored."),
        stat(ids, "Last validated", f"time() - max(fleetvalidator_last_validation_timestamp_seconds{{{sel}}})", 4, y + 4,
             w=4, unit="s", steps=[{"color": "green", "value": None}, {"color": "orange", "value": 300},
                                   {"color": "red", "value": 900}],
             desc="Includes the MCO metrics-collector delay (default 5m)."),
        stat(ids, "OpenShift", f"max by (openshift_version) (fleetvalidator_cluster_info{{{sel}}})", 8, y + 4,
             w=4, legend="{{openshift_version}}", text_mode="name"),
    ]
    if extra_version:
        panels.append(stat(ids, "ACM", f"max by (acm_version) (fleetvalidator_cluster_info{{{sel}}})", 12, y + 4,
                           w=4, legend="{{acm_version}}", text_mode="name"))
    panels.append(bargauge(ids, "Score by section", f"max by (group) (fleetvalidator_group_score{{{sel}}})",
                           "{{group}}", 16, y))
    return panels


def variables(var_name, label, cluster_type):
    return {"list": [
        {"name": "datasource", "label": "Data source", "type": "datasource", "query": "prometheus",
         "current": {"selected": True, "text": "Observatorium", "value": "Observatorium"},
         "regex": "", "refresh": 1, "hide": 0, "includeAll": False, "multi": False},
        {"name": var_name, "label": label, "type": "query", "datasource": DS,
         "definition": f'label_values(fleetvalidator_cluster_score{{cluster_type="{cluster_type}"}}, managed_cluster)',
         "query": {"query": f'label_values(fleetvalidator_cluster_score{{cluster_type="{cluster_type}"}}, managed_cluster)',
                   "refId": "PrometheusVariableQueryEditor-VariableQuery"},
         "refresh": 2, "sort": 1, "multi": False, "includeAll": False, "hide": 0},
    ]}


def dashboard(uid, title, desc, templating, panels):
    return {
        "uid": uid, "title": title, "description": desc,
        "tags": ["fleet-validator", "acm", "readiness"],
        "timezone": "browser", "editable": True, "graphTooltip": 1, "schemaVersion": 37, "version": 1,
        "refresh": "1m", "time": {"from": "now-24h", "to": "now"},
        "annotations": {"list": []},
        "links": [{"title": "Fleet validator", "type": "dashboards", "tags": ["fleet-validator"],
                   "asDropdown": True, "includeVars": False, "keepTime": True}],
        "templating": templating, "panels": panels,
    }


def hub_dashboard():
    ids = Ids()
    sel = 'managed_cluster="$hub", cluster_type="hub"'
    panels = [row(ids, "Overview", 0)]
    panels += overview(ids, sel, 1, extra_version=True)
    panels.append(timeseries(ids, "Hub score over time", f"max(fleetvalidator_cluster_score{{{sel}}})",
                             "score", 0, 9, w=12))
    panels.append(timeseries(ids, "Managed cluster scores over time",
                             'max by (managed_cluster) (fleetvalidator_cluster_score{cluster_type="managed"})',
                             "{{managed_cluster}}", 12, 9, w=12))
    y = 17
    for g in HUB_GROUPS:
        panels.append(row(ids, g, y))
        panels.append(check_table(ids, g, sel, y + 1))
        y += 10

    fleet = {
        "id": ids.next(), "type": "table", "title": "Managed clusters",
        "description": "Every validated managed cluster. Open one in the Managed Cluster Validation dashboard.",
        "datasource": DS, "gridPos": {"h": 9, "w": 24, "x": 0, "y": y + 1},
        "targets": [
            target('max by (managed_cluster) (fleetvalidator_cluster_score{cluster_type="managed"})', "A", fmt="table"),
            target('max by (managed_cluster) (fleetvalidator_cluster_ready{cluster_type="managed"})', "B", fmt="table"),
            target('max by (managed_cluster) (fleetvalidator_cluster_checks{cluster_type="managed", state="fail"})',
                   "C", fmt="table"),
            target('max by (managed_cluster, openshift_version) (fleetvalidator_cluster_info{cluster_type="managed"})',
                   "D", fmt="table"),
        ],
        "transformations": [
            {"id": "merge", "options": {}},
            {"id": "organize", "options": {
                "excludeByName": {"Time": True, "Value #D": True},
                "indexByName": {"managed_cluster": 0, "Value #B": 1, "Value #A": 2, "Value #C": 3, "openshift_version": 4},
                "renameByName": {"managed_cluster": "Cluster", "Value #A": "Score", "Value #B": "Ready",
                                 "Value #C": "Failing checks", "openshift_version": "OpenShift"},
            }},
        ],
        "fieldConfig": {"defaults": {"custom": {"align": "left"}}, "overrides": [
            {"matcher": {"id": "byName", "options": "Cluster"}, "properties": [
                {"id": "links", "value": [{"title": "Open cluster checklist",
                                           "url": "/d/managed-cluster-validation-dashboard/"
                                                  "?var-managed_cluster=${__value.raw}&${datasource:queryparam}"}]}]},
            {"matcher": {"id": "byName", "options": "Ready"}, "properties": [
                {"id": "mappings", "value": [{"type": "value", "options": {
                    "1": {"text": "READY", "color": "green", "index": 0},
                    "0": {"text": "NOT READY", "color": "red", "index": 1}}}]},
                {"id": "custom.displayMode", "value": "color-background"},
                {"id": "custom.cellOptions", "value": {"type": "color-background"}}]},
            {"matcher": {"id": "byName", "options": "Score"}, "properties": [
                {"id": "min", "value": 0}, {"id": "max", "value": 100},
                {"id": "thresholds", "value": {"mode": "absolute", "steps": [
                    {"color": "red", "value": None}, {"color": "orange", "value": 70}, {"color": "green", "value": 90}]}},
                {"id": "custom.displayMode", "value": "gradient-gauge"},
                {"id": "custom.cellOptions", "value": {"type": "gauge", "mode": "gradient"}}]},
        ]},
        "options": {"showHeader": True, "sortBy": [{"displayName": "Score", "desc": False}]},
    }
    panels.append(row(ids, "Managed clusters", y))
    panels.append(fleet)
    return dashboard("acm-hub-validation-dashboard", "ACM Hub Validation",
                     "Production readiness checklist of the ACM hub (local-cluster), from fleet-validator.",
                     variables("hub", "Hub", "hub"), panels)


def managed_dashboard():
    ids = Ids()
    sel = 'managed_cluster="$managed_cluster", cluster_type="managed"'
    panels = [row(ids, "Overview", 0)]
    panels += overview(ids, sel, 1, extra_version=False)
    panels.append(timeseries(ids, "Score over time", f"max(fleetvalidator_cluster_score{{{sel}}})",
                             "score", 0, 9, w=12))
    panels.append(timeseries(ids, "Section scores over time",
                             f"max by (group) (fleetvalidator_group_score{{{sel}}})", "{{group}}", 12, 9, w=12))
    y = 17
    for g in MANAGED_GROUPS:
        panels.append(row(ids, g, y))
        panels.append(check_table(ids, g, sel, y + 1))
        y += 10
    return dashboard("managed-cluster-validation-dashboard", "Managed Cluster Validation",
                     "Production readiness checklist of one ACM managed cluster, from fleet-validator. "
                     "Checks marked (probe) come from the fleet-validator-spoke-probes policy.",
                     variables("managed_cluster", "Managed cluster", "managed"), panels)


def main():
    os.makedirs(OUT, exist_ok=True)
    for name, d in (("acm-hub-validation-dashboard", hub_dashboard()),
                    ("managed-cluster-validation-dashboard", managed_dashboard())):
        path = os.path.join(OUT, name + ".json")
        with open(path, "w") as f:
            json.dump(d, f, indent=2)
            f.write("\n")
        print("wrote", os.path.relpath(path, ROOT))


if __name__ == "__main__":
    main()
