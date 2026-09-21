# Faultline documentation

Faultline sits between an application and its dependency. Point the application
at a Faultline listener, define deterministic fault rules, and observe how the
application responds.

| Goal | Document |
| --- | --- |
| Run the first HTTP example | [Getting started](getting-started.md) |
| Deploy on a server | [Deployment](deployment.md) |
| Find every command and flag | [CLI reference](cli-reference.md) |
| Write rules and match traffic | [Configuration](configuration.md) |
| Toggle injection, reload, and read counters | [CLI operations](operations.md) |
| Use the HTTPS administration UI | [Tester UI](tester-ui.md) |
| Test a lost COMMIT acknowledgement | [PostgreSQL and MySQL](databases.md) |

Runnable demonstrations live under `examples/`. Implementation plans and
detailed verification evidence live under `plans/`.
