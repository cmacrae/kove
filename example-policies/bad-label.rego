package bad

labels["app"] = ["violation"]

types = ["Deployment", "StatefulSet"]

main[return] {
	r := input
	r.kind == types[_]
	r.metadata.labels.app == labels.app[_]

	return := {
		"Name": r.metadata.name,
		"Namespace": r.metadata.namespace,
		"Kind": r.kind,
		"ApiVersion": r.apiVersion,
		"RuleSet": "Bad labels",
	}
}
