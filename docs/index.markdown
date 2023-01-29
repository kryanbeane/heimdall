---
# Feel free to add content and custom Front Matter to this file.
# To modify the layout, see https://jekyllrb.com/docs/themes/#overriding-theme-defaults

layout: home
---

# Welcome to Heimdall's Wiki!
Heimdall is an open-source tool for safeguarding resources in multi-operator Kubernetes environments. By reconfiguring RBAC resources, Heimdall blocks unauthorized changes made by Operators, ensuring the integrity of specific resources within the cluster. In case of any detected changes that deviate from the expected state, Heimdall sends an interactive notification to the developer via a pre-configured Slack channel. This response enables the developer to quickly rectify the issue. The configured owner of the Resource will be permitted to change the Resource freely throughout.

## The Problem
In Kubernetes, Operators and Controllers are responsible for reconciling the actual state of a resource with its desired state. However, when two Operators with conflicting desired states are both reconciling the same resource, it leads to an ongoing state change, causing confusion and issues. Heimdall solves this problem by providing a way to monitor resources and prevent unauthorized changes.
