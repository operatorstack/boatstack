### Safety checks distinguish SQL from ordinary code

Boatstack no longer blocks release checks whose command contains `check-update` or Python API files that configure the `DELETE` HTTP method. Real unbounded SQL `DELETE FROM` and `UPDATE … SET` operations remain denied, while bounded updates and ordinary product commits can proceed through the normal agent workflow.
