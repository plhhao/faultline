# Tester UI

The Tester UI lets editors create a draft, validate it, review a diff, and apply
it over HTTPS. Logins are independent; there is no SSO integration.

## Local setup

```bash
python3 examples/tester/setup.py
```

The script builds the binary, creates localhost certificates, prepares HTTP/gRPC
configuration, and creates editor/viewer accounts. Follow the
[tester guide](../examples/tester/README.md), then open `https://localhost:8443`.

## Editing a rule

1. Select a proxy and edit rules in the draft.
2. Select **Validate draft & review diff**.
3. Compare the Active and Draft columns for changed fields, rules, or order.
4. Select **Apply reviewed draft**. The revision changes, but injection stays disabled.
5. Select **Enable injection** to let rules affect new traffic.

**Compare with latest active** updates only the Active comparison column.
**Use latest revision as draft base** changes the revision precondition to solve
a conflict; the current draft can overwrite another editor's change.
**Discard draft and load active** removes the entire draft and loads active rules.

Drafts live in browser-tab memory. Reloading or closing the tab loses them. On
session expiry, an edited draft remains for the user to decide what to do with;
an untouched draft reloads from the active configuration.

The `Injection ON/OFF` badge is server-confirmed state. Status polls every five
seconds; the Updated timestamp does not mean a rule was just applied. `Observed
outcomes` are proxy observations, not proof that an upstream committed or rolled
back work. `Rule counters` are eligible and selected counts for the active revision.
