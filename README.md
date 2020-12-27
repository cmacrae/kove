<p align="center">
  <a href="https://github.com/cmacrae/kube-opa-violation-exporter/blob/master/LICENSE">
    <img src="https://img.shields.io/github/license/cmacrae/kube-opa-violation-exporter.svg?color=a6dcef" alt="License Badge">
  </a>
  <a href="https://github.com/cmacrae/kube-opa-violation-exporter/compare/v1.0.0...HEAD">
    <img src="https://img.shields.io/github/commits-since/cmacrae/kube-opa-violation-exporter/latest.svg?color=ea907a" alt="Version Badge">
  </a>
  <a href="https://github.com/cmacrae/kube-opa-violation-exporter/projects/1">
    <img src="https://img.shields.io/badge/Project-tasks-7fdbda.svg?logo=trello" alt="GitHub Project Badge">
  </a>
  <a href="https://hub.docker.com/r/cmacrae/kube-opa-violation-exporter">
    <img src="https://img.shields.io/badge/docker-image-2496ED.svg?logo=Docker" alt="Helm Badge">
  </a>
  <a href="https://charts.cmacr.ae/#kube-opa-violation-exporter">
    <img src="https://img.shields.io/badge/helm-chart-277A9F.svg?logo=Helm" alt="Helm Badge">
  </a>
</p>

# kube-opa-violation-exporter
Watch your in cluster k8s manifests for OPA policy violations and export them as Prometheus metrics

## About
[Open Policy Agent](https://www.openpolicyagent.org/) provide the fearsome-but-trustworthy  [gatekeeper](https://github.com/open-policy-agent/gatekeeper), which
allows for [admission control](https://www.openpolicyagent.org/docs/latest/kubernetes-introduction/#how-does-it-work-with-plain-opa-and-kube-mgmt) of Kubernetes
manifests being submitted to the API. This is really nice and allows administrators to control the manifests coming in as fine-grained as they please.  

However, administrators may not always want to take direct action (such as denial) on manifests arriving at the API. This is where kube-opa-violation-exporter comes in.  
It allows administrators of Kubernetes clusters to define [Rego](https://www.openpolicyagent.org/docs/latest/policy-language/) policies that they want to flag violations for by exposing a [Prometheus](https://prometheus.io/) metric.  

Some example use cases include monitoring the use of deprecated APIs, unwanted docker images, or container vars containing strings like `API_KEY`, etc.  
Administrators can craft dashboards or alerts when such conditions are observed to better expose this information to users.

## Usage
`ConfigMap` objects containing the Rego policy/policies and the application configuration can be mounted to configure what you want to evaluate and how you want to evaluate it.

### Options
| Option   | Default       | Description                    |
|:---------|:--------------|:-------------------------------|
| `config` | `config.yaml` | Path to the configuration      |
| `policy` | `policy.rego` | Path to the policy to evaluate |

#### `config`
Configuration of the exporter is very simple at the moment. A YAML manifest can be provided in the following format to describe which objects in which namespace you want to watch for evaluation:
```yaml
namespace: default
ignore_children: true
objects:
  - group: apps
    version: v1
    resource: deployments
  - group: apps
    version: v1
    resource: daemonsets
  - group: apps
    version: v1
    resource: replicasets
```

| Option            | Default | Description                                                                                                                                          |
|:------------------|:--------|:-----------------------------------------------------------------------------------------------------------------------------------------------------|
| `namespace`       | `""`    | Kubernetes namespace to watch objects in. If empty or omitted, all namespaces will be observed                                                       |
| `ignore_children` | `false` | Boolean that decides if objects spawned as part of a user managed object (such as a ReplicaSet from a user managed Deployment) should be evaluated   |
| `objects`         | none    | A list of [GroupVersionResource](https://pkg.go.dev/k8s.io/apimachinery/pkg/runtime/schema#GroupVersionResource) expressions to observe and evaluate |

The example configuration would instruct the exporter to monitor `apps/v1/Deployment`, `apps/v1/DaemonSet`, and `apps/v1/ReplicaSet` objects in the `default` namespace, but ignore child objects.

#### `policy`
Check [`example/policies`](example/policies), where you will find [the 1.16 deprecation policy from kube-no-trouble](https://github.com/doitintl/kube-no-trouble/blob/master/rules/deprecated-1-16.rego) and a simplistic "bad label" policy to play around with.  

## Deployment
A Helm chart is available for easy deployment at https://charts.cmacr.ae (documentation coming soon!)
