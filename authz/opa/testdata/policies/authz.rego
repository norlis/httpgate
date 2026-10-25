# METADATA
# title: Example
# description: Example package with documentation
package authz

import future.keywords.contains
import future.keywords.in
import future.keywords.if

default allow := false

default action_allowed := false

default roles := {"anonymous"}

roles := input.payload.roles if {
	count(input.payload.roles) > 0
}

# whitelist
allow if {
	some action in data.whitelist
	action_matches(action)
}

allow if {
	action_allowed
}

action_allowed if {
	some role in roles
	some permission in data.roles[role]
	some path in data.permissions[permission]
	action_matches(path)
}

# Anchored at the start so a pattern cannot match an allowed action smuggled
# into the path or query of another request ("GET:/x?y=GET:/health").
# input.action is "METHOD:/path?query"; patterns decide their own end anchor.
action_matches(pattern) if {
	regex.match(concat("", ["^(?:", pattern, ")"]), input.action)
}

# permission names granted to the request's roles
permissions contains perm if {
	some role in roles
	some perm in data.roles[role]
}

# flattened regex path patterns granted to the request's roles
allowed_resources contains pattern if {
	some role in roles
	some perm in data.roles[role]
	some pattern in data.permissions[perm]
}
