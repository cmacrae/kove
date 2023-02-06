package general

get_default(val, key, _) = val[key]

get_default(val, key, fallback) = fallback {
    not val[key]
}

# Deployment: Kafka Connect JDBC Sink image version check
main[output] {
    minimum_vers = "0.0.10"
    input.kind == "KafkaConnect"

    regex.match("^harbor.ci.nutmeg.co.uk/nutmeg/kafka-connect-jdbc-sink", input.spec.image)
    extractor := regex.split(":", input.spec.image)
    vers := extractor[1]
    semver.compare(minimum_vers, vers) > 0

    output := {
            "Name": input.metadata.name,
            "Namespace": input.metadata.namespace,
            "Kind": input.kind,
            "ApiVersion": input.apiVersion,

            "RuleSet": sprintf("JDBC Sink image version %s is lower than the minimum version %s.", [input.spec.image, minimum_vers]),
            "Data": get_default(input.metadata.annotations, "nutmeg.com/owner", "<undefined>"),
    }
}