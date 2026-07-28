### Publisher auto-merge keeps minimum App permissions

The publisher no longer reads branch-protection administration endpoints at
runtime. Fleet conformance verifies repository policy, while the App uses only
contents and pull-request permissions to request native auto-merge.
