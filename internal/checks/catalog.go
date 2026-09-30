package checks

// Groups are the dashboard sections, in display order.
const (
	GroupPlatform      = "Platform"
	GroupACM           = "ACM Hub"
	GroupBackup        = "Backup & DR"
	GroupObservability = "Observability"
	GroupGovernance    = "Governance"
	GroupGitOps        = "GitOps"
	GroupFleet         = "Fleet"
	GroupRegistration  = "Registration"
	GroupAgents        = "Agents & Add-ons"
)

// Probe ConfigurationPolicy names in deploy/acm/policy-spoke-probes.yaml.
const (
	ProbeCOAvailable = "fv-probe-clusteroperators-available"
	ProbeCODegraded  = "fv-probe-clusteroperators-not-degraded"
	ProbeMCPDegraded = "fv-probe-mcp-not-degraded"
	ProbeMCPUpdated  = "fv-probe-mcp-updated"
	ProbeCSR         = "fv-probe-no-pending-csr"
	ProbeEtcd        = "fv-probe-etcd-members"
	ProbeKlusterlet  = "fv-probe-klusterlet-agent"
)

func governanceChecks(prefix string) []Check {
	return []Check{
		{ID: prefix + ".gov.policies-bound", Group: GroupGovernance, Severity: Critical,
			Title:   "Policies are placed on the cluster",
			Command: "oc get policy -n <cluster>", Run: checkPoliciesBound},
		{ID: prefix + ".gov.operator-policies", Group: GroupGovernance, Severity: Critical,
			Title:   "Minimum OperatorPolicies present",
			Command: "oc get policy -n <cluster> -o yaml | grep 'kind: OperatorPolicy'", Run: checkPolicyKindCount("OperatorPolicy")},
		{ID: prefix + ".gov.configuration-policies", Group: GroupGovernance, Severity: Critical,
			Title:   "Minimum ConfigurationPolicies present",
			Command: "oc get policy -n <cluster> -o yaml | grep 'kind: ConfigurationPolicy'", Run: checkPolicyKindCount("ConfigurationPolicy")},
		{ID: prefix + ".gov.compliant", Group: GroupGovernance, Severity: Critical,
			Title:   "All placed policies Compliant",
			Command: "oc get policy -n <cluster>", Run: checkPoliciesCompliant},
	}
}

