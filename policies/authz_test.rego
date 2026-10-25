package authz_test

import data.authz

# This test will pass.
test_ok := true

test_allow_whitelist if {
	authz.allow
		with input as {"payload": {"roles": []}, "action": "GET:/health"}
		with data.whitelist as ["GET:/health$"]
}

test_allow_viewer if {
	authz.allow
		with input as {"payload": {"roles": ["viewer"]}, "action": "GET:/api/box"}
		with data.roles as {"viewer": ["templates.view_all"]}
		with data.permissions as {"templates.view_all": [ "GET:/api/box$"]}
}

test_allow_viewer_deny if {
	not authz.allow
		with input as {"payload": {"roles": ["viewer"]}, "action": "GET:/api/entry/key?v=production/proxy"}
		with data.roles as {"viewer": ["templates.view_development_qa_global"]}
		with data.permissions as {"templates.view_development_qa_global": [ "^GET:/api/entry/key\\?v=(development|qa|global)/(.*)"]}
}

# Patterns match from the start of the action: a whitelisted action smuggled
# into the query string of another request must not grant access.
test_deny_whitelist_smuggled_in_query if {
	not authz.allow
		with input as {"payload": {"roles": []}, "action": "GET:/admin/secret?x=GET:/health"}
		with data.whitelist as ["GET:/health$"]
}

test_deny_permission_smuggled_in_query if {
	not authz.allow
		with input as {"payload": {"roles": ["viewer"]}, "action": "GET:/admin/secret?x=GET:/api/box"}
		with data.roles as {"viewer": ["templates.view_all"]}
		with data.permissions as {"templates.view_all": ["GET:/api/box$"]}
}

test_deny_permission_smuggled_in_path if {
	not authz.allow
		with input as {"payload": {"roles": ["viewer"]}, "action": "GET:/admin/GET:/api/box"}
		with data.roles as {"viewer": ["templates.view_all"]}
		with data.permissions as {"templates.view_all": ["GET:/api/box$"]}
}

# A pattern that already carries its own anchor keeps working.
test_allow_explicitly_anchored_pattern if {
	authz.allow
		with input as {"payload": {"roles": ["viewer"]}, "action": "GET:/api/entry/key?v=qa/x"}
		with data.roles as {"viewer": ["templates.view_development_qa_global"]}
		with data.permissions as {"templates.view_development_qa_global": ["^GET:/api/entry/key\\?v=(development|qa|global)/(.*)"]}
}
