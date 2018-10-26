package k8s
import data.kubernetes.policies

# Matches provides an abstraction to find policies that match the (name). 
policymatches[[name, policy]] {
    policy := policies[name]
}