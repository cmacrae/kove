<p align="center">
  <a href="https://github.com/cmacrae/kube-opa-violation-exporter/blob/master/LICENSE">
    <img src="https://img.shields.io/github/license/cmacrae/kube-opa-violation-exporter.svg?color=a6dcef" alt="License Badge">
  </a>
  <a href="https://github.com/cmacrae/kube-opa-violation-exporter/compare/v0.1.0...HEAD">
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
It allows administrators of Kubernetes clusters to define OPA policies that they want to flag violations for by exposing a [Prometheus](https://prometheus.io/) metric.  

Some example use cases include monitoring the use of deprecated APIs, unwanted docker images, or container vars containing strings like `API_KEY`, etc.  
Administrators can craft dashboards or alerts when such conditions are observed to better expose this information to users.

## Usage
In its current implementation, usage is very simple. A single flag is accepted: `-policy` - the path to the policy to evaluate (defaults to `policy.rego`).  
The intended use is that a `ConfigMap` containing the policy/policies be mounted into the exporter container for it to evaluate.

Check [`example-policies`](example-policies), where you will find [the 1.16 deprecation policy from kube-no-trouble](https://github.com/doitintl/kube-no-trouble/blob/master/rules/deprecated-1-16.rego) to play around with.  


## Deployment
A Helm chart is available for easy deployment at https://charts.cmacr.ae (documentation coming soon!)