// HubChecks is the ACM hub (local-cluster) checklist.
func HubChecks() []Check {
	c := []Check{
		// Platform
		{ID: "hub.platform.clusterversion", Group: GroupPlatform, Severity: Critical, Title: "ClusterVersion Available, not Failing",
			Command: "oc get clusterversion", Run: checkClusterVersion},
		{ID: "hub.platform.clusteroperators", Group: GroupPlatform, Severity: Critical, Title: "ClusterOperators Available, not Degraded",
			Command: "oc get clusteroperators", Run: checkClusterOperators},
		{ID: "hub.platform.mcp", Group: GroupPlatform, Severity: Critical, Title: "MachineConfigPools Updated, not Degraded",
			Command: "oc get mcp", Run: checkMCP},
		{ID: "hub.platform.nodes", Group: GroupPlatform, Severity: Critical, Title: "Nodes Ready, no pressure",
			Command: "oc get nodes", Run: checkNodes},
		{ID: "hub.platform.csr", Group: GroupPlatform, Severity: Warning, Title: "No pending CSRs",
			Command: "oc get csr | grep Pending", Run: checkPendingCSRs},
		{ID: "hub.platform.etcd-members", Group: GroupPlatform, Severity: Critical, Title: "etcd members healthy",
			Command: "oc get pods -n openshift-etcd -l app=etcd", Run: checkEtcdMembers},
		{ID: "hub.platform.etcd-db-size", Group: GroupPlatform, Severity: Warning, Title: "etcd DB size within limits",
			Command: "etcd_mvcc_db_total_size_in_bytes", Run: checkEtcdDBSize},
		{ID: "hub.platform.api-latency", Group: GroupPlatform, Severity: Warning, Title: "API server p99 latency < 1s",
			Command: "apiserver_request_duration_seconds p99", Run: checkAPILatency},
		// ACM Hub
		{ID: "hub.acm.mch-phase", Group: GroupACM, Severity: Critical, Title: "MultiClusterHub Running",
			Command: "oc get multiclusterhub -A", Run: checkMCHPhase},
		{ID: "hub.acm.mch-components", Group: GroupACM, Severity: Critical, Title: "MultiClusterHub components Available",
			Command: "oc get mch -n open-cluster-management -o jsonpath='{.items[0].status.components}'", Run: checkMCHComponents},
		{ID: "hub.acm.mce", Group: GroupACM, Severity: Critical, Title: "MultiClusterEngine Available",
			Command: "oc get multiclusterengine", Run: checkMCE},
		{ID: "hub.acm.csv", Group: GroupACM, Severity: Critical, Title: "ACM operator CSV Succeeded",
			Command: "oc get csv -n open-cluster-management", Run: checkACMCSV},
		{ID: "hub.acm.pods", Group: GroupACM, Severity: Critical, Title: "ACM hub pods Running",
			Command: "oc get pods -n open-cluster-management", Run: checkHubPods},
		{ID: "hub.acm.mce-pods", Group: GroupACM, Severity: Critical, Title: "MCE pods Running",
			Command: "oc get pods -n multicluster-engine", Run: checkMCEPods},
		// Backup & DR
		{ID: "hub.backup.enabled", Group: GroupBackup, Severity: Critical, Title: "cluster-backup component enabled",
			Command: "oc get mch -o jsonpath='{.items[0].spec.overrides.components}'", Run: checkBackupEnabled},
		{ID: "hub.backup.oadp", Group: GroupBackup, Severity: Critical, Title: "OADP operator Succeeded",
			Command: "oc get csv -n open-cluster-management-backup", Run: checkOADP},
		{ID: "hub.backup.storage-location", Group: GroupBackup, Severity: Critical, Title: "BackupStorageLocation Available",
			Command: "oc get backupstoragelocation -n open-cluster-management-backup", Run: checkBSL},
		{ID: "hub.backup.schedule", Group: GroupBackup, Severity: Critical, Title: "BackupSchedule Enabled, no collision",
			Command: "oc get backupschedule -n open-cluster-management-backup", Run: checkBackupSchedule},
		{ID: "hub.backup.recent", Group: GroupBackup, Severity: Warning, Title: "Recent backup Completed",
			Command: "oc get backup.velero.io -n open-cluster-management-backup", Run: checkRecentBackup},
		{ID: "hub.backup.policy", Group: GroupBackup, Severity: Warning, Title: "backup-restore-enabled policy Compliant",
			Command: "oc get policy backup-restore-enabled -n open-cluster-management-backup", Run: checkBackupPolicy},
		// Observability
		{ID: "hub.obs.mco", Group: GroupObservability, Severity: Warning, Title: "MultiClusterObservability Ready",
			Command: "oc get multiclusterobservability", Run: checkMCO},
		{ID: "hub.obs.pods", Group: GroupObservability, Severity: Warning, Title: "Observability pods Running",
			Command: "oc get pods -n open-cluster-management-observability", Run: checkObservabilityPods},
		{ID: "hub.obs.uwm", Group: GroupObservability, Severity: Info, Title: "User Workload Monitoring enabled",
			Command: "oc get cm cluster-monitoring-config -n openshift-monitoring -o yaml", Run: checkUWM},
		// GitOps
		{ID: "hub.gitops.argocd", Group: GroupGitOps, Severity: Warning, Title: "Argo CD instance Available",
			Command: "oc get argocd -A", Run: checkArgoCD},
		{ID: "hub.gitops.apps-synced", Group: GroupGitOps, Severity: Warning, Title: "Argo CD Applications Synced",
			Command: "oc get applications.argoproj.io -A", Run: checkArgoApps("sync")},
		{ID: "hub.gitops.apps-healthy", Group: GroupGitOps, Severity: Warning, Title: "Argo CD Applications Healthy",
			Command: "oc get applications.argoproj.io -A", Run: checkArgoApps("health")},
		{ID: "hub.gitops.gitopscluster", Group: GroupGitOps, Severity: Info, Title: "GitOpsCluster registration successful",
			Command: "oc get gitopscluster -A", Run: checkGitOpsCluster},
		// Fleet
		{ID: "hub.fleet.available", Group: GroupFleet, Severity: Critical, Title: "All ManagedClusters Available",
			Command: "oc get managedclusters", Run: checkFleetAvailable},
		{ID: "hub.fleet.joined", Group: GroupFleet, Severity: Warning, Title: "All ManagedClusters accepted and joined",
			Command: "oc get managedclusters", Run: checkFleetJoined},
		{ID: "hub.fleet.local-addons", Group: GroupFleet, Severity: Warning, Title: "local-cluster add-ons Available",
			Command: "oc get managedclusteraddons -n local-cluster", Run: checkAddonsHealthy},
		{ID: "hub.fleet.fleet-compliance", Group: GroupFleet, Severity: Warning, Title: "No NonCompliant policies in the fleet",
			Command: "oc get policy -A | grep NonCompliant", Run: checkFleetCompliance},
		{ID: "hub.gov.root-placement", Group: GroupGovernance, Severity: Warning, Title: "Every root policy reaches a cluster",
			Command: "oc get policy -A -l '!policy.open-cluster-management.io/root-policy'", Run: checkRootPolicyPlacement},
	}
	return append(c, governanceChecks("hub")...)
}

