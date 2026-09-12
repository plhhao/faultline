# Task Workflow

For repository changes, follow **DEFINE → PLAN → BUILD → VERIFY → REVIEW → Done**. Scale detail to the task; small changes need only a brief definition and plan. Continue without approval between stages unless required by the user or an actual permission boundary.

1. **DEFINE:** Clarify the outcome, scope, constraints, and acceptance criteria. Read relevant instructions and specifications; resolve blocking ambiguity before implementation.
2. **PLAN:** State the smallest set of changes and how to verify the acceptance criteria. Avoid unrelated work.
3. **BUILD:** Implement the plan, following repository rules. Revisit DEFINE or PLAN if new evidence changes the scope.
4. **VERIFY:** Run checks appropriate to the change. Record actual results; distinguish passed, failed, and skipped checks. Documentation-only changes need content and link checks, not runtime tests.
5. **REVIEW:** Inspect the final changes for correctness, scope, regressions, duplication, stale comments, and consistency with the specification. Fix findings and repeat affected verification and review.

Mark **Done** only when acceptance criteria are met and no blocking findings or required checks remain unresolved. Otherwise report the blocker; do not log unfinished work as completed.

After Done, add one concise entry to root `CHANGELOG.md`: date (`YYYY-MM-DD`), completed outcome, affected paths, verification results, and material limitations if any. Keep newest entries first and preserve existing history. Do not invent results, backfill earlier work, or create recursive entries for the changelog update itself. Check the entry before the final response.
