# Security governance

Repository administrators must preserve these GitHub controls; they cannot be
enforced by tracked source alone. They were enabled for
`rechedev9/fragforge` on 2026-07-23.

- Enable the dependency graph, Dependabot alerts, security updates, and secret
  scanning with push protection for `rechedev9/fragforge`.
- Protect `main`, including administrators: require the `security`, `go`, `web`,
  `desktop`, and `landing` checks; require branches to be up to date; disallow
  force pushes and deletion.
- Keep direct commits to `main` limited to maintainers. The workflow runs the
  same required checks on pushes as an additional safety net.
- Review Dependabot pull requests with the affected package gate and production
  audit before merging.

The CI workflow performs repository-portable checks: Go vulnerability and
security scans, a redacted secret scan, production dependency audits for all
three JavaScript packages, and the web, desktop, and landing build gates.