// ManagedChecks is the managed (spoke) cluster checklist.
func ManagedChecks() []Check {
	c := []Check{
		// Registration (hub-side view of the klusterlet)
		{ID: "mc.reg.accepted", Group: GroupRegistration, Severity: Critical, Title: "Hub accepted the cluster",
			Command: "oc get managedcluster <cluster>", Run: registrationCond("HubAcceptedManagedCluster", false)},
		{ID: "mc.reg.joined", Group: GroupRegistration, Severity: Critical, Title: "Klusterlet joined",
			Command: "oc get managedcluster <cluster>", Run: registrationCond("ManagedClusterJoined", false)},
		{ID: "mc.reg.available", Group: GroupRegistration, Severity: Critical, Title: "Cluster Available (reachable)",
			Command: "oc get managedcluster <cluster>", Run: registrationCond("ManagedClusterConditionAvailable", false)},
		{ID: "mc.reg.import", Group: GroupRegistration, Severity: Critical, Title: "Import succeeded",
			Command: "oc get managedcluster <cluster> -o yaml", Run: registrationCond("ManagedClusterImportSucceeded", true)},
		{ID: "mc.reg.clock", Group: GroupRegistration, Severity: Warning, Title: "Clock synced with hub",
			Command: "oc get managedcluster <cluster> -o yaml", Run: registrationCond("ManagedClusterConditionClockSynced", true)},
		{ID: "mc.reg.lease", Group: GroupRegistration, Severity: Critical, Title: "Registration agent lease fresh",
			Command: "oc get lease managed-cluster-lease -n <cluster>", Run: checkLease},
		// Agents & add-ons
		{ID: "mc.agents.required-addons", Group: GroupAgents, Severity: Critical, Title: "Required add-ons installed",
			Command: "oc get managedclusteraddons -n <cluster>", Run: checkRequiredAddons},
		{ID: "mc.agents.addons-available", Group: GroupAgents, Severity: Critical, Title: "Add-ons Available, not Degraded",
			Command: "oc get managedclusteraddons -n <cluster>", Run: checkAddonsHealthy},
		{ID: "mc.agents.klusterlet", Group: GroupAgents, Severity: Critical, Title: "Klusterlet agent deployments ready (probe)",
			Command: "oc get pods -n open-cluster-management-agent", Run: probe(ProbeKlusterlet)},
		// Platform (spoke OpenShift baseline)
		{ID: "mc.platform.version", Group: GroupPlatform, Severity: Critical, Title: "OpenShift version, no failed upgrade",
			Command: "oc get managedclusterinfo -n <cluster> <cluster>", Run: checkSpokeVersion},
		{ID: "mc.platform.nodes", Group: GroupPlatform, Severity: Critical, Title: "Nodes Ready",
			Command: "oc get managedclusterinfo -n <cluster> <cluster> -o yaml", Run: checkSpokeNodes},
		{ID: "mc.platform.co-available", Group: GroupPlatform, Severity: Critical, Title: "ClusterOperators Available (probe)",
			Command: "oc get clusteroperators", Run: probe(ProbeCOAvailable)},
		{ID: "mc.platform.co-degraded", Group: GroupPlatform, Severity: Critical, Title: "ClusterOperators not Degraded (probe)",
			Command: "oc get clusteroperators", Run: probe(ProbeCODegraded)},
		{ID: "mc.platform.mcp-degraded", Group: GroupPlatform, Severity: Critical, Title: "MachineConfigPools not Degraded (probe)",
			Command: "oc get mcp", Run: probe(ProbeMCPDegraded)},
		{ID: "mc.platform.mcp-updated", Group: GroupPlatform, Severity: Warning, Title: "MachineConfigPools Updated (probe)",
			Command: "oc get mcp", Run: probe(ProbeMCPUpdated)},
		{ID: "mc.platform.csr", Group: GroupPlatform, Severity: Warning, Title: "No pending CSRs (probe)",
			Command: "oc get csr | grep Pending", Run: probe(ProbeCSR)},
		{ID: "mc.platform.etcd", Group: GroupPlatform, Severity: Critical, Title: "etcd members available (probe)",
			Command: "oc get etcd cluster -o yaml", Run: probe(ProbeEtcd)},
	}
	return append(c, governanceChecks("mc")...)
}

// Catalog returns every check by cluster type (used by the UI and docs).
func Catalog() map[ClusterType][]Check {
	return map[ClusterType][]Check{Hub: HubChecks(), Managed: ManagedChecks()}
}
