package example

# Label matchers we want to look for.
labels["secure"] = ["nope"]

# Kinds of objects we care about evaluating.
# This isn't strictly necessary if you're satisfied with the 'objects' configuration
# option for the exporter; it'll only watch what it's told.
kinds = ["Deployment", "StatefulSet", "DaemonSet"]

bad[stuff] {
	# Assign our object manifest (input) to the variable 'r'
	r := input

	# Does our object's 'kind' field match any in our 'kinds' array?
	r.kind == kinds[_]

	# Does the value of our object's 'secure' label match any in our 'labels.secure' array?
	r.metadata.labels.secure == labels.secure[_]

	# If the above conditions are true, express a set containing various pieces of
	# information about our object. As you can see, we're assigning this to the variable
	# 'stuff', which you may notice in the expression signature is what we're returning.
	# This information is then used to expose a Prometheus metric with labels using this
	# information.
	stuff := {
		"Name": r.metadata.name,
		"Namespace": r.metadata.namespace,
		"Kind": r.kind,
		"ApiVersion": r.apiVersion,
		"RuleSet": "Insecure object", # Explain why this is a violation
	}
}
