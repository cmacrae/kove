package appchart_version

# Declare the minimum version we want
minimum_vers = "3.0.2"

main[output] {
	# Evaluate spcific chart
	regex.match("^specific-chart-name", input.metadata.labels["helm.sh/chart"])

	# Split the value of the 'helm.sh/chart' label into a '-' separated list
	extractor := regex.split("-", input.metadata.labels["helm.sh/chart"])

	# Get the version number at the final position of the list
	vers := extractor[minus(count(extractor), 1)]

	# Compare the version against our minimum version
	semver.compare(minimum_vers, vers) > 0

	output := {
		"Name": input.metadata.name,
		"Namespace": input.metadata.namespace,
		"Kind": input.kind,
		"ApiVersion": input.apiVersion,
		"RuleSet": sprintf("Chart version %s is lower than the minimum version %s", [vers, minimum_vers]),
		"Data": input.metadata.annotations.["company.domain/team"], # Expose the owner
	}
}
