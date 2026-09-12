# Code Comment Rules

Apply to new or modified Go code and tests.

- Write concise English. Comment only intent, constraints, contracts, or trade-offs that names, types, and clear code cannot express. Improve unclear code first.
- Do not comment every function, statement, or small change. Exported symbols do not automatically need comments.
- Keep comments beside relevant code, usually one or two short sentences. Move lengthy explanations to design documents. Avoid banners, step-by-step narration, stacked explanations, and comments explaining comments.
- Before changing a function, read its existing comments. Update or replace them in place; preserve accurate comments and remove redundant ones. Never append a correction or extension beneath an existing explanation, even within the same `//` block.
- Describe current behavior only. Review changed code for stale, contradictory, or duplicate comments. Put change history in commits/PRs; remove commented-out code.
- When documentation is needed, place `//` comments above declarations, starting with the symbol name; package documentation starts with `Package <name>`.
- Explain non-obvious errors, side effects, snapshot ownership, synchronization, cancellation, cleanup, and fault phase/scope. State only guarantees supported by implementation and tests.
- Use `TODO:` for specific remaining work; include an issue reference when available, never invent one.
