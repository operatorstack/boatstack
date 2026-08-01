### Update receipts verify current state

Boatstack no longer treats a detached `SUCCEEDED` update receipt as permanent proof after its generated repository diff has been discarded. A clean retry verifies the target bundle, hooks, runtime pin, helper identity, and preserved integrations; when that postcondition is missing, it safely reopens only the bounded local update and regenerates the infrastructure diff. External publication receipts retain their existing duplicate-suppression behavior.
